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
- Scheduled commands - run at specific times or intervals
- File output format - send files to Telegram from command output
- Media group support - send multiple files as albums
- Cleanup functionality - delete previously sent files from chat

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

# Optional: Enable cleanup functionality to delete sent files
# Path is relative to config file location, or use absolute path
message_store_path: "messages.json"  # Creates alongside config.yaml

defaults:
  timeout: 60s
  max_output: 5000
  max_files_per_group: 10  # Max files per Telegram media group
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
workdir: "/path/to/dir"  # Working directory for command execution
timeout: 300s          # Max execution time
max_output: 10000      # Max output characters
confirm: true          # Require confirmation before running
category: deploy       # Category for menu grouping
icon: "🚀"             # Emoji icon for menu

# Scheduling options (mutually exclusive)
schedule:              # Run at specific times (HH:MM format)
  - "09:00"
  - "18:00"
interval: 5m           # Run every X duration (e.g., 5m, 1h)
initial_paused: false  # Start with schedule paused (default: false)
quiet: false           # Suppress "Running..." messages (default: false)
```

## File Output Format

Commands can send files to Telegram by outputting special file references:

```bash
echo "Here's your file:"
echo "[file:/path/to/document.pdf]"
```

**Features:**
- Multiple files are sent as a Telegram media group (album)
- Text before file references becomes the caption
- File types are auto-detected (photo, video, audio, document)
- Relative paths are resolved against `workdir`

**Example command:**
```yaml
name: random-image
description: "Send a random image"
workdir: "/path/to/images"
command: |
  images=(*.jpg *.png)
  selected="${images[$((RANDOM % ${#images[@]}))]}"
  echo "Random image:"
  echo "[file:$selected]"
category: media
icon: "🖼️"
```

## Scheduled Commands

Commands can run automatically at specific times or intervals:

**Time-of-day scheduling:**
```yaml
name: daily-report
command: "./generate-report.sh"
schedule:
  - "09:00"
  - "18:00"
```

**Interval scheduling:**
```yaml
name: health-check
command: "./check-health.sh"
interval: 5m
```

**Features:**
- Scheduled commands run for all allowed chat IDs
- When you invoke a scheduled command manually (e.g., `/health-check`), a menu appears with options to:
  - **Run now** - Execute the command immediately
  - **Pause/Resume schedule** - Toggle automatic execution
- Pause state is kept in memory (resets on bot restart)
- Use `initial_paused: true` to start commands paused
- Use `quiet: true` to suppress "Running /cmd..." messages and hide file-only output text

**Quiet mode** is useful for file-generating commands where you only want to see the file, not the output text:
```yaml
name: daily-image
command: "echo '[file:/path/to/image.jpg]'"
interval: 1h
quiet: true  # Only sends the file, no "Running..." or "[file:...]" text
```

**Note:** Commands with arguments cannot be scheduled.

## Cleanup

When `message_store_path` is configured, the bot tracks all sent file messages and provides a cleanup menu to delete them:

- **Last hour** - Delete files sent in the last hour
- **Last 24 hours** - Delete files sent in the last day
- **Older than 1 day/week/month** - Delete older files
- **All files** - Delete all tracked files

The cleanup button appears in the main menu when enabled.

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
