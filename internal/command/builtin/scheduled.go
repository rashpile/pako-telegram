package builtin

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/rashpile/pako-telegram/internal/scheduler"
	pkgcmd "github.com/rashpile/pako-telegram/pkg/command"
)

// ScheduleLister provides a list of active scheduled commands.
type ScheduleLister interface {
	ListActive() []scheduler.ActiveCommandInfo
}

// ScheduledCommand shows active scheduled commands and their next run times.
type ScheduledCommand struct {
	lister ScheduleLister
}

// NewScheduledCommand creates a scheduled command.
func NewScheduledCommand() *ScheduledCommand {
	return &ScheduledCommand{}
}

// SetScheduleLister sets the schedule lister.
func (s *ScheduledCommand) SetScheduleLister(lister ScheduleLister) {
	s.lister = lister
}

// Name returns "scheduled".
func (s *ScheduledCommand) Name() string {
	return "scheduled"
}

// Description returns the scheduled command description.
func (s *ScheduledCommand) Description() string {
	return "Show active scheduled commands"
}

// Category returns the command's category for menu grouping.
func (s *ScheduledCommand) Category() pkgcmd.CategoryInfo {
	return pkgcmd.CategoryInfo{
		Name: "system",
		Icon: "📅",
	}
}

// Execute lists active scheduled commands with their next run times.
func (s *ScheduledCommand) Execute(ctx context.Context, args []string, output io.Writer) error {
	if s.lister == nil {
		fmt.Fprintln(output, "Scheduler not available.")
		return nil
	}

	active := s.lister.ListActive()
	if len(active) == 0 {
		fmt.Fprintln(output, "No active scheduled commands.")
		return nil
	}

	fmt.Fprintln(output, "Active scheduled commands:")
	fmt.Fprintln(output, "")

	for _, cmd := range active {
		// Format next run time
		until := time.Until(cmd.NextRun)
		var nextStr string
		if until < time.Minute {
			nextStr = fmt.Sprintf("in %ds", int(until.Seconds()))
		} else if until < time.Hour {
			nextStr = fmt.Sprintf("in %dm", int(until.Minutes()))
		} else if until < 24*time.Hour {
			nextStr = fmt.Sprintf("in %dh %dm", int(until.Hours()), int(until.Minutes())%60)
		} else {
			nextStr = cmd.NextRun.Format("Mon 15:04")
		}

		// Format schedule type
		var schedType string
		if cmd.Interval > 0 {
			schedType = fmt.Sprintf("every %s", cmd.Interval)
		} else if len(cmd.Times) > 0 {
			schedType = fmt.Sprintf("at %s", cmd.Times[0])
			if len(cmd.Times) > 1 {
				schedType += fmt.Sprintf(" (+%d more)", len(cmd.Times)-1)
			}
		}

		fmt.Fprintf(output, "/%s\n", cmd.Name)
		fmt.Fprintf(output, "  %s, next: %s\n", schedType, nextStr)
		fmt.Fprintln(output, "")
	}

	return nil
}
