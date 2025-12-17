// Package main implements the Discord plugin for Relicta.
package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"github.com/relicta-tech/relicta-plugin-sdk/helpers"
	"github.com/relicta-tech/relicta-plugin-sdk/plugin"
)

// Shared HTTP client for connection reuse across requests.
// Includes security hardening: TLS 1.3+, redirect protection, SSRF prevention.
var defaultHTTPClient = &http.Client{
	Timeout: 10 * time.Second,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		// Limit redirect chain length
		if len(via) >= 3 {
			return fmt.Errorf("too many redirects")
		}
		// Prevent redirect to non-HTTPS
		if req.URL.Scheme != "https" {
			return fmt.Errorf("redirect to non-HTTPS URL not allowed")
		}
		// Prevent redirect away from Discord domains (SSRF protection)
		if req.URL.Host != "discord.com" && req.URL.Host != "discordapp.com" {
			return fmt.Errorf("redirect away from Discord domains not allowed")
		}
		return nil
	},
	Transport: &http.Transport{
		MaxIdleConns:        10,
		MaxIdleConnsPerHost: 5,
		IdleConnTimeout:     90 * time.Second,
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS13,
		},
	},
}

// DiscordPlugin implements the Discord notification plugin.
type DiscordPlugin struct{}

// Config represents the Discord plugin configuration.
type Config struct {
	// WebhookURL is the Discord webhook URL.
	WebhookURL string `json:"webhook,omitempty"`
	// Username is the bot username (overrides webhook default).
	Username string `json:"username,omitempty"`
	// AvatarURL is the bot avatar URL (overrides webhook default).
	AvatarURL string `json:"avatar_url,omitempty"`
	// NotifyOnSuccess sends notification on successful release.
	NotifyOnSuccess bool `json:"notify_on_success"`
	// NotifyOnError sends notification on failed release.
	NotifyOnError bool `json:"notify_on_error"`
	// IncludeChangelog includes changelog in the notification.
	IncludeChangelog bool `json:"include_changelog"`
	// Mentions is a list of users/roles to mention (format: <@user_id> or <@&role_id>).
	Mentions []string `json:"mentions,omitempty"`
	// ThreadID posts to a specific thread within the channel (optional).
	ThreadID string `json:"thread_id,omitempty"`
	// Color is the embed color in hex (default: 5763719 for success, 15548997 for error).
	Color int `json:"color,omitempty"`
}

// DiscordMessage represents a Discord webhook message payload.
type DiscordMessage struct {
	Content   string  `json:"content,omitempty"`
	Username  string  `json:"username,omitempty"`
	AvatarURL string  `json:"avatar_url,omitempty"`
	Embeds    []Embed `json:"embeds,omitempty"`
	ThreadID  string  `json:"thread_id,omitempty"`
}

// Embed represents a Discord embed.
type Embed struct {
	Title       string       `json:"title,omitempty"`
	Description string       `json:"description,omitempty"`
	URL         string       `json:"url,omitempty"`
	Color       int          `json:"color,omitempty"`
	Fields      []EmbedField `json:"fields,omitempty"`
	Footer      *EmbedFooter `json:"footer,omitempty"`
	Timestamp   string       `json:"timestamp,omitempty"`
	Author      *EmbedAuthor `json:"author,omitempty"`
}

// EmbedField represents a field in a Discord embed.
type EmbedField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline,omitempty"`
}

// EmbedFooter represents the footer of a Discord embed.
type EmbedFooter struct {
	Text    string `json:"text,omitempty"`
	IconURL string `json:"icon_url,omitempty"`
}

// EmbedAuthor represents the author section of a Discord embed.
type EmbedAuthor struct {
	Name    string `json:"name,omitempty"`
	URL     string `json:"url,omitempty"`
	IconURL string `json:"icon_url,omitempty"`
}

// Discord embed colors (decimal values).
const (
	ColorSuccess = 5763719  // Green (#57F287)
	ColorError   = 15548997 // Red (#ED4245)
)

// GetInfo returns plugin metadata.
func (p *DiscordPlugin) GetInfo() plugin.Info {
	return plugin.Info{
		Name:        "discord",
		Version:     "2.0.0",
		Description: "Send Discord notifications for releases",
		Author:      "Relicta Team",
		Hooks: []plugin.Hook{
			plugin.HookPostPublish,
			plugin.HookOnSuccess,
			plugin.HookOnError,
		},
		ConfigSchema: `{
			"type": "object",
			"properties": {
				"webhook": {"type": "string", "description": "Discord webhook URL (or use DISCORD_WEBHOOK_URL env)"},
				"username": {"type": "string", "description": "Bot username", "default": "Relicta"},
				"avatar_url": {"type": "string", "description": "Bot avatar URL"},
				"notify_on_success": {"type": "boolean", "description": "Notify on success", "default": true},
				"notify_on_error": {"type": "boolean", "description": "Notify on error", "default": true},
				"include_changelog": {"type": "boolean", "description": "Include changelog", "default": false},
				"mentions": {"type": "array", "items": {"type": "string"}, "description": "Users/roles to mention (<@user_id> or <@&role_id>)"},
				"thread_id": {"type": "string", "description": "Thread ID to post to (optional)"},
				"color": {"type": "integer", "description": "Embed color in decimal (default varies by status)"}
			},
			"required": ["webhook"]
		}`,
	}
}

// Execute runs the plugin for a given hook.
func (p *DiscordPlugin) Execute(ctx context.Context, req plugin.ExecuteRequest) (*plugin.ExecuteResponse, error) {
	cfg := p.parseConfig(req.Config)

	switch req.Hook {
	case plugin.HookPostPublish, plugin.HookOnSuccess:
		if !cfg.NotifyOnSuccess {
			return &plugin.ExecuteResponse{
				Success: true,
				Message: "Success notification disabled",
			}, nil
		}
		return p.sendSuccessNotification(ctx, cfg, req.Context, req.DryRun)

	case plugin.HookOnError:
		if !cfg.NotifyOnError {
			return &plugin.ExecuteResponse{
				Success: true,
				Message: "Error notification disabled",
			}, nil
		}
		return p.sendErrorNotification(ctx, cfg, req.Context, req.DryRun)

	default:
		return &plugin.ExecuteResponse{
			Success: true,
			Message: fmt.Sprintf("Hook %s not handled", req.Hook),
		}, nil
	}
}

// buildDiscordMentions formats mentions for Discord.
func buildDiscordMentions(mentions []string) string {
	if len(mentions) == 0 {
		return ""
	}
	// Discord mentions are like <@user_id> for users or <@&role_id> for roles
	var formatted []string
	for _, m := range mentions {
		if strings.HasPrefix(m, "<@") {
			formatted = append(formatted, m)
		} else {
			formatted = append(formatted, fmt.Sprintf("<@%s>", m))
		}
	}
	return strings.Join(formatted, " ")
}

// sendSuccessNotification sends a success notification.
func (p *DiscordPlugin) sendSuccessNotification(ctx context.Context, cfg *Config, releaseCtx plugin.ReleaseContext, dryRun bool) (*plugin.ExecuteResponse, error) {
	// Build embed fields
	fields := []EmbedField{
		{Name: "Version", Value: releaseCtx.Version, Inline: true},
		{Name: "Release Type", Value: cases.Title(language.English).String(releaseCtx.ReleaseType), Inline: true},
		{Name: "Branch", Value: releaseCtx.Branch, Inline: true},
		{Name: "Tag", Value: releaseCtx.TagName, Inline: true},
	}

	if releaseCtx.Changes != nil {
		features := len(releaseCtx.Changes.Features)
		fixes := len(releaseCtx.Changes.Fixes)
		breaking := len(releaseCtx.Changes.Breaking)

		summary := fmt.Sprintf("%d features, %d fixes", features, fixes)
		if breaking > 0 {
			summary += fmt.Sprintf(", %d breaking changes", breaking)
		}
		fields = append(fields, EmbedField{Name: "Changes", Value: summary, Inline: false})
	}

	description := ""
	if cfg.IncludeChangelog && releaseCtx.ReleaseNotes != "" {
		// Truncate if too long (Discord limit is 4096 for embed description)
		notes := releaseCtx.ReleaseNotes
		if len(notes) > 2000 {
			notes = notes[:2000] + "..."
		}
		// Escape HTML to prevent XSS attacks in release notes
		description = html.EscapeString(notes)
	}

	// Build mentions content
	mentionText := buildDiscordMentions(cfg.Mentions)

	// Determine color
	color := cfg.Color
	if color == 0 {
		color = ColorSuccess
	}

	// Build embed URL if repository URL is available
	embedURL := ""
	if releaseCtx.RepositoryURL != "" && releaseCtx.TagName != "" {
		embedURL = fmt.Sprintf("%s/releases/tag/%s", strings.TrimSuffix(releaseCtx.RepositoryURL, ".git"), releaseCtx.TagName)
	}

	msg := DiscordMessage{
		Content:   mentionText,
		Username:  cfg.Username,
		AvatarURL: cfg.AvatarURL,
		ThreadID:  cfg.ThreadID,
		Embeds: []Embed{
			{
				Title:       fmt.Sprintf("Release %s Published!", releaseCtx.Version),
				Description: description,
				URL:         embedURL,
				Color:       color,
				Fields:      fields,
				Footer: &EmbedFooter{
					Text: "Relicta",
				},
				Timestamp: time.Now().UTC().Format(time.RFC3339),
			},
		},
	}

	if dryRun {
		return &plugin.ExecuteResponse{
			Success: true,
			Message: "Would send Discord success notification",
			Outputs: map[string]any{
				"version": releaseCtx.Version,
			},
		}, nil
	}

	if err := p.sendMessage(ctx, cfg.WebhookURL, msg); err != nil {
		return &plugin.ExecuteResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to send Discord message: %v", err),
		}, nil
	}

	return &plugin.ExecuteResponse{
		Success: true,
		Message: "Sent Discord success notification",
	}, nil
}

// sendErrorNotification sends an error notification.
func (p *DiscordPlugin) sendErrorNotification(ctx context.Context, cfg *Config, releaseCtx plugin.ReleaseContext, dryRun bool) (*plugin.ExecuteResponse, error) {
	fields := []EmbedField{
		{Name: "Version", Value: releaseCtx.Version, Inline: true},
		{Name: "Branch", Value: releaseCtx.Branch, Inline: true},
	}

	// Build mentions content
	mentionText := buildDiscordMentions(cfg.Mentions)

	// Determine color
	color := cfg.Color
	if color == 0 {
		color = ColorError
	}

	msg := DiscordMessage{
		Content:   mentionText,
		Username:  cfg.Username,
		AvatarURL: cfg.AvatarURL,
		ThreadID:  cfg.ThreadID,
		Embeds: []Embed{
			{
				Title:  fmt.Sprintf("Release %s Failed", releaseCtx.Version),
				Color:  color,
				Fields: fields,
				Footer: &EmbedFooter{
					Text: "Relicta",
				},
				Timestamp: time.Now().UTC().Format(time.RFC3339),
			},
		},
	}

	if dryRun {
		return &plugin.ExecuteResponse{
			Success: true,
			Message: "Would send Discord error notification",
		}, nil
	}

	if err := p.sendMessage(ctx, cfg.WebhookURL, msg); err != nil {
		return &plugin.ExecuteResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to send Discord message: %v", err),
		}, nil
	}

	return &plugin.ExecuteResponse{
		Success: true,
		Message: "Sent Discord error notification",
	}, nil
}

// sendMessage sends a message to Discord.
func (p *DiscordPlugin) sendMessage(ctx context.Context, webhookURL string, msg DiscordMessage) error {
	payload, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	// Append thread_id to URL if specified
	finalURL := webhookURL
	if msg.ThreadID != "" {
		parsedURL, err := url.Parse(webhookURL)
		if err != nil {
			return fmt.Errorf("failed to parse webhook URL: %w", err)
		}
		q := parsedURL.Query()
		q.Set("thread_id", msg.ThreadID)
		parsedURL.RawQuery = q.Encode()
		finalURL = parsedURL.String()
	}

	req, err := http.NewRequestWithContext(ctx, "POST", finalURL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := defaultHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Discord returns 204 No Content on success, or 200 OK with wait=true
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("discord returned status %d", resp.StatusCode)
	}

	return nil
}

// parseConfig parses the plugin configuration.
func (p *DiscordPlugin) parseConfig(raw map[string]any) *Config {
	parser := helpers.NewConfigParser(raw)

	return &Config{
		WebhookURL:       parser.GetString("webhook", "DISCORD_WEBHOOK_URL", ""),
		Username:         parser.GetString("username", "", "Relicta"),
		AvatarURL:        parser.GetString("avatar_url", "", ""),
		NotifyOnSuccess:  parser.GetBool("notify_on_success", true),
		NotifyOnError:    parser.GetBool("notify_on_error", true),
		IncludeChangelog: parser.GetBool("include_changelog", false),
		ThreadID:         parser.GetString("thread_id", "", ""),
		Color:            parser.GetInt("color", 0),
		Mentions:         parser.GetStringSlice("mentions", nil),
	}
}

// validateDiscordWebhookURL validates a Discord webhook URL.
func validateDiscordWebhookURL(webhookURL string) error {
	if webhookURL == "" {
		return fmt.Errorf("webhook URL is required")
	}

	parsed, err := url.Parse(webhookURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	if parsed.Scheme != "https" {
		return fmt.Errorf("webhook URL must use HTTPS")
	}

	if parsed.Host != "discord.com" && parsed.Host != "discordapp.com" {
		return fmt.Errorf("webhook URL must be on discord.com or discordapp.com")
	}

	if !strings.HasPrefix(parsed.Path, "/api/webhooks/") {
		return fmt.Errorf("webhook URL path must start with /api/webhooks/")
	}

	return nil
}

// Validate validates the plugin configuration.
func (p *DiscordPlugin) Validate(_ context.Context, config map[string]any) (*plugin.ValidateResponse, error) {
	vb := helpers.NewValidationBuilder()

	// Get webhook URL with env fallback
	parser := helpers.NewConfigParser(config)
	webhook := parser.GetString("webhook", "DISCORD_WEBHOOK_URL", "")

	// Check environment fallback if not in config
	if webhook == "" {
		webhook = os.Getenv("DISCORD_WEBHOOK_URL")
	}

	if webhook == "" {
		vb.AddErrorWithCode("webhook",
			"Discord webhook URL is required (set DISCORD_WEBHOOK_URL env var or configure webhook)",
			"required")
	} else {
		if err := validateDiscordWebhookURL(webhook); err != nil {
			vb.AddErrorWithCode("webhook", err.Error(), "format")
		}
	}

	return vb.Build(), nil
}
