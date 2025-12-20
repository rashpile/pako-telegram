// Package builtin provides built-in commands like /help, /status, /reload.
package builtin

import (
	"context"
	"fmt"
	"io"
	"sort"

	pkgcmd "github.com/rashpile/pako-telegram/pkg/command"
)

// CommandLister returns all available commands.
type CommandLister interface {
	All() []pkgcmd.Command
}

// HelpCommand lists all available commands.
type HelpCommand struct {
	lister CommandLister
}

// NewHelpCommand creates a help command.
func NewHelpCommand(lister CommandLister) *HelpCommand {
	return &HelpCommand{lister: lister}
}

// Name returns "help".
func (h *HelpCommand) Name() string {
	return "help"
}

// Description returns the help description.
func (h *HelpCommand) Description() string {
	return "List available commands"
}

// Execute writes the list of commands to output.
func (h *HelpCommand) Execute(ctx context.Context, args []string, output io.Writer) error {
	commands := h.lister.All()

	// Sort by name
	sort.Slice(commands, func(i, j int) bool {
		return commands[i].Name() < commands[j].Name()
	})

	fmt.Fprintln(output, "Available commands:")
	fmt.Fprintln(output)

	for _, cmd := range commands {
		fmt.Fprintf(output, "/%s - %s\n", cmd.Name(), cmd.Description())
	}

	return nil
}
