package scheduler

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"

	pkgcmd "github.com/rashpile/pako-telegram/pkg/command"
)

func TestParseTime(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    TimeOfDay
		wantErr bool
	}{
		{"valid morning", "09:00", TimeOfDay{9, 0}, false},
		{"valid evening", "18:30", TimeOfDay{18, 30}, false},
		{"midnight", "00:00", TimeOfDay{0, 0}, false},
		{"end of day", "23:59", TimeOfDay{23, 59}, false},
		{"noon", "12:00", TimeOfDay{12, 0}, false},
		{"invalid hour", "25:00", TimeOfDay{}, true},
		{"invalid minute", "09:60", TimeOfDay{}, true},
		{"missing leading zero", "9:00", TimeOfDay{}, true},
		{"invalid format", "9am", TimeOfDay{}, true},
		{"empty string", "", TimeOfDay{}, true},
		{"too long", "09:00:00", TimeOfDay{}, true},
		{"no colon", "0900", TimeOfDay{}, true},
		{"letters", "ab:cd", TimeOfDay{}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseTime(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseTime(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("ParseTime(%q) = %+v, want %+v", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseTimes(t *testing.T) {
	tests := []struct {
		name    string
		input   []string
		want    []TimeOfDay
		wantErr bool
	}{
		{
			name:  "multiple valid times",
			input: []string{"09:00", "14:30", "18:00"},
			want:  []TimeOfDay{{9, 0}, {14, 30}, {18, 0}},
		},
		{
			name:  "single time",
			input: []string{"12:00"},
			want:  []TimeOfDay{{12, 0}},
		},
		{
			name:  "empty list",
			input: []string{},
			want:  []TimeOfDay{},
		},
		{
			name:    "one invalid time",
			input:   []string{"09:00", "invalid"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseTimes(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseTimes() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if len(got) != len(tt.want) {
					t.Errorf("ParseTimes() len = %d, want %d", len(got), len(tt.want))
					return
				}
				for i := range got {
					if got[i] != tt.want[i] {
						t.Errorf("ParseTimes()[%d] = %+v, want %+v", i, got[i], tt.want[i])
					}
				}
			}
		})
	}
}

func TestNextTimeOfDay(t *testing.T) {
	loc := time.Local

	tests := []struct {
		name     string
		now      time.Time
		tod      TimeOfDay
		wantTime time.Time
	}{
		{
			name:     "next is today",
			now:      time.Date(2024, 1, 15, 8, 0, 0, 0, loc),
			tod:      TimeOfDay{9, 0},
			wantTime: time.Date(2024, 1, 15, 9, 0, 0, 0, loc),
		},
		{
			name:     "next is tomorrow",
			now:      time.Date(2024, 1, 15, 20, 0, 0, 0, loc),
			tod:      TimeOfDay{9, 0},
			wantTime: time.Date(2024, 1, 16, 9, 0, 0, 0, loc),
		},
		{
			name:     "exact same time goes to tomorrow",
			now:      time.Date(2024, 1, 15, 9, 0, 0, 0, loc),
			tod:      TimeOfDay{9, 0},
			wantTime: time.Date(2024, 1, 16, 9, 0, 0, 0, loc),
		},
		{
			name:     "one minute before",
			now:      time.Date(2024, 1, 15, 8, 59, 0, 0, loc),
			tod:      TimeOfDay{9, 0},
			wantTime: time.Date(2024, 1, 15, 9, 0, 0, 0, loc),
		},
		{
			name:     "midnight crossing",
			now:      time.Date(2024, 1, 15, 23, 59, 0, 0, loc),
			tod:      TimeOfDay{0, 0},
			wantTime: time.Date(2024, 1, 16, 0, 0, 0, 0, loc),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := nextTimeOfDay(tt.now, tt.tod)
			if !got.Equal(tt.wantTime) {
				t.Errorf("nextTimeOfDay() = %v, want %v", got, tt.wantTime)
			}
		})
	}
}

// fakeCommand implements pkgcmd.Command for testing.
type fakeCommand struct {
	name string
}

func (f *fakeCommand) Name() string                                            { return f.name }
func (f *fakeCommand) Description() string                                     { return "test command" }
func (f *fakeCommand) Execute(ctx context.Context, args []string, w io.Writer) error { return nil }

// fakeExecutor records executed commands.
type fakeExecutor struct {
	mu       sync.Mutex
	executed []executedRecord
}

type executedRecord struct {
	chatID  int64
	cmdName string
}

func (f *fakeExecutor) ExecuteScheduled(ctx context.Context, chatID int64, cmd pkgcmd.Command) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.executed = append(f.executed, executedRecord{
		chatID:  chatID,
		cmdName: cmd.Name(),
	})
	return nil
}

func (f *fakeExecutor) getExecuted() []executedRecord {
	f.mu.Lock()
	defer f.mu.Unlock()
	result := make([]executedRecord, len(f.executed))
	copy(result, f.executed)
	return result
}

func TestSchedulerNextExecution(t *testing.T) {
	exec := &fakeExecutor{}
	s := New(Config{
		ChatIDs:  []int64{123},
		Executor: exec,
	})

	cmd1 := &fakeCommand{name: "cmd1"}
	cmd2 := &fakeCommand{name: "cmd2"}

	// Add commands with different schedule times
	now := time.Now()
	laterToday := TimeOfDay{Hour: now.Hour(), Minute: now.Minute() + 5}
	if laterToday.Minute >= 60 {
		laterToday.Hour = (laterToday.Hour + 1) % 24
		laterToday.Minute = laterToday.Minute - 60
	}

	s.UpdateCommands([]ScheduledCommand{
		{Name: "cmd1", Times: []TimeOfDay{laterToday}, Command: cmd1},
		{Name: "cmd2", Times: []TimeOfDay{{23, 59}}, Command: cmd2},
	})

	nextTime, nextCmd := s.nextExecution()

	if nextCmd == nil {
		t.Fatal("nextExecution returned nil command")
	}

	// The command with the earlier time should be selected
	if nextCmd.Name != "cmd1" {
		t.Errorf("nextExecution() selected %q, want %q", nextCmd.Name, "cmd1")
	}

	// Next time should be in the future
	if !nextTime.After(now) {
		t.Errorf("nextExecution() time %v is not after now %v", nextTime, now)
	}
}

func TestSchedulerUpdateCommands(t *testing.T) {
	exec := &fakeExecutor{}
	s := New(Config{
		ChatIDs:  []int64{123},
		Executor: exec,
	})

	// Initially no commands
	_, cmd := s.nextExecution()
	if cmd != nil {
		t.Error("expected no command initially")
	}

	// Add commands
	s.UpdateCommands([]ScheduledCommand{
		{Name: "test", Times: []TimeOfDay{{9, 0}}, Command: &fakeCommand{name: "test"}},
	})

	_, cmd = s.nextExecution()
	if cmd == nil {
		t.Error("expected command after update")
	}

	// Clear commands
	s.UpdateCommands(nil)

	_, cmd = s.nextExecution()
	if cmd != nil {
		t.Error("expected no command after clearing")
	}
}
