package builtin

import (
	"context"
	"fmt"
	"io"

	"github.com/rashpile/pako-telegram/internal/status"
)

// StatusCommand shows system resource usage.
type StatusCommand struct {
	collector status.Collector
}

// NewStatusCommand creates a status command.
func NewStatusCommand(collector status.Collector) *StatusCommand {
	return &StatusCommand{collector: collector}
}

// Name returns "status".
func (s *StatusCommand) Name() string {
	return "status"
}

// Description returns the status description.
func (s *StatusCommand) Description() string {
	return "Show CPU, memory, and disk usage"
}

// Execute collects and writes system metrics.
func (s *StatusCommand) Execute(ctx context.Context, args []string, output io.Writer) error {
	metrics, err := s.collector.Collect(ctx)
	if err != nil {
		return err
	}

	fmt.Fprintf(output, "System Status\n")
	fmt.Fprintf(output, "─────────────\n\n")

	fmt.Fprintf(output, "CPU:    %5.1f%%\n", metrics.CPUPercent)
	fmt.Fprintf(output, "Memory: %5.1f%% (%s / %s)\n",
		metrics.MemoryPercent,
		formatBytes(metrics.MemoryUsed),
		formatBytes(metrics.MemoryTotal),
	)
	fmt.Fprintf(output, "Disk:   %5.1f%% (%s / %s)\n",
		metrics.DiskPercent,
		formatBytes(metrics.DiskUsed),
		formatBytes(metrics.DiskTotal),
	)

	return nil
}

// formatBytes converts bytes to human-readable format.
func formatBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
