// Package command provides command registration and lookup.
package command

import (
	"sync"

	pkgcmd "github.com/rashpile/pako-telegram/pkg/command"
)

// Registry manages available commands with thread-safe access.
type Registry struct {
	mu       sync.RWMutex
	commands map[string]pkgcmd.Command
}

// NewRegistry creates an empty command registry.
func NewRegistry() *Registry {
	return &Registry{
		commands: make(map[string]pkgcmd.Command),
	}
}

// Register adds a command. Overwrites if name exists.
func (r *Registry) Register(cmd pkgcmd.Command) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.commands[cmd.Name()] = cmd
}

// Get retrieves a command by name. Returns nil if not found.
func (r *Registry) Get(name string) pkgcmd.Command {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.commands[name]
}

// All returns all registered commands (for /help).
func (r *Registry) All() []pkgcmd.Command {
	r.mu.RLock()
	defer r.mu.RUnlock()

	cmds := make([]pkgcmd.Command, 0, len(r.commands))
	for _, cmd := range r.commands {
		cmds = append(cmds, cmd)
	}
	return cmds
}

// Reload atomically replaces all YAML-based commands.
// Built-in commands are preserved.
func (r *Registry) Reload(commands []pkgcmd.Command) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Create new map with provided commands
	newCommands := make(map[string]pkgcmd.Command, len(commands))
	for _, cmd := range commands {
		newCommands[cmd.Name()] = cmd
	}

	// Preserve built-in commands (help, status, reload)
	builtins := []string{"help", "status", "reload"}
	for _, name := range builtins {
		if cmd, ok := r.commands[name]; ok {
			newCommands[name] = cmd
		}
	}

	r.commands = newCommands
}
