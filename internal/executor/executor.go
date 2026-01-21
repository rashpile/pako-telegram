// Package executor provides shell command execution with streaming output.
package executor

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/rashpile/pako-telegram/internal/command"
)

// ShellExecutor runs commands via /bin/sh -c.
type ShellExecutor struct{}

// NewShellExecutor creates a shell executor.
func NewShellExecutor() *ShellExecutor {
	return &ShellExecutor{}
}

// Execute runs a shell command with the provided configuration.
func (e *ShellExecutor) Execute(ctx context.Context, cfg command.ExecuteConfig) error {
	// Build full command with arguments
	fullCmd := cfg.Command
	if len(cfg.Args) > 0 {
		fullCmd = cfg.Command + " " + strings.Join(cfg.Args, " ")
	}

	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", fullCmd)
	cmd.Stdout = cfg.Output
	cmd.Stderr = cfg.Output

	// Set working directory if specified
	if cfg.Workdir != "" {
		cmd.Dir = cfg.Workdir
	}

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("command timed out or cancelled")
		}
		return fmt.Errorf("command failed: %w", err)
	}

	return nil
}
