// Package executor provides shell command execution with streaming output.
package executor

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// ShellExecutor runs commands via /bin/sh -c.
type ShellExecutor struct{}

// NewShellExecutor creates a shell executor.
func NewShellExecutor() *ShellExecutor {
	return &ShellExecutor{}
}

// Execute runs a shell command with arguments, streaming output to writer.
func (e *ShellExecutor) Execute(ctx context.Context, command string, args []string, output io.Writer) error {
	// Build full command with arguments
	fullCmd := command
	if len(args) > 0 {
		fullCmd = command + " " + strings.Join(args, " ")
	}

	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", fullCmd)
	cmd.Stdout = output
	cmd.Stderr = output

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("command timed out or cancelled")
		}
		return fmt.Errorf("command failed: %w", err)
	}

	return nil
}
