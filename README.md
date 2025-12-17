# Relicta Discord Plugin

Official Discord plugin for [Relicta](https://github.com/relicta-tech/relicta) - AI-powered release management.

## Features

- Send release notifications to Discord channels
- Rich embed message formatting
- Configurable success/error notifications
- User/role mentions support
- Thread posting support
- Custom embed colors

## Installation

```bash
relicta plugin install discord
relicta plugin enable discord
```

## Configuration

Add to your `release.config.yaml`:

```yaml
plugins:
  - name: discord
    enabled: true
    config:
      username: "Release Bot"
      notify_on_success: true
      notify_on_error: true
      include_changelog: true
      mentions:
        - "<@123456789>"  # User mention
        - "<@&987654321>" # Role mention
```

### Environment Variables

- `DISCORD_WEBHOOK_URL` - Discord webhook URL (required)

### Configuration Options

| Option | Description | Default |
|--------|-------------|---------|
| `webhook` | Discord webhook URL (prefer using env var) | - |
| `username` | Bot username | `Relicta` |
| `avatar_url` | Bot avatar URL | - |
| `notify_on_success` | Send notification on success | `true` |
| `notify_on_error` | Send notification on error | `true` |
| `include_changelog` | Include changelog in embed | `false` |
| `mentions` | Users/roles to mention | - |
| `thread_id` | Thread ID to post to | - |
| `color` | Embed color (decimal) | Status-based |

## Creating a Webhook

1. Open Discord and go to Server Settings
2. Navigate to Integrations → Webhooks
3. Click "New Webhook"
4. Configure name and channel
5. Copy the webhook URL and set it as `DISCORD_WEBHOOK_URL`

## Mention Formats

- User: `<@user_id>` (e.g., `<@123456789012345678>`)
- Role: `<@&role_id>` (e.g., `<@&987654321098765432>`)

## Hooks

This plugin responds to the following hooks:

- `post_publish` - Sends success notification
- `on_success` - Sends success notification
- `on_error` - Sends error notification

## Development

```bash
# Build
go build -o discord .

# Test locally
relicta plugin install ./discord
relicta plugin enable discord
relicta publish --dry-run
```

## License

MIT License - see [LICENSE](LICENSE) for details.
