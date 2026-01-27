package builtin

import (
	"context"
	"fmt"
	"io"

	"github.com/rashpile/pako-telegram/internal/version"
	pkgcmd "github.com/rashpile/pako-telegram/pkg/command"
)

// VersionCommand shows the current bot version.
type VersionCommand struct{}

// NewVersionCommand creates a version command.
func NewVersionCommand() *VersionCommand {
	return &VersionCommand{}
}

// Name returns "version".
func (v *VersionCommand) Name() string {
	return "version"
}

// Description returns the version command description.
func (v *VersionCommand) Description() string {
	return "Show current bot version"
}

// Category returns the command's category for menu grouping.
func (v *VersionCommand) Category() pkgcmd.CategoryInfo {
	return pkgcmd.CategoryInfo{
		Name: "system",
		Icon: "ℹ️",
	}
}

// Execute writes the version information.
func (v *VersionCommand) Execute(ctx context.Context, args []string, output io.Writer) error {
	fmt.Fprintf(output, "Version:    %s\n", version.Version)
	fmt.Fprintf(output, "Commit:     %s\n", version.Commit)
	fmt.Fprintf(output, "Build Date: %s\n", version.BuildDate)
	return nil
}