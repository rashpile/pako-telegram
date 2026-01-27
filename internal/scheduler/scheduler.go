// Package scheduler provides scheduled command execution at specified times.
package scheduler

import (
	"context"
	"log/slog"
	"sync"
	"time"

	pkgcmd "github.com/rashpile/pako-telegram/pkg/command"
)

// TimeOfDay represents a time in HH:MM format.
type TimeOfDay struct {
	Hour   int
	Minute int
}

// ScheduledCommand represents a command with its schedule configuration.
type ScheduledCommand struct {
	Name          string
	Times         []TimeOfDay   // Time-of-day scheduling (e.g., 09:00, 18:00)
	Interval      time.Duration // Interval scheduling (e.g., 5m)
	InitialPaused bool          // Start with schedule paused
	Command       pkgcmd.Command
	lastRun       time.Time // For interval scheduling
}

// CommandExecutor executes commands and sends output to chats.
type CommandExecutor interface {
	ExecuteScheduled(ctx context.Context, chatID int64, cmd pkgcmd.Command) error
}

// Config holds scheduler dependencies.
type Config struct {
	ChatIDs  []int64
	Executor CommandExecutor
}

// Scheduler manages scheduled command execution.
type Scheduler struct {
	chatIDs  []int64
	executor CommandExecutor
	commands []ScheduledCommand
	paused   map[string]bool // paused command names
	mu       sync.RWMutex
	wakeup   chan struct{} // signal to recalculate next execution
}

// New creates a scheduler with the given configuration.
func New(cfg Config) *Scheduler {
	return &Scheduler{
		chatIDs:  cfg.ChatIDs,
		executor: cfg.Executor,
		paused:   make(map[string]bool),
		wakeup:   make(chan struct{}, 1),
	}
}

// IsPaused returns true if the command is paused.
func (s *Scheduler) IsPaused(name string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.paused[name]
}

// SetPaused sets the paused state for a command.
func (s *Scheduler) SetPaused(name string, paused bool) {
	s.mu.Lock()
	if paused {
		s.paused[name] = true
	} else {
		delete(s.paused, name)
	}
	s.mu.Unlock()

	// Signal to recalculate next execution
	select {
	case s.wakeup <- struct{}{}:
	default:
	}

	slog.Info("scheduler command paused state changed", "command", name, "paused", paused)
}

// UpdateCommands updates the list of scheduled commands.
// This can be called when YAML commands are reloaded.
func (s *Scheduler) UpdateCommands(commands []ScheduledCommand) {
	s.mu.Lock()

	// Build map of existing commands to preserve state
	existing := make(map[string]*ScheduledCommand)
	for i := range s.commands {
		existing[s.commands[i].Name] = &s.commands[i]
	}

	// Process new commands
	for i := range commands {
		cmd := &commands[i]
		if old, found := existing[cmd.Name]; found {
			// Preserve lastRun for existing interval commands
			cmd.lastRun = old.lastRun
		} else {
			// New command - apply InitialPaused if set
			if cmd.InitialPaused {
				s.paused[cmd.Name] = true
			}
		}
	}

	s.commands = commands
	s.mu.Unlock()

	// Signal to recalculate next execution
	select {
	case s.wakeup <- struct{}{}:
	default:
	}

	slog.Info("scheduler commands updated", "count", len(commands))
}

// Run starts the scheduler. Blocks until context is cancelled.
func (s *Scheduler) Run(ctx context.Context) error {
	slog.Info("scheduler started")

	for {
		// Get next execution time
		nextTime, cmd := s.nextExecution()

		if cmd == nil {
			// No scheduled commands, wait for update or cancellation
			select {
			case <-ctx.Done():
				slog.Info("scheduler stopped")
				return ctx.Err()
			case <-s.wakeup:
				continue
			}
		}

		waitDuration := time.Until(nextTime)
		if waitDuration < 0 {
			waitDuration = 0
		}

		slog.Debug("scheduler waiting",
			"command", cmd.Name,
			"next_run", nextTime.Format("15:04:05"),
			"wait", waitDuration.Round(time.Second),
		)

		select {
		case <-ctx.Done():
			slog.Info("scheduler stopped")
			return ctx.Err()

		case <-s.wakeup:
			// Commands updated, recalculate
			continue

		case <-time.After(waitDuration):
			// Execute the command
			s.executeForAllChats(ctx, cmd)
		}
	}
}

// nextExecution finds the earliest next execution time across all commands.
func (s *Scheduler) nextExecution() (time.Time, *ScheduledCommand) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.commands) == 0 {
		return time.Time{}, nil
	}

	now := time.Now()
	var earliest time.Time
	var earliestCmd *ScheduledCommand

	for i := range s.commands {
		cmd := &s.commands[i]

		// Skip paused commands
		if s.paused[cmd.Name] {
			continue
		}

		// Handle interval scheduling
		if cmd.Interval > 0 {
			var nextRun time.Time
			if cmd.lastRun.IsZero() {
				// First run: execute immediately
				nextRun = now
			} else {
				nextRun = cmd.lastRun.Add(cmd.Interval)
			}
			if earliest.IsZero() || nextRun.Before(earliest) {
				earliest = nextRun
				earliestCmd = cmd
			}
			continue
		}

		// Handle time-of-day scheduling
		for _, t := range cmd.Times {
			nextRun := nextTimeOfDay(now, t)
			if earliest.IsZero() || nextRun.Before(earliest) {
				earliest = nextRun
				earliestCmd = cmd
			}
		}
	}

	return earliest, earliestCmd
}

// nextTimeOfDay calculates the next occurrence of a time of day.
func nextTimeOfDay(now time.Time, tod TimeOfDay) time.Time {
	// Create time for today at the specified hour:minute
	next := time.Date(
		now.Year(), now.Month(), now.Day(),
		tod.Hour, tod.Minute, 0, 0,
		now.Location(),
	)

	// If the time has already passed today, schedule for tomorrow
	if !next.After(now) {
		next = next.Add(24 * time.Hour)
	}

	return next
}

// executeForAllChats runs the command and sends output to all configured chats.
func (s *Scheduler) executeForAllChats(ctx context.Context, cmd *ScheduledCommand) {
	slog.Info("executing scheduled command", "command", cmd.Name)

	// Update lastRun for interval commands
	if cmd.Interval > 0 {
		s.mu.Lock()
		cmd.lastRun = time.Now()
		s.mu.Unlock()
	}

	for _, chatID := range s.chatIDs {
		if err := s.executor.ExecuteScheduled(ctx, chatID, cmd.Command); err != nil {
			slog.Error("scheduled command failed",
				"command", cmd.Name,
				"chat_id", chatID,
				"error", err,
			)
		}
	}
}

// ParseTime parses a time string in "HH:MM" format.
func ParseTime(s string) (TimeOfDay, error) {
	if len(s) != 5 || s[2] != ':' {
		return TimeOfDay{}, &ParseError{Input: s, Message: "must be in HH:MM format"}
	}

	// Validate all characters are digits (except colon)
	for i, c := range s {
		if i == 2 {
			continue
		}
		if c < '0' || c > '9' {
			return TimeOfDay{}, &ParseError{Input: s, Message: "must be in HH:MM format"}
		}
	}

	hour := (int(s[0]-'0') * 10) + int(s[1]-'0')
	minute := (int(s[3]-'0') * 10) + int(s[4]-'0')

	if hour > 23 {
		return TimeOfDay{}, &ParseError{Input: s, Message: "hour must be 00-23"}
	}
	if minute > 59 {
		return TimeOfDay{}, &ParseError{Input: s, Message: "minute must be 00-59"}
	}

	return TimeOfDay{Hour: hour, Minute: minute}, nil
}

// ParseTimes parses a list of time strings.
func ParseTimes(times []string) ([]TimeOfDay, error) {
	result := make([]TimeOfDay, len(times))
	for i, t := range times {
		tod, err := ParseTime(t)
		if err != nil {
			return nil, err
		}
		result[i] = tod
	}
	return result, nil
}

// ParseError represents a time parsing error.
type ParseError struct {
	Input   string
	Message string
}

func (e *ParseError) Error() string {
	return e.Message
}
