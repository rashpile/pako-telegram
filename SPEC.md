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

---

# Feature: Interactive Command Arguments

## Project Definition

Add interactive command arguments to the Telegram bot. When a user invokes a command that has defined arguments, the bot prompts for each argument sequentially, validates input, and executes the shell command with collected values substituted via Go templates.

## Requirements

### Functional

- **Argument Definition in YAML**: Extend `YAMLCommandDef` struct to support an `arguments` array
- **Argument Fields**: Each argument has `name`, `description`, `required`, `type`, `choices`, `default`, `sensitive`
- **Argument Types**: Support `string`, `int`, `bool`, `choice`
- **Sequential Prompting**: After command invocation, bot prompts for each argument one at a time
- **Template Substitution**: Use Go template syntax `{{.argName}}` in command field
- **Choice Presentation**: Display inline keyboard buttons if choices ≤4, otherwise text list
- **Validation**: Validate input against type and choices; re-prompt with error message on invalid input
- **Default Values**: Skip prompting if argument has default and is not required
- **Cancellation**: Support `/cancel` command to abort argument collection mid-flow
- **Timeout**: Abort argument collection after configurable timeout (default 120s)
- **Sensitive Arguments**: When `sensitive: true`, delete user's message after capturing the value
- **Multi-line Input**: Support multi-line text input for string arguments

### Non-Functional

- **State Storage**: In-memory map for pending argument sessions (acceptable to lose state on restart)
- **Concurrency Safety**: Guard state map with mutex for concurrent access
- **Context Propagation**: Pass context through argument collection for proper cancellation
- **Minimal Footprint**: No external dependencies for state storage

## Technical Specification

### Architecture

Extend existing command execution flow:

```
User sends /cmd
    │
    ▼
Registry.Get(cmd) → YAMLCommand
    │
    ▼
Has arguments? ─── No ──→ Execute immediately
    │
   Yes
    │
    ▼
ArgumentCollector.Start(chatID, cmd)
    │
    ▼
Prompt for arg[0]
    │
    ▼
User responds ──→ Validate ──→ Invalid? Re-prompt
    │                              │
   Valid                          │
    │◄─────────────────────────────┘
    ▼
More args? ─── Yes ──→ Prompt for arg[N]
    │
   No
    │
    ▼
Render command template with collected args
    │
    ▼
Execute shell command
```

### Data Model

**Extended YAMLCommandDef:**

```go
type YAMLCommandDef struct {
    Name            string         `yaml:"name"`
    Description     string         `yaml:"description"`
    Command         string         `yaml:"command"`
    Timeout         time.Duration  `yaml:"timeout"`
    MaxOutput       int            `yaml:"max_output"`
    Confirm         bool           `yaml:"confirm"`
    Category        string         `yaml:"category"`
    Icon            string         `yaml:"icon"`
    Arguments       []ArgumentDef  `yaml:"arguments"`
    ArgumentTimeout time.Duration  `yaml:"argument_timeout"`
}

type ArgumentDef struct {
    Name        string   `yaml:"name"`
    Description string   `yaml:"description"`
    Required    bool     `yaml:"required"`
    Type        string   `yaml:"type"`    // string, int, bool, choice
    Choices     []string `yaml:"choices"` // for choice type
    Default     string   `yaml:"default"`
    Sensitive   bool     `yaml:"sensitive"`
}
```

**Argument Session State:**

```go
type ArgumentSession struct {
    ChatID      int64
    Command     *YAMLCommand
    Arguments   []ArgumentDef
    Collected   map[string]string
    CurrentIdx  int
    StartedAt   time.Time
    TimeoutDur  time.Duration
}

type ArgumentCollector struct {
    mu             sync.RWMutex
    sessions       map[int64]*ArgumentSession // keyed by chat ID
    defaultTimeout time.Duration
}
```

### Key Components

1. **ArgumentDef**: Struct for argument definition in YAML
2. **ArgumentSession**: Tracks in-progress argument collection per chat
3. **ArgumentCollector**: Manages sessions, prompting, validation, timeout
4. **TemplateRenderer**: Renders command string with collected arguments

### Validation Logic

| Type | Validation |
|------|------------|
| `string` | Non-empty if required |
| `int` | Parseable as integer |
| `bool` | `true`, `false`, `yes`, `no`, `1`, `0` |
| `choice` | Value exists in choices array |

### Message Flow

**Prompt Message Format:**
```
{argument.description}
```

**Validation Error Format:**
```
Invalid input: {error reason}
{argument.description}
```

**Choice Presentation (≤4 choices):**
- Inline keyboard with one button per choice

**Choice Presentation (>4 choices):**
```
{argument.description}

Options:
1. choice1
2. choice2
3. choice3
...
```

## UI/UX

- Bot prompts appear as regular messages
- Inline keyboard buttons for choices (≤4 options)
- Sensitive input messages deleted immediately after capture
- `/cancel` available at any prompt to abort
- Timeout message: "Argument collection timed out. Please try again."

## Constraints

- Single active argument session per chat (new command cancels pending session)
- State lost on bot restart (acceptable per requirements)
- Go template syntax required in YAML command field
- Maximum argument count not enforced (YAML author responsibility)

## Acceptance Criteria

1. **YAML Parsing**: Bot loads commands with `arguments` field without error
2. **Sequential Prompting**: `/chat` with 2 arguments prompts for each in order
3. **Validation**: Invalid int input re-prompts with error message
4. **Choice Buttons**: Command with ≤4 choices shows inline keyboard
5. **Choice Text**: Command with >4 choices shows numbered text list
6. **Template Rendering**: `{{.argName}}` replaced with collected value in shell command
7. **Cancellation**: `/cancel` during prompting aborts and confirms to user
8. **Timeout**: No response for 120s (default) aborts with timeout message
9. **Sensitive**: Message with sensitive input deleted after capture
10. **Multi-line**: String argument accepts input with newlines
11. **Default Values**: Argument with default skips prompting (unless required without value)
12. **Existing Commands**: Commands without `arguments` field execute immediately as before

## Example YAML

```yaml
name: chat
description: "Chat with AI"
arguments:
  - name: prompt
    description: "Enter your prompt"
    required: true
    type: string
  - name: model
    description: "Select model"
    type: choice
    choices: ["gpt-4", "gpt-3.5", "claude"]
    default: "gpt-4"
argument_timeout: 60s
command: |
  curl -X POST https://api.example.com/chat \
    -d '{"prompt": "{{.prompt}}", "model": "{{.model}}"}'
timeout: 30s
category: ai
icon: "🤖"
```

```yaml
name: deploy
description: "Deploy application"
arguments:
  - name: env
    description: "Select environment"
    required: true
    type: choice
    choices: ["staging", "prod"]
  - name: version
    description: "Enter version (e.g., 1.2.3)"
    required: true
    type: string
  - name: force
    description: "Force deployment? (yes/no)"
    type: bool
    default: "false"
argument_timeout: 120s
command: |
  ./deploy.sh --env={{.env}} --version={{.version}} --force={{.force}}
timeout: 300s
confirm: true
category: deploy
icon: "🚀"
```