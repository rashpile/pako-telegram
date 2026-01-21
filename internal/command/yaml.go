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
	Timeout         time.Duration `yaml:"timeout"`
	MaxOutput       int           `yaml:"max_output"`
	Confirm         bool          `yaml:"confirm"`
	Category        string        `yaml:"category"`
	Icon            string        `yaml:"icon"`
	Arguments       []ArgumentDef `yaml:"arguments"`
	ArgumentTimeout time.Duration `yaml:"argument_timeout"`
}

// YAMLCommand is a Command implementation backed by a shell command.
type YAMLCommand struct {
	def      YAMLCommandDef
	executor Executor
}

// Executor runs shell commands. Injected to allow testing.
type Executor interface {
	Execute(ctx context.Context, command string, args []string, output io.Writer) error
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
	return y.executor.Execute(ctx, y.def.Command, args, output)
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
	return y.executor.Execute(ctx, rendered, nil, output)
}

// FileResponse returns nil for basic YAML commands.
// Commands that support file responses should embed this type.
func (y *YAMLCommand) FileResponse() *pkgcmd.FileResponse {
	return nil
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
