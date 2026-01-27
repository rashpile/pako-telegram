package command

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/rashpile/pako-telegram/internal/config"
	pkgcmd "github.com/rashpile/pako-telegram/pkg/command"
)

// ArgumentDef represents a command argument definition.
type ArgumentDef struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Required    bool     `yaml:"required"`
	Type        string   `yaml:"type"` // string, int, bool, choice
	Choices     []string `yaml:"choices"`
	Default     string   `yaml:"default"`
	Sensitive   bool     `yaml:"sensitive"`
}

// YAMLCommandDef represents a shell command definition from YAML.
type YAMLCommandDef struct {
	Name            string        `yaml:"name"`
	Description     string        `yaml:"description"`
	Command         string        `yaml:"command"`
	Workdir         string        `yaml:"workdir"`
	Timeout         time.Duration `yaml:"timeout"`
	MaxOutput       int           `yaml:"max_output"`
	Confirm         bool          `yaml:"confirm"`
	Category        string        `yaml:"category"`
	Icon            string        `yaml:"icon"`
	Arguments       []ArgumentDef `yaml:"arguments"`
	ArgumentTimeout time.Duration `yaml:"argument_timeout"`
	Schedule        []string      `yaml:"schedule"` // List of "HH:MM" times for scheduled execution
}

// YAMLCommand is a Command implementation backed by a shell command.
type YAMLCommand struct {
	def      YAMLCommandDef
	executor Executor
}

// ExecuteConfig holds parameters for command execution.
type ExecuteConfig struct {
	Command string
	Args    []string
	Output  io.Writer
	Workdir string
}

// Executor runs shell commands. Injected to allow testing.
type Executor interface {
	Execute(ctx context.Context, cfg ExecuteConfig) error
}

// Name returns the command name.
func (y *YAMLCommand) Name() string {
	return y.def.Name
}

// Description returns the command description.
func (y *YAMLCommand) Description() string {
	return y.def.Description
}

// Execute runs the shell command with arguments.
func (y *YAMLCommand) Execute(ctx context.Context, args []string, output io.Writer) error {
	return y.executor.Execute(ctx, ExecuteConfig{
		Command: y.def.Command,
		Args:    args,
		Output:  output,
		Workdir: y.def.Workdir,
	})
}

// Metadata returns command configuration.
func (y *YAMLCommand) Metadata() pkgcmd.Metadata {
	return pkgcmd.Metadata{
		Timeout:        y.def.Timeout,
		MaxOutput:      y.def.MaxOutput,
		RequireConfirm: y.def.Confirm,
	}
}

// Category returns the command's category for menu grouping.
func (y *YAMLCommand) Category() pkgcmd.CategoryInfo {
	return pkgcmd.CategoryInfo{
		Name: y.def.Category,
		Icon: y.def.Icon,
	}
}

// Arguments returns the command's argument definitions.
func (y *YAMLCommand) Arguments() []ArgumentDef {
	return y.def.Arguments
}

// ArgumentTimeout returns the timeout for argument collection.
func (y *YAMLCommand) ArgumentTimeout() time.Duration {
	return y.def.ArgumentTimeout
}

// HasArguments returns true if the command has defined arguments.
func (y *YAMLCommand) HasArguments() bool {
	return len(y.def.Arguments) > 0
}

// CommandTemplate returns the raw command template string.
func (y *YAMLCommand) CommandTemplate() string {
	return y.def.Command
}

// ExecuteRendered runs a pre-rendered command string.
func (y *YAMLCommand) ExecuteRendered(ctx context.Context, rendered string, output io.Writer) error {
	return y.executor.Execute(ctx, ExecuteConfig{
		Command: rendered,
		Output:  output,
		Workdir: y.def.Workdir,
	})
}

// Workdir returns the command's working directory.
func (y *YAMLCommand) Workdir() string {
	return y.def.Workdir
}

// FileResponse returns nil for basic YAML commands.
// Commands that support file responses should embed this type.
func (y *YAMLCommand) FileResponse() *pkgcmd.FileResponse {
	return nil
}

// Schedule returns the list of scheduled times for this command.
func (y *YAMLCommand) Schedule() []string {
	return y.def.Schedule
}

// Loader loads YAML command definitions from a directory.
type Loader struct {
	dir      string
	defaults config.DefaultsConfig
	executor Executor
}

// NewLoader creates a YAML command loader.
func NewLoader(dir string, defaults config.DefaultsConfig, executor Executor) *Loader {
	return &Loader{
		dir:      dir,
		defaults: defaults,
		executor: executor,
	}
}

// Load reads all .yaml files from the configured directory and subdirectories.
func (l *Loader) Load() ([]pkgcmd.Command, error) {
	if _, err := os.Stat(l.dir); os.IsNotExist(err) {
		return nil, nil // No commands directory is OK
	}

	var commands []pkgcmd.Command
	err := filepath.WalkDir(l.dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		ext := filepath.Ext(d.Name())
		if ext != ".yaml" && ext != ".yml" {
			return nil
		}

		cmd, err := l.loadFile(path)
		if err != nil {
			return fmt.Errorf("load %s: %w", path, err)
		}

		commands = append(commands, cmd)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk commands directory: %w", err)
	}

	return commands, nil
}

// loadFile parses a single YAML command file.
func (l *Loader) loadFile(path string) (*YAMLCommand, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var def YAMLCommandDef
	if err := yaml.Unmarshal(data, &def); err != nil {
		return nil, fmt.Errorf("parse yaml: %w", err)
	}

	if def.Name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if def.Command == "" {
		return nil, fmt.Errorf("command is required")
	}

	// Validate schedule
	if len(def.Schedule) > 0 {
		if len(def.Arguments) > 0 {
			return nil, fmt.Errorf("commands with arguments cannot be scheduled")
		}
		for _, t := range def.Schedule {
			if err := validateTimeFormat(t); err != nil {
				return nil, fmt.Errorf("invalid schedule time %q: %w", t, err)
			}
		}
	}

	// Apply defaults
	if def.Timeout == 0 {
		def.Timeout = l.defaults.Timeout
	}
	if def.MaxOutput == 0 {
		def.MaxOutput = l.defaults.MaxOutput
	}
	if def.Description == "" {
		def.Description = def.Command
	}

	return &YAMLCommand{
		def:      def,
		executor: l.executor,
	}, nil
}

// validateTimeFormat validates a time string in "HH:MM" format.
func validateTimeFormat(t string) error {
	if len(t) != 5 || t[2] != ':' {
		return fmt.Errorf("must be in HH:MM format")
	}

	hour := (int(t[0]-'0') * 10) + int(t[1]-'0')
	minute := (int(t[3]-'0') * 10) + int(t[4]-'0')

	// Validate digits
	for i, c := range t {
		if i == 2 {
			continue // skip colon
		}
		if c < '0' || c > '9' {
			return fmt.Errorf("must be in HH:MM format")
		}
	}

	if hour < 0 || hour > 23 {
		return fmt.Errorf("hour must be 00-23")
	}
	if minute < 0 || minute > 59 {
		return fmt.Errorf("minute must be 00-59")
	}

	return nil
}
