// Package command provides command registration and lookup.
package command

import (
	"sort"
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

	// Preserve built-in commands (help, status, reload, version)
	builtins := []string{"help", "status", "reload", "version"}
	for _, name := range builtins {
		if cmd, ok := r.commands[name]; ok {
			newCommands[name] = cmd
		}
	}

	r.commands = newCommands
}

// CategoryWithCommands holds a category and its commands.
type CategoryWithCommands struct {
	Name     string
	Icon     string
	Commands []pkgcmd.Command
}

// Categories returns commands grouped by category, sorted alphabetically.
// Commands without a category are grouped under "other".
func (r *Registry) Categories() []CategoryWithCommands {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Group commands by category
	groups := make(map[string]*CategoryWithCommands)

	for _, cmd := range r.commands {
		catName := "other"
		catIcon := ""

		if withCat, ok := cmd.(pkgcmd.WithCategory); ok {
			info := withCat.Category()
			if info.Name != "" {
				catName = info.Name
				catIcon = info.Icon
			}
		}

		group, exists := groups[catName]
		if !exists {
			group = &CategoryWithCommands{
				Name: catName,
				Icon: catIcon,
			}
			groups[catName] = group
		}
		// Update icon if we have one and the group doesn't
		if catIcon != "" && group.Icon == "" {
			group.Icon = catIcon
		}
		group.Commands = append(group.Commands, cmd)
	}

	// Convert to slice and sort
	result := make([]CategoryWithCommands, 0, len(groups))
	for _, group := range groups {
		// Sort commands within category
		sort.Slice(group.Commands, func(i, j int) bool {
			return group.Commands[i].Name() < group.Commands[j].Name()
		})
		result = append(result, *group)
	}

	// Sort categories (other always last)
	sort.Slice(result, func(i, j int) bool {
		if result[i].Name == "other" {
			return false
		}
		if result[j].Name == "other" {
			return true
		}
		return result[i].Name < result[j].Name
	})

	return result
}

// ByCategory returns commands in a specific category.
func (r *Registry) ByCategory(category string) []pkgcmd.Command {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var cmds []pkgcmd.Command
	for _, cmd := range r.commands {
		catName := "other"
		if withCat, ok := cmd.(pkgcmd.WithCategory); ok {
			info := withCat.Category()
			if info.Name != "" {
				catName = info.Name
			}
		}

		if catName == category {
			cmds = append(cmds, cmd)
		}
	}

	// Sort by name
	sort.Slice(cmds, func(i, j int) bool {
		return cmds[i].Name() < cmds[j].Name()
	})

	return cmds
}
