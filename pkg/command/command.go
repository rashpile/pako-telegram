// Package command defines the interface for executable commands.
// Implement this interface to create custom commands that can be invoked via Telegram.
package command

import (
	"context"
	"io"
	"time"
)

// Command defines the contract for all executable commands.
// Both YAML-defined shell commands and Go plugins implement this interface.
type Command interface {
	// Name returns the command name without the leading slash (e.g., "deploy").
	Name() string

	// Description returns a human-readable description for /help output.
	Description() string

	// Execute runs the command with given arguments, streaming output to writer.
	// The context carries cancellation signals for timeout/shutdown.
	Execute(ctx context.Context, args []string, output io.Writer) error
}

// Metadata holds optional command configuration.
type Metadata struct {
	Timeout        time.Duration
	MaxOutput      int
	RequireConfirm bool
}

// DefaultMetadata returns sensible defaults for command execution.
func DefaultMetadata() Metadata {
	return Metadata{
		Timeout:        60 * time.Second,
		MaxOutput:      5000,
		RequireConfirm: false,
	}
}

// WithMetadata extends Command with configuration options.
type WithMetadata interface {
	Command
	Metadata() Metadata
}
