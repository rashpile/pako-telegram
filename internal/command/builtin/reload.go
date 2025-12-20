package builtin

import (
	"context"
	"fmt"
	"io"

	pkgcmd "github.com/rashpile/pako-telegram/pkg/command"
)

// CommandLoader loads commands from configuration.
type CommandLoader interface {
	Load() ([]pkgcmd.Command, error)
}

// CommandReloader replaces commands in the registry.
type CommandReloader interface {
	Reload(commands []pkgcmd.Command)
}

// ReloadCommand reloads YAML command configurations.
type ReloadCommand struct {
	loader   CommandLoader
	reloader CommandReloader
}

// NewReloadCommand creates a reload command.
func NewReloadCommand(loader CommandLoader, reloader CommandReloader) *ReloadCommand {
	return &ReloadCommand{
		loader:   loader,
		reloader: reloader,
	}
}

// Name returns "reload".
func (r *ReloadCommand) Name() string {
	return "reload"
}

// Description returns the reload description.
func (r *ReloadCommand) Description() string {
	return "Reload command configurations"
}

// Execute reloads commands from YAML files.
func (r *ReloadCommand) Execute(ctx context.Context, args []string, output io.Writer) error {
	commands, err := r.loader.Load()
	if err != nil {
		return fmt.Errorf("load commands: %w", err)
	}

	r.reloader.Reload(commands)

	fmt.Fprintf(output, "Reloaded %d commands\n", len(commands))

	return nil
}
