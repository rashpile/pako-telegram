package command

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/rashpile/pako-telegram/internal/config"
	pkgcmd "github.com/rashpile/pako-telegram/pkg/command"
)

// YAMLCommandDef represents a shell command definition from YAML.
type YAMLCommandDef struct {
	Name        string        `yaml:"name"`
	Description string        `yaml:"description"`
	Command     string        `yaml:"command"`
	Timeout     time.Duration `yaml:"timeout"`
	MaxOutput   int           `yaml:"max_output"`
	Confirm     bool          `yaml:"confirm"`
	Category    string        `yaml:"category"`
	Icon        string        `yaml:"icon"`
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

// Load reads all .yaml files from the configured directory.
func (l *Loader) Load() ([]pkgcmd.Command, error) {
	entries, err := os.ReadDir(l.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // No commands directory is OK
		}
		return nil, fmt.Errorf("read commands directory: %w", err)
	}

	var commands []pkgcmd.Command
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		ext := filepath.Ext(entry.Name())
		if ext != ".yaml" && ext != ".yml" {
			continue
		}

		path := filepath.Join(l.dir, entry.Name())
		cmd, err := l.loadFile(path)
		if err != nil {
			return nil, fmt.Errorf("load %s: %w", entry.Name(), err)
		}

		commands = append(commands, cmd)
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
