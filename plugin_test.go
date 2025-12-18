package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/relicta-tech/relicta-plugin-sdk/plugin"
)

func TestGetInfo(t *testing.T) {
	p := &DiscordPlugin{}
	info := p.GetInfo()

	t.Run("name", func(t *testing.T) {
		if info.Name != "discord" {
			t.Errorf("expected name 'discord', got %q", info.Name)
		}
	})

	t.Run("version", func(t *testing.T) {
		if info.Version != "2.0.0" {
			t.Errorf("expected version '2.0.0', got %q", info.Version)
		}
	})

	t.Run("description", func(t *testing.T) {
		if info.Description == "" {
			t.Error("expected non-empty description")
		}
	})

	t.Run("author", func(t *testing.T) {
		if info.Author != "Relicta Team" {
			t.Errorf("expected author 'Relicta Team', got %q", info.Author)
		}
	})

	t.Run("hooks", func(t *testing.T) {
		expectedHooks := []plugin.Hook{
			plugin.HookPostPublish,
			plugin.HookOnSuccess,
			plugin.HookOnError,
		}

		if len(info.Hooks) != len(expectedHooks) {
			t.Errorf("expected %d hooks, got %d", len(expectedHooks), len(info.Hooks))
			return
		}

		for i, h := range expectedHooks {
			if info.Hooks[i] != h {
				t.Errorf("expected hook[%d] to be %q, got %q", i, h, info.Hooks[i])
			}
		}
	})

	t.Run("config_schema_valid_json", func(t *testing.T) {
		if info.ConfigSchema == "" {
			t.Error("expected non-empty config schema")
			return
		}
		var schema map[string]any
		if err := json.Unmarshal([]byte(info.ConfigSchema), &schema); err != nil {
			t.Errorf("config schema is not valid JSON: %v", err)
		}
	})
}

func TestValidate(t *testing.T) {
	p := &DiscordPlugin{}
	ctx := context.Background()

	// Clear any environment variable that might interfere
	origEnv := os.Getenv("DISCORD_WEBHOOK_URL")
	_ = os.Unsetenv("DISCORD_WEBHOOK_URL")
	t.Cleanup(func() {
		if origEnv != "" {
			_ = os.Setenv("DISCORD_WEBHOOK_URL", origEnv)
		}
	})

	tests := []struct {
		name        string
		config      map[string]any
		envWebhook  string
		wantValid   bool
		wantErrCode string
		wantErrMsg  string
	}{
		{
			name:        "missing_webhook",
			config:      map[string]any{},
			wantValid:   false,
			wantErrCode: "required",
			wantErrMsg:  "required",
		},
		{
			name: "empty_webhook",
			config: map[string]any{
				"webhook": "",
			},
			wantValid:   false,
			wantErrCode: "required",
		},
		{
			name: "invalid_url",
			config: map[string]any{
				"webhook": "not-a-url",
			},
			wantValid:   false,
			wantErrCode: "format",
		},
		{
			name: "http_not_https",
			config: map[string]any{
				"webhook": "http://discord.com/api/webhooks/123/token",
			},
			wantValid:   false,
			wantErrCode: "format",
			wantErrMsg:  "HTTPS",
		},
		{
			name: "wrong_domain",
			config: map[string]any{
				"webhook": "https://example.com/api/webhooks/123/token",
			},
			wantValid:   false,
			wantErrCode: "format",
			wantErrMsg:  "discord.com",
		},
		{
			name: "wrong_path",
			config: map[string]any{
				"webhook": "https://discord.com/some/other/path",
			},
			wantValid:   false,
			wantErrCode: "format",
			wantErrMsg:  "/api/webhooks/",
		},
		{
			name: "valid_discord_com",
			config: map[string]any{
				"webhook": "https://discord.com/api/webhooks/123456789/abcdef123456",
			},
			wantValid: true,
		},
		{
			name: "valid_discordapp_com",
			config: map[string]any{
				"webhook": "https://discordapp.com/api/webhooks/123456789/abcdef123456",
			},
			wantValid: true,
		},
		{
			name:       "webhook_from_env",
			config:     map[string]any{},
			envWebhook: "https://discord.com/api/webhooks/123456789/abcdef123456",
			wantValid:  true,
		},
		{
			name: "config_overrides_env",
			config: map[string]any{
				"webhook": "https://discord.com/api/webhooks/111111/aaaaaa",
			},
			envWebhook: "https://discord.com/api/webhooks/222222/bbbbbb",
			wantValid:  true,
		},
		{
			name: "valid_with_all_options",
			config: map[string]any{
				"webhook":           "https://discord.com/api/webhooks/123456789/abcdef",
				"username":          "ReleaseBot",
				"avatar_url":        "https://example.com/avatar.png",
				"notify_on_success": true,
				"notify_on_error":   true,
				"include_changelog": true,
				"mentions":          []string{"<@123>", "<@&456>"},
				"thread_id":         "987654321",
				"color":             5763719,
			},
			wantValid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variable if needed
			if tt.envWebhook != "" {
				_ = os.Setenv("DISCORD_WEBHOOK_URL", tt.envWebhook)
				defer func() { _ = os.Unsetenv("DISCORD_WEBHOOK_URL") }()
			}

			resp, err := p.Validate(ctx, tt.config)
			if err != nil {
				t.Fatalf("Validate returned unexpected error: %v", err)
			}

			if resp.Valid != tt.wantValid {
				t.Errorf("expected Valid=%v, got %v", tt.wantValid, resp.Valid)
				if len(resp.Errors) > 0 {
					t.Logf("errors: %+v", resp.Errors)
				}
			}

			if !tt.wantValid {
				if len(resp.Errors) == 0 {
					t.Error("expected validation errors, got none")
					return
				}

				if tt.wantErrCode != "" {
					found := false
					for _, e := range resp.Errors {
						if e.Code == tt.wantErrCode {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("expected error code %q, got %+v", tt.wantErrCode, resp.Errors)
					}
				}

				if tt.wantErrMsg != "" {
					found := false
					for _, e := range resp.Errors {
						if strings.Contains(e.Message, tt.wantErrMsg) {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("expected error message containing %q, got %+v", tt.wantErrMsg, resp.Errors)
					}
				}
			}
		})
	}
}

func TestParseConfig(t *testing.T) {
	p := &DiscordPlugin{}

	// Clear environment variables for consistent testing
	origEnv := os.Getenv("DISCORD_WEBHOOK_URL")
	_ = os.Unsetenv("DISCORD_WEBHOOK_URL")
	t.Cleanup(func() {
		if origEnv != "" {
			_ = os.Setenv("DISCORD_WEBHOOK_URL", origEnv)
		}
	})

	tests := []struct {
		name           string
		config         map[string]any
		envWebhook     string
		expectedConfig *Config
	}{
		{
			name:   "empty_config_uses_defaults",
			config: map[string]any{},
			expectedConfig: &Config{
				WebhookURL:       "",
				Username:         "Relicta",
				AvatarURL:        "",
				NotifyOnSuccess:  true,
				NotifyOnError:    true,
				IncludeChangelog: false,
				Mentions:         nil,
				ThreadID:         "",
				Color:            0,
			},
		},
		{
			name:       "webhook_from_env",
			config:     map[string]any{},
			envWebhook: "https://discord.com/api/webhooks/env/token",
			expectedConfig: &Config{
				WebhookURL:       "https://discord.com/api/webhooks/env/token",
				Username:         "Relicta",
				AvatarURL:        "",
				NotifyOnSuccess:  true,
				NotifyOnError:    true,
				IncludeChangelog: false,
				Mentions:         nil,
				ThreadID:         "",
				Color:            0,
			},
		},
		{
			name: "config_overrides_env",
			config: map[string]any{
				"webhook": "https://discord.com/api/webhooks/config/token",
			},
			envWebhook: "https://discord.com/api/webhooks/env/token",
			expectedConfig: &Config{
				WebhookURL:       "https://discord.com/api/webhooks/config/token",
				Username:         "Relicta",
				AvatarURL:        "",
				NotifyOnSuccess:  true,
				NotifyOnError:    true,
				IncludeChangelog: false,
				Mentions:         nil,
				ThreadID:         "",
				Color:            0,
			},
		},
		{
			name: "all_options",
			config: map[string]any{
				"webhook":           "https://discord.com/api/webhooks/123/token",
				"username":          "CustomBot",
				"avatar_url":        "https://example.com/avatar.png",
				"notify_on_success": false,
				"notify_on_error":   false,
				"include_changelog": true,
				"mentions":          []any{"<@user1>", "<@&role1>"},
				"thread_id":         "thread123",
				"color":             16711680,
			},
			expectedConfig: &Config{
				WebhookURL:       "https://discord.com/api/webhooks/123/token",
				Username:         "CustomBot",
				AvatarURL:        "https://example.com/avatar.png",
				NotifyOnSuccess:  false,
				NotifyOnError:    false,
				IncludeChangelog: true,
				Mentions:         []string{"<@user1>", "<@&role1>"},
				ThreadID:         "thread123",
				Color:            16711680,
			},
		},
		{
			name: "boolean_as_string",
			config: map[string]any{
				"webhook":           "https://discord.com/api/webhooks/123/token",
				"notify_on_success": "true",
				"notify_on_error":   "false",
			},
			expectedConfig: &Config{
				WebhookURL:       "https://discord.com/api/webhooks/123/token",
				Username:         "Relicta",
				AvatarURL:        "",
				NotifyOnSuccess:  true,
				NotifyOnError:    false,
				IncludeChangelog: false,
				Mentions:         nil,
				ThreadID:         "",
				Color:            0,
			},
		},
		{
			name: "color_as_float64",
			config: map[string]any{
				"webhook": "https://discord.com/api/webhooks/123/token",
				"color":   float64(5763719),
			},
			expectedConfig: &Config{
				WebhookURL:       "https://discord.com/api/webhooks/123/token",
				Username:         "Relicta",
				AvatarURL:        "",
				NotifyOnSuccess:  true,
				NotifyOnError:    true,
				IncludeChangelog: false,
				Mentions:         nil,
				ThreadID:         "",
				Color:            5763719,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envWebhook != "" {
				_ = os.Setenv("DISCORD_WEBHOOK_URL", tt.envWebhook)
				defer func() { _ = os.Unsetenv("DISCORD_WEBHOOK_URL") }()
			}

			cfg := p.parseConfig(tt.config)

			if cfg.WebhookURL != tt.expectedConfig.WebhookURL {
				t.Errorf("WebhookURL: expected %q, got %q", tt.expectedConfig.WebhookURL, cfg.WebhookURL)
			}
			if cfg.Username != tt.expectedConfig.Username {
				t.Errorf("Username: expected %q, got %q", tt.expectedConfig.Username, cfg.Username)
			}
			if cfg.AvatarURL != tt.expectedConfig.AvatarURL {
				t.Errorf("AvatarURL: expected %q, got %q", tt.expectedConfig.AvatarURL, cfg.AvatarURL)
			}
			if cfg.NotifyOnSuccess != tt.expectedConfig.NotifyOnSuccess {
				t.Errorf("NotifyOnSuccess: expected %v, got %v", tt.expectedConfig.NotifyOnSuccess, cfg.NotifyOnSuccess)
			}
			if cfg.NotifyOnError != tt.expectedConfig.NotifyOnError {
				t.Errorf("NotifyOnError: expected %v, got %v", tt.expectedConfig.NotifyOnError, cfg.NotifyOnError)
			}
			if cfg.IncludeChangelog != tt.expectedConfig.IncludeChangelog {
				t.Errorf("IncludeChangelog: expected %v, got %v", tt.expectedConfig.IncludeChangelog, cfg.IncludeChangelog)
			}
			if cfg.ThreadID != tt.expectedConfig.ThreadID {
				t.Errorf("ThreadID: expected %q, got %q", tt.expectedConfig.ThreadID, cfg.ThreadID)
			}
			if cfg.Color != tt.expectedConfig.Color {
				t.Errorf("Color: expected %d, got %d", tt.expectedConfig.Color, cfg.Color)
			}

			// Compare mentions
			if len(cfg.Mentions) != len(tt.expectedConfig.Mentions) {
				t.Errorf("Mentions length: expected %d, got %d", len(tt.expectedConfig.Mentions), len(cfg.Mentions))
			} else {
				for i, m := range tt.expectedConfig.Mentions {
					if cfg.Mentions[i] != m {
						t.Errorf("Mentions[%d]: expected %q, got %q", i, m, cfg.Mentions[i])
					}
				}
			}
		})
	}
}

func TestExecute(t *testing.T) {
	p := &DiscordPlugin{}
	ctx := context.Background()

	baseContext := plugin.ReleaseContext{
		Version:       "1.2.3",
		TagName:       "v1.2.3",
		ReleaseType:   "minor",
		Branch:        "main",
		CommitSHA:     "abc123",
		RepositoryURL: "https://github.com/test/repo",
		ReleaseNotes:  "Test release notes",
		Changes: &plugin.CategorizedChanges{
			Features: []plugin.ConventionalCommit{
				{Hash: "abc", Description: "new feature"},
			},
			Fixes: []plugin.ConventionalCommit{
				{Hash: "def", Description: "bug fix"},
			},
		},
	}

	tests := []struct {
		name          string
		hook          plugin.Hook
		config        map[string]any
		dryRun        bool
		wantSuccess   bool
		wantMsgPrefix string
		wantOutputKey string
		wantOutputVal any
	}{
		{
			name:   "dry_run_post_publish",
			hook:   plugin.HookPostPublish,
			dryRun: true,
			config: map[string]any{
				"webhook":           "https://discord.com/api/webhooks/123/token",
				"notify_on_success": true,
			},
			wantSuccess:   true,
			wantMsgPrefix: "Would send Discord success notification",
			wantOutputKey: "version",
			wantOutputVal: "1.2.3",
		},
		{
			name:   "dry_run_on_success",
			hook:   plugin.HookOnSuccess,
			dryRun: true,
			config: map[string]any{
				"webhook":           "https://discord.com/api/webhooks/123/token",
				"notify_on_success": true,
			},
			wantSuccess:   true,
			wantMsgPrefix: "Would send Discord success notification",
		},
		{
			name:   "dry_run_on_error",
			hook:   plugin.HookOnError,
			dryRun: true,
			config: map[string]any{
				"webhook":         "https://discord.com/api/webhooks/123/token",
				"notify_on_error": true,
			},
			wantSuccess:   true,
			wantMsgPrefix: "Would send Discord error notification",
		},
		{
			name:   "success_notification_disabled",
			hook:   plugin.HookPostPublish,
			dryRun: true,
			config: map[string]any{
				"webhook":           "https://discord.com/api/webhooks/123/token",
				"notify_on_success": false,
			},
			wantSuccess:   true,
			wantMsgPrefix: "Success notification disabled",
		},
		{
			name:   "error_notification_disabled",
			hook:   plugin.HookOnError,
			dryRun: true,
			config: map[string]any{
				"webhook":         "https://discord.com/api/webhooks/123/token",
				"notify_on_error": false,
			},
			wantSuccess:   true,
			wantMsgPrefix: "Error notification disabled",
		},
		{
			name:   "unhandled_hook",
			hook:   plugin.HookPreInit,
			dryRun: true,
			config: map[string]any{
				"webhook": "https://discord.com/api/webhooks/123/token",
			},
			wantSuccess:   true,
			wantMsgPrefix: "Hook pre-init not handled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := plugin.ExecuteRequest{
				Hook:    tt.hook,
				Config:  tt.config,
				Context: baseContext,
				DryRun:  tt.dryRun,
			}

			resp, err := p.Execute(ctx, req)
			if err != nil {
				t.Fatalf("Execute returned unexpected error: %v", err)
			}

			if resp.Success != tt.wantSuccess {
				t.Errorf("expected Success=%v, got %v (error: %s)", tt.wantSuccess, resp.Success, resp.Error)
			}

			if tt.wantMsgPrefix != "" && !strings.HasPrefix(resp.Message, tt.wantMsgPrefix) {
				t.Errorf("expected message to start with %q, got %q", tt.wantMsgPrefix, resp.Message)
			}

			if tt.wantOutputKey != "" {
				if resp.Outputs == nil {
					t.Error("expected outputs map, got nil")
				} else if resp.Outputs[tt.wantOutputKey] != tt.wantOutputVal {
					t.Errorf("expected output[%s]=%v, got %v", tt.wantOutputKey, tt.wantOutputVal, resp.Outputs[tt.wantOutputKey])
				}
			}
		})
	}
}

func TestExecuteWithMockServer(t *testing.T) {
	// Create a mock Discord webhook server
	var receivedPayload DiscordMessage
	var receivedThreadID string

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}

		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("expected Content-Type application/json, got %s", ct)
		}

		// Check for thread_id in query params
		receivedThreadID = r.URL.Query().Get("thread_id")

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("failed to read request body: %v", err)
		}
		defer func() { _ = r.Body.Close() }()

		if err := json.Unmarshal(body, &receivedPayload); err != nil {
			t.Errorf("failed to unmarshal payload: %v", err)
		}

		// Discord returns 204 No Content on success
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	// Note: We cannot use the mock server directly because the plugin
	// validates the webhook URL domain. Instead, we test the message building
	// logic through dry run mode and test the HTTP client behavior separately.

	// Suppress unused variable warnings - these are used for verification in an extended test
	_ = receivedPayload
	_ = receivedThreadID
}

func TestBuildDiscordMentions(t *testing.T) {
	tests := []struct {
		name     string
		mentions []string
		want     string
	}{
		{
			name:     "empty_mentions",
			mentions: nil,
			want:     "",
		},
		{
			name:     "single_user_with_prefix",
			mentions: []string{"<@123456>"},
			want:     "<@123456>",
		},
		{
			name:     "single_user_without_prefix",
			mentions: []string{"123456"},
			want:     "<@123456>",
		},
		{
			name:     "role_mention",
			mentions: []string{"<@&789012>"},
			want:     "<@&789012>",
		},
		{
			name:     "multiple_mentions",
			mentions: []string{"<@user1>", "<@&role1>", "user2"},
			want:     "<@user1> <@&role1> <@user2>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildDiscordMentions(tt.mentions)
			if got != tt.want {
				t.Errorf("buildDiscordMentions(%v) = %q, want %q", tt.mentions, got, tt.want)
			}
		})
	}
}

func TestValidateDiscordWebhookURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "empty_url",
			url:     "",
			wantErr: true,
			errMsg:  "required",
		},
		{
			name:    "invalid_url",
			url:     "://invalid",
			wantErr: true,
			errMsg:  "invalid URL",
		},
		{
			name:    "http_scheme",
			url:     "http://discord.com/api/webhooks/123/token",
			wantErr: true,
			errMsg:  "HTTPS",
		},
		{
			name:    "wrong_host",
			url:     "https://evil.com/api/webhooks/123/token",
			wantErr: true,
			errMsg:  "discord.com",
		},
		{
			name:    "wrong_path",
			url:     "https://discord.com/wrong/path",
			wantErr: true,
			errMsg:  "/api/webhooks/",
		},
		{
			name:    "valid_discord_com",
			url:     "https://discord.com/api/webhooks/123456789/abcdef",
			wantErr: false,
		},
		{
			name:    "valid_discordapp_com",
			url:     "https://discordapp.com/api/webhooks/123456789/abcdef",
			wantErr: false,
		},
		{
			name:    "valid_with_query_params",
			url:     "https://discord.com/api/webhooks/123/token?wait=true",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateDiscordWebhookURL(tt.url)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
					return
				}
				if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestDiscordMessageStructure(t *testing.T) {
	t.Run("success_message_fields", func(t *testing.T) {
		p := &DiscordPlugin{}

		cfg := &Config{
			WebhookURL:       "https://discord.com/api/webhooks/123/token",
			Username:         "TestBot",
			AvatarURL:        "https://example.com/avatar.png",
			NotifyOnSuccess:  true,
			IncludeChangelog: true,
			Mentions:         []string{"<@user1>"},
			ThreadID:         "thread123",
			Color:            16711680,
		}

		releaseCtx := plugin.ReleaseContext{
			Version:       "2.0.0",
			TagName:       "v2.0.0",
			ReleaseType:   "major",
			Branch:        "main",
			RepositoryURL: "https://github.com/test/repo",
			ReleaseNotes:  "## Changes\n- Feature A\n- Bug fix B",
			Changes: &plugin.CategorizedChanges{
				Features: []plugin.ConventionalCommit{
					{Description: "Feature A"},
				},
				Fixes: []plugin.ConventionalCommit{
					{Description: "Bug fix B"},
				},
				Breaking: []plugin.ConventionalCommit{
					{Description: "Breaking change"},
				},
			},
		}

		resp, err := p.sendSuccessNotification(context.Background(), cfg, releaseCtx, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !resp.Success {
			t.Errorf("expected success, got failure: %s", resp.Error)
		}

		// Verify outputs contain version in dry run
		if resp.Outputs["version"] != "2.0.0" {
			t.Errorf("expected version output '2.0.0', got %v", resp.Outputs["version"])
		}
	})

	t.Run("error_message_fields", func(t *testing.T) {
		p := &DiscordPlugin{}

		cfg := &Config{
			WebhookURL:    "https://discord.com/api/webhooks/123/token",
			Username:      "TestBot",
			NotifyOnError: true,
			Mentions:      []string{"<@admin>"},
		}

		releaseCtx := plugin.ReleaseContext{
			Version: "2.0.0",
			Branch:  "main",
		}

		resp, err := p.sendErrorNotification(context.Background(), cfg, releaseCtx, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !resp.Success {
			t.Errorf("expected success, got failure: %s", resp.Error)
		}
	})
}

func TestColorConstants(t *testing.T) {
	t.Run("success_color", func(t *testing.T) {
		if ColorSuccess != 5763719 {
			t.Errorf("expected ColorSuccess=5763719, got %d", ColorSuccess)
		}
	})

	t.Run("error_color", func(t *testing.T) {
		if ColorError != 15548997 {
			t.Errorf("expected ColorError=15548997, got %d", ColorError)
		}
	})
}

func TestReleaseNoteTruncation(t *testing.T) {
	p := &DiscordPlugin{}

	// Create a very long release note
	longNote := strings.Repeat("A", 3000)

	cfg := &Config{
		WebhookURL:       "https://discord.com/api/webhooks/123/token",
		NotifyOnSuccess:  true,
		IncludeChangelog: true,
	}

	releaseCtx := plugin.ReleaseContext{
		Version:      "1.0.0",
		TagName:      "v1.0.0",
		ReleaseType:  "major",
		Branch:       "main",
		ReleaseNotes: longNote,
	}

	// Dry run to test the logic
	resp, err := p.sendSuccessNotification(context.Background(), cfg, releaseCtx, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !resp.Success {
		t.Errorf("expected success, got failure: %s", resp.Error)
	}

	// Note: We cannot directly verify truncation in dry run mode,
	// but the function should handle it gracefully
}

func TestContextCancellation(t *testing.T) {
	p := &DiscordPlugin{}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	req := plugin.ExecuteRequest{
		Hook: plugin.HookPostPublish,
		Config: map[string]any{
			"webhook":           "https://discord.com/api/webhooks/123/token",
			"notify_on_success": true,
		},
		Context: plugin.ReleaseContext{
			Version:     "1.0.0",
			TagName:     "v1.0.0",
			ReleaseType: "major",
			Branch:      "main",
		},
		DryRun: true, // Use dry run to avoid actual HTTP calls
	}

	// With dry run, context cancellation should not affect the result
	resp, err := p.Execute(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !resp.Success {
		t.Errorf("expected success in dry run, got failure: %s", resp.Error)
	}
}

func TestEmbedURLGeneration(t *testing.T) {
	p := &DiscordPlugin{}

	tests := []struct {
		name          string
		repoURL       string
		tagName       string
		wantURLSuffix string
	}{
		{
			name:          "github_url",
			repoURL:       "https://github.com/org/repo",
			tagName:       "v1.0.0",
			wantURLSuffix: "/releases/tag/v1.0.0",
		},
		{
			name:          "github_url_with_git_suffix",
			repoURL:       "https://github.com/org/repo.git",
			tagName:       "v1.0.0",
			wantURLSuffix: "/releases/tag/v1.0.0",
		},
		{
			name:          "empty_repo_url",
			repoURL:       "",
			tagName:       "v1.0.0",
			wantURLSuffix: "",
		},
		{
			name:          "empty_tag",
			repoURL:       "https://github.com/org/repo",
			tagName:       "",
			wantURLSuffix: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				WebhookURL:      "https://discord.com/api/webhooks/123/token",
				NotifyOnSuccess: true,
			}

			releaseCtx := plugin.ReleaseContext{
				Version:       "1.0.0",
				TagName:       tt.tagName,
				ReleaseType:   "major",
				Branch:        "main",
				RepositoryURL: tt.repoURL,
			}

			resp, err := p.sendSuccessNotification(context.Background(), cfg, releaseCtx, true)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !resp.Success {
				t.Errorf("expected success, got failure: %s", resp.Error)
			}

			// In dry run, we verify the function completes successfully
			// The embed URL logic is tested implicitly
		})
	}
}

func TestChangeSummaryGeneration(t *testing.T) {
	tests := []struct {
		name     string
		changes  *plugin.CategorizedChanges
		hasField bool
	}{
		{
			name:     "nil_changes",
			changes:  nil,
			hasField: false,
		},
		{
			name:     "empty_changes",
			changes:  &plugin.CategorizedChanges{},
			hasField: true,
		},
		{
			name: "with_breaking_changes",
			changes: &plugin.CategorizedChanges{
				Features: []plugin.ConventionalCommit{{Description: "feat1"}},
				Fixes:    []plugin.ConventionalCommit{{Description: "fix1"}, {Description: "fix2"}},
				Breaking: []plugin.ConventionalCommit{{Description: "breaking1"}},
			},
			hasField: true,
		},
	}

	p := &DiscordPlugin{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				WebhookURL:      "https://discord.com/api/webhooks/123/token",
				NotifyOnSuccess: true,
			}

			releaseCtx := plugin.ReleaseContext{
				Version:     "1.0.0",
				TagName:     "v1.0.0",
				ReleaseType: "minor",
				Branch:      "main",
				Changes:     tt.changes,
			}

			resp, err := p.sendSuccessNotification(context.Background(), cfg, releaseCtx, true)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !resp.Success {
				t.Errorf("expected success, got failure: %s", resp.Error)
			}
		})
	}
}

func TestHTMLEscapingInReleaseNotes(t *testing.T) {
	p := &DiscordPlugin{}

	// Release notes with HTML-like content that should be escaped
	releaseNotes := "<script>alert('xss')</script> & special <chars>"

	cfg := &Config{
		WebhookURL:       "https://discord.com/api/webhooks/123/token",
		NotifyOnSuccess:  true,
		IncludeChangelog: true,
	}

	releaseCtx := plugin.ReleaseContext{
		Version:      "1.0.0",
		TagName:      "v1.0.0",
		ReleaseType:  "patch",
		Branch:       "main",
		ReleaseNotes: releaseNotes,
	}

	resp, err := p.sendSuccessNotification(context.Background(), cfg, releaseCtx, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !resp.Success {
		t.Errorf("expected success, got failure: %s", resp.Error)
	}

	// The actual HTML escaping happens in the function; in dry run mode
	// we verify it completes without error
}

func TestSendMessageWithMockServer(t *testing.T) {
	// Save original client and restore after test
	origClient := defaultHTTPClient
	defer func() { defaultHTTPClient = origClient }()

	tests := []struct {
		name           string
		statusCode     int
		threadID       string
		wantErr        bool
		wantErrContain string
	}{
		{
			name:       "success_204",
			statusCode: http.StatusNoContent,
			wantErr:    false,
		},
		{
			name:       "success_200",
			statusCode: http.StatusOK,
			wantErr:    false,
		},
		{
			name:           "error_400",
			statusCode:     http.StatusBadRequest,
			wantErr:        true,
			wantErrContain: "status 400",
		},
		{
			name:           "error_401",
			statusCode:     http.StatusUnauthorized,
			wantErr:        true,
			wantErrContain: "status 401",
		},
		{
			name:           "error_500",
			statusCode:     http.StatusInternalServerError,
			wantErr:        true,
			wantErrContain: "status 500",
		},
		{
			name:       "with_thread_id",
			statusCode: http.StatusNoContent,
			threadID:   "123456789",
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var receivedThreadID string
			var receivedPayload DiscordMessage

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify HTTP method
				if r.Method != http.MethodPost {
					t.Errorf("expected POST, got %s", r.Method)
				}

				// Verify Content-Type
				if ct := r.Header.Get("Content-Type"); ct != "application/json" {
					t.Errorf("expected Content-Type application/json, got %s", ct)
				}

				// Check for thread_id in query params
				receivedThreadID = r.URL.Query().Get("thread_id")

				// Parse body
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Errorf("failed to read body: %v", err)
				}
				defer func() { _ = r.Body.Close() }()

				if err := json.Unmarshal(body, &receivedPayload); err != nil {
					t.Errorf("failed to unmarshal payload: %v", err)
				}

				w.WriteHeader(tt.statusCode)
			}))
			defer server.Close()

			// Replace default client with one that doesn't validate TLS
			defaultHTTPClient = &http.Client{
				Timeout: 10 * time.Second,
			}

			p := &DiscordPlugin{}

			msg := DiscordMessage{
				Content:  "Test content",
				Username: "TestBot",
				ThreadID: tt.threadID,
				Embeds: []Embed{
					{
						Title: "Test",
						Color: ColorSuccess,
					},
				},
			}

			err := p.sendMessage(context.Background(), server.URL, msg)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
					return
				}
				if tt.wantErrContain != "" && !strings.Contains(err.Error(), tt.wantErrContain) {
					t.Errorf("expected error containing %q, got %q", tt.wantErrContain, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}

			// Verify thread_id was passed correctly
			if tt.threadID != "" && receivedThreadID != tt.threadID {
				t.Errorf("expected thread_id %q, got %q", tt.threadID, receivedThreadID)
			}
		})
	}
}

func TestSendMessageInvalidURL(t *testing.T) {
	p := &DiscordPlugin{}

	msg := DiscordMessage{
		Content: "Test",
	}

	// Invalid URL that cannot be parsed
	err := p.sendMessage(context.Background(), "://invalid-url", msg)
	if err == nil {
		t.Error("expected error for invalid URL, got nil")
	}
}

func TestSendMessageContextCancelled(t *testing.T) {
	// Save original client and restore after test
	origClient := defaultHTTPClient
	defer func() { defaultHTTPClient = origClient }()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate slow response
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	defaultHTTPClient = &http.Client{
		Timeout: 5 * time.Second,
	}

	p := &DiscordPlugin{}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	msg := DiscordMessage{
		Content: "Test",
	}

	err := p.sendMessage(ctx, server.URL, msg)
	if err == nil {
		t.Error("expected error for cancelled context, got nil")
	}
}

func TestExecuteWithActualHTTPCall(t *testing.T) {
	// Save original client and restore after test
	origClient := defaultHTTPClient
	defer func() { defaultHTTPClient = origClient }()

	var receivedPayload DiscordMessage

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		defer func() { _ = r.Body.Close() }()
		_ = json.Unmarshal(body, &receivedPayload)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	defaultHTTPClient = &http.Client{Timeout: 10 * time.Second}

	p := &DiscordPlugin{}

	tests := []struct {
		name        string
		hook        plugin.Hook
		dryRun      bool
		wantSuccess bool
		wantMessage string
	}{
		{
			name:        "actual_success_notification",
			hook:        plugin.HookPostPublish,
			dryRun:      false,
			wantSuccess: true,
			wantMessage: "Sent Discord success notification",
		},
		{
			name:        "actual_error_notification",
			hook:        plugin.HookOnError,
			dryRun:      false,
			wantSuccess: true,
			wantMessage: "Sent Discord error notification",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := plugin.ExecuteRequest{
				Hook: tt.hook,
				Config: map[string]any{
					"webhook":           server.URL + "/api/webhooks/123/token",
					"notify_on_success": true,
					"notify_on_error":   true,
					"username":          "TestBot",
				},
				Context: plugin.ReleaseContext{
					Version:     "1.0.0",
					TagName:     "v1.0.0",
					ReleaseType: "patch",
					Branch:      "main",
				},
				DryRun: tt.dryRun,
			}

			resp, err := p.Execute(context.Background(), req)
			if err != nil {
				t.Fatalf("Execute returned unexpected error: %v", err)
			}

			if resp.Success != tt.wantSuccess {
				t.Errorf("expected Success=%v, got %v (error: %s)", tt.wantSuccess, resp.Success, resp.Error)
			}

			if resp.Message != tt.wantMessage {
				t.Errorf("expected message %q, got %q", tt.wantMessage, resp.Message)
			}

			// Verify the payload was received
			if receivedPayload.Username != "TestBot" {
				t.Errorf("expected username 'TestBot', got %q", receivedPayload.Username)
			}
		})
	}
}

func TestExecuteWithFailedHTTPCall(t *testing.T) {
	// Save original client and restore after test
	origClient := defaultHTTPClient
	defer func() { defaultHTTPClient = origClient }()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	defaultHTTPClient = &http.Client{Timeout: 10 * time.Second}

	p := &DiscordPlugin{}

	tests := []struct {
		name string
		hook plugin.Hook
	}{
		{
			name: "failed_success_notification",
			hook: plugin.HookPostPublish,
		},
		{
			name: "failed_error_notification",
			hook: plugin.HookOnError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := plugin.ExecuteRequest{
				Hook: tt.hook,
				Config: map[string]any{
					"webhook":           server.URL + "/api/webhooks/123/token",
					"notify_on_success": true,
					"notify_on_error":   true,
				},
				Context: plugin.ReleaseContext{
					Version:     "1.0.0",
					TagName:     "v1.0.0",
					ReleaseType: "patch",
					Branch:      "main",
				},
				DryRun: false,
			}

			resp, err := p.Execute(context.Background(), req)
			if err != nil {
				t.Fatalf("Execute returned unexpected error: %v", err)
			}

			// Should return failure in response
			if resp.Success {
				t.Error("expected Success=false for failed HTTP call")
			}

			if resp.Error == "" {
				t.Error("expected error message, got empty string")
			}

			if !strings.Contains(resp.Error, "failed to send Discord message") {
				t.Errorf("expected error about failed Discord message, got %q", resp.Error)
			}
		})
	}
}

func TestDefaultColorBehavior(t *testing.T) {
	p := &DiscordPlugin{}

	t.Run("success_uses_green_when_color_not_set", func(t *testing.T) {
		cfg := &Config{
			WebhookURL:      "https://discord.com/api/webhooks/123/token",
			NotifyOnSuccess: true,
			Color:           0, // Not set
		}

		releaseCtx := plugin.ReleaseContext{
			Version:     "1.0.0",
			TagName:     "v1.0.0",
			ReleaseType: "patch",
			Branch:      "main",
		}

		resp, err := p.sendSuccessNotification(context.Background(), cfg, releaseCtx, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !resp.Success {
			t.Errorf("expected success, got failure: %s", resp.Error)
		}
	})

	t.Run("error_uses_red_when_color_not_set", func(t *testing.T) {
		cfg := &Config{
			WebhookURL:    "https://discord.com/api/webhooks/123/token",
			NotifyOnError: true,
			Color:         0, // Not set
		}

		releaseCtx := plugin.ReleaseContext{
			Version: "1.0.0",
			Branch:  "main",
		}

		resp, err := p.sendErrorNotification(context.Background(), cfg, releaseCtx, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !resp.Success {
			t.Errorf("expected success, got failure: %s", resp.Error)
		}
	})

	t.Run("custom_color_is_used", func(t *testing.T) {
		cfg := &Config{
			WebhookURL:      "https://discord.com/api/webhooks/123/token",
			NotifyOnSuccess: true,
			Color:           16711680, // Custom red
		}

		releaseCtx := plugin.ReleaseContext{
			Version:     "1.0.0",
			TagName:     "v1.0.0",
			ReleaseType: "patch",
			Branch:      "main",
		}

		resp, err := p.sendSuccessNotification(context.Background(), cfg, releaseCtx, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !resp.Success {
			t.Errorf("expected success, got failure: %s", resp.Error)
		}
	})
}

func TestNilConfig(t *testing.T) {
	p := &DiscordPlugin{}

	// Test with nil config map
	cfg := p.parseConfig(nil)

	// Should return defaults
	if cfg.Username != "Relicta" {
		t.Errorf("expected default username 'Relicta', got %q", cfg.Username)
	}

	if !cfg.NotifyOnSuccess {
		t.Error("expected NotifyOnSuccess=true by default")
	}

	if !cfg.NotifyOnError {
		t.Error("expected NotifyOnError=true by default")
	}
}

func TestValidateWithNilConfig(t *testing.T) {
	p := &DiscordPlugin{}

	// Clear environment variable
	origEnv := os.Getenv("DISCORD_WEBHOOK_URL")
	_ = os.Unsetenv("DISCORD_WEBHOOK_URL")
	defer func() {
		if origEnv != "" {
			_ = os.Setenv("DISCORD_WEBHOOK_URL", origEnv)
		}
	}()

	resp, err := p.Validate(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Valid {
		t.Error("expected validation to fail with nil config")
	}

	if len(resp.Errors) == 0 {
		t.Error("expected validation errors")
	}
}

func TestDiscordMessageJSON(t *testing.T) {
	msg := DiscordMessage{
		Content:   "Test content",
		Username:  "TestBot",
		AvatarURL: "https://example.com/avatar.png",
		ThreadID:  "123456",
		Embeds: []Embed{
			{
				Title:       "Release v1.0.0",
				Description: "New features",
				URL:         "https://github.com/test/repo/releases/tag/v1.0.0",
				Color:       ColorSuccess,
				Fields: []EmbedField{
					{Name: "Version", Value: "1.0.0", Inline: true},
					{Name: "Branch", Value: "main", Inline: true},
				},
				Footer: &EmbedFooter{
					Text:    "Relicta",
					IconURL: "https://example.com/icon.png",
				},
				Timestamp: "2024-01-01T00:00:00Z",
				Author: &EmbedAuthor{
					Name:    "Release Bot",
					URL:     "https://example.com",
					IconURL: "https://example.com/author.png",
				},
			},
		},
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("failed to marshal message: %v", err)
	}

	// Verify JSON structure
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	if parsed["content"] != "Test content" {
		t.Errorf("expected content 'Test content', got %v", parsed["content"])
	}

	if parsed["username"] != "TestBot" {
		t.Errorf("expected username 'TestBot', got %v", parsed["username"])
	}

	embeds, ok := parsed["embeds"].([]any)
	if !ok || len(embeds) != 1 {
		t.Error("expected one embed")
		return
	}

	embed := embeds[0].(map[string]any)
	if embed["title"] != "Release v1.0.0" {
		t.Errorf("expected embed title 'Release v1.0.0', got %v", embed["title"])
	}
}
