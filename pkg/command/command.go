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

// CategoryInfo holds category metadata for menu organization.
type CategoryInfo struct {
	Name string // Category name (e.g., "system", "deploy")
	Icon string // Emoji icon (e.g., "📊", "🚀")
}

// WithCategory extends Command with category information for menu grouping.
type WithCategory interface {
	Command
	Category() CategoryInfo
}

// FileResponse indicates a command wants to send a file after execution.
type FileResponse struct {
	Path    string // Path to the file to send
	Caption string // Optional caption for the file
	Cleanup bool   // If true, delete file after sending
}

// WithFileResponse extends Command for commands that return files.
// After Execute() completes, the bot checks FileResponse() and sends the file.
type WithFileResponse interface {
	Command
	FileResponse() *FileResponse
}
