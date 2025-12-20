# pako-telegram

## Project Definition

Build an internal Telegram bot that executes configurable shell commands on the host laptop and streams output back to the user. Commands are defined via YAML files (one per command) with support for Go interface-based plugins for complex tasks. Security enforced via chat ID allowlist.

## Requirements

### Functional

- Execute shell commands with positional arguments (`/cmd arg1 arg2`)
- Stream command output in real-time chunks (progressive Telegram messages)
- Load command definitions from `commands/*.yaml` directory
- Support Go plugins implementing a `Command` interface for complex logic
- Hot-reload command configs via `/reload` without restart
- Interactive confirmations (inline keyboards) for commands marked `confirm: true`
- Audit log all executions to SQLite (timestamp, chat_id, command, args, exit_code, duration)
- Built-in `/status` command: CPU %, memory usage, disk space
- Built-in `/help` command: list all available commands with descriptions
- Built-in `/reload` command: hot-reload YAML configurations

### Non-Functional

- Chat ID allowlist authentication (reject unauthorized users)
- Per-command configurable timeout and max output size
- Markdown formatting for output (code blocks)
- Graceful error handling, exit on fatal errors (systemd restarts)
- Long-running daemon mode

## Technical Specification

### Architecture

Modular monolith with clear separation:

```
cmd/pako-telegram/      # Entry point
internal/
  bot/                  # Telegram handler, message routing
  auth/                 # Chat ID allowlist validation
  command/              # Command interface, registry, YAML loader
  executor/             # Shell execution, streaming, timeouts
  plugin/               # Plugin loader
  audit/                # SQLite audit logging
  status/               # System metrics (CPU, RAM, disk)
  config/               # Configuration loading
pkg/                    # Public interfaces (Command interface for plugins)
```

### Technology Stack

- **Language**: Go 1.25+
- **Telegram**: telegram-bot-api/v5 or equivalent
- **Database**: SQLite (modernc.org/sqlite - pure Go)
- **Config**: YAML (gopkg.in/yaml.v3)
- **System metrics**: gopsutil/v3

### Data Model

**Command Config YAML** (`commands/<name>.yaml`):
```yaml
name: deploy
description: "Deploy application to production"
command: "/opt/scripts/deploy.sh"
timeout: 300s
max_output: 10000
confirm: true
```

**Main Config** (`config.yaml`):
```yaml
telegram:
  token: "${BOT_TOKEN}"
  allowed_chat_ids:
    - 123456789

commands_dir: "./commands"
plugins_dir: "./plugins"

database:
  path: "./audit.db"

defaults:
  timeout: 60s
  max_output: 5000
```

**Audit Log Schema**:
```sql
CREATE TABLE audit_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
    chat_id INTEGER NOT NULL,
    username TEXT,
    command TEXT NOT NULL,
    args TEXT,
    exit_code INTEGER,
    duration_ms INTEGER
);
CREATE INDEX idx_audit_timestamp ON audit_log(timestamp);
CREATE INDEX idx_audit_chat_id ON audit_log(chat_id);
```

**Plugin Interface** (`pkg/command/command.go`):

```go
type Command interface {
    Name() string
    Description() string
    Execute(ctx context.Context, args []string, output io.Writer) error
}
```

### Integrations

- **Telegram Bot API**: Long polling for updates
- **Local shell**: Execute via `/bin/sh -c` with configurable timeout

## UI/UX

- Output formatted as Markdown code blocks
- Inline keyboards for confirmation dialogs (Confirm/Cancel buttons)
- Streaming: edit message progressively or send multiple messages for long output
- Truncate with "[output truncated]" if exceeds max_output

## Deployment

- **Runtime**: Long-running daemon (systemd on Linux, launchd on macOS)
- **Config paths**:
  - Config: `~/.config/pako-telegram/config.yaml`
  - Commands: `~/.config/pako-telegram/commands/`
  - Plugins: `~/.config/pako-telegram/plugins/`
  - Database: `~/.local/state/pako-telegram/audit.db`
- **Logging**: stderr (captured by service manager)
- **Service file**: Provide example systemd unit and launchd plist

## Constraints

- Single user, single machine deployment
- No webhook support (long polling only)
- Commands defined by owner are trusted
- Requires network access to Telegram API

## Acceptance Criteria

1. Bot connects to Telegram and responds to `/help`
2. `/status` returns CPU, memory, and disk usage formatted in Markdown
3. Shell commands defined in YAML execute and stream output
4. Unauthoriz[README.md](README.md)ed chat IDs receive rejection message
5. Commands with `confirm: true` show inline keyboard before execution
6. `/reload` picks up new/modified YAML files without restart
7. All command executions logged to SQLite with correct metadata
8. Commands respect configured timeout (process killed on expiry)
9. Go plugins loaded at startup and callable via Telegram
10. Daemon runs stable under systemd/launchd with proper signal handling