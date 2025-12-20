# pako-telegram

Internal Telegram bot for executing ops tasks on your laptop.

## Features

- Execute shell commands via Telegram
- Real-time streaming output
- YAML-based command configuration
- Interactive confirmations for dangerous commands
- Chat ID allowlist security
- Audit logging to SQLite
- Hot-reload configuration

## Prerequisites

- Go 1.25+
- Telegram Bot Token (from [@BotFather](https://t.me/botfather))

## Installation

```bash
make build
cp bin/pako-telegram /usr/local/bin/
```

## Configuration

1. Create config directory:
```bash
mkdir -p ~/.config/pako-telegram/commands
mkdir -p ~/.local/state/pako-telegram
```

2. Create config file (`~/.config/pako-telegram/config.yaml`):
```yaml
telegram:
  token: "${BOT_TOKEN}"
  allowed_chat_ids:
    - YOUR_CHAT_ID  # Get this by messaging @userinfobot

commands_dir: "./commands"

database:
  path: "~/.local/state/pako-telegram/audit.db"

defaults:
  timeout: 60s
  max_output: 5000
```

3. Add commands (`~/.config/pako-telegram/commands/uptime.yaml`):
```yaml
name: uptime
description: "Show system uptime"
command: "uptime"
timeout: 10s
```

## Usage

```bash
# Set your bot token
export BOT_TOKEN="your-telegram-bot-token"

# Run the bot
pako-telegram -config ~/.config/pako-telegram/config.yaml
```

## Built-in Commands

| Command | Description |
|---------|-------------|
| `/help` | List all available commands |
| `/status` | Show CPU, memory, and disk usage |
| `/reload` | Hot-reload command configurations |

## Command YAML Format

```yaml
name: deploy           # Command name (without /)
description: "..."     # Shown in /help
command: "/path/to/script.sh"  # Shell command to execute
timeout: 300s          # Max execution time
max_output: 10000      # Max output characters
confirm: true          # Require confirmation before running
```

## Deployment

### systemd (Linux)

```bash
sudo cp deploy/systemd/pako-telegram.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now pako-telegram@$USER
```

### launchd (macOS)

```bash
cp deploy/launchd/com.pako-telegram.plist ~/Library/LaunchAgents/
launchctl load ~/Library/LaunchAgents/com.pako-telegram.plist
```

## Testing

```bash
make test
```
