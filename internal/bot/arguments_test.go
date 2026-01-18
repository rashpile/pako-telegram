package bot

import (
	"testing"
	"time"

	"github.com/rashpile/pako-telegram/internal/command"
)

func TestValidateArgument(t *testing.T) {
	tests := []struct {
		name    string
		arg     command.ArgumentDef
		input   string
		wantErr bool
	}{
		{
			name:    "required string empty",
			arg:     command.ArgumentDef{Name: "test", Required: true, Type: "string"},
			input:   "",
			wantErr: true,
		},
		{
			name:    "required string with value",
			arg:     command.ArgumentDef{Name: "test", Required: true, Type: "string"},
			input:   "hello",
			wantErr: false,
		},
		{
			name:    "optional string empty",
			arg:     command.ArgumentDef{Name: "test", Required: false, Type: "string"},
			input:   "",
			wantErr: false,
		},
		{
			name:    "valid int",
			arg:     command.ArgumentDef{Name: "test", Type: "int"},
			input:   "42",
			wantErr: false,
		},
		{
			name:    "invalid int",
			arg:     command.ArgumentDef{Name: "test", Type: "int"},
			input:   "not a number",
			wantErr: true,
		},
		{
			name:    "valid bool true",
			arg:     command.ArgumentDef{Name: "test", Type: "bool"},
			input:   "yes",
			wantErr: false,
		},
		{
			name:    "valid bool false",
			arg:     command.ArgumentDef{Name: "test", Type: "bool"},
			input:   "no",
			wantErr: false,
		},
		{
			name:    "invalid bool",
			arg:     command.ArgumentDef{Name: "test", Type: "bool"},
			input:   "maybe",
			wantErr: true,
		},
		{
			name:    "valid choice",
			arg:     command.ArgumentDef{Name: "test", Type: "choice", Choices: []string{"a", "b", "c"}},
			input:   "b",
			wantErr: false,
		},
		{
			name:    "invalid choice",
			arg:     command.ArgumentDef{Name: "test", Type: "choice", Choices: []string{"a", "b", "c"}},
			input:   "d",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateArgument(&tt.arg, tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateArgument() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRenderCommand(t *testing.T) {
	tests := []struct {
		name     string
		template string
		args     map[string]string
		want     string
		wantErr  bool
	}{
		{
			name:     "simple substitution",
			template: "echo {{.message}}",
			args:     map[string]string{"message": "hello"},
			want:     "echo hello",
			wantErr:  false,
		},
		{
			name:     "multiple substitutions",
			template: "deploy --env={{.env}} --version={{.version}}",
			args:     map[string]string{"env": "prod", "version": "1.0.0"},
			want:     "deploy --env=prod --version=1.0.0",
			wantErr:  false,
		},
		{
			name:     "with special characters",
			template: "echo '{{.prompt}}'",
			args:     map[string]string{"prompt": "hello world"},
			want:     "echo 'hello world'",
			wantErr:  false,
		},
		{
			name:     "invalid template",
			template: "echo {{.missing}",
			args:     map[string]string{},
			want:     "",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := RenderCommand(tt.template, tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("RenderCommand() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("RenderCommand() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestArgumentSession(t *testing.T) {
	session := &ArgumentSession{
		ChatID: 123,
		Arguments: []command.ArgumentDef{
			{Name: "arg1", Description: "First arg"},
			{Name: "arg2", Description: "Second arg"},
		},
		Collected:  make(map[string]string),
		CurrentIdx: 0,
		StartedAt:  time.Now(),
		TimeoutDur: 120 * time.Second,
	}

	// Test CurrentArg
	arg := session.CurrentArg()
	if arg == nil || arg.Name != "arg1" {
		t.Errorf("CurrentArg() = %v, want arg1", arg)
	}

	// Test IsComplete - should be false
	if session.IsComplete() {
		t.Error("IsComplete() = true, want false")
	}

	// Advance to next argument
	session.CurrentIdx = 1
	arg = session.CurrentArg()
	if arg == nil || arg.Name != "arg2" {
		t.Errorf("CurrentArg() = %v, want arg2", arg)
	}

	// Complete all arguments
	session.CurrentIdx = 2
	if !session.IsComplete() {
		t.Error("IsComplete() = false, want true")
	}

	// Test IsExpired
	if session.IsExpired() {
		t.Error("IsExpired() = true, want false for fresh session")
	}

	// Test expired session
	expiredSession := &ArgumentSession{
		StartedAt:  time.Now().Add(-200 * time.Second),
		TimeoutDur: 120 * time.Second,
	}
	if !expiredSession.IsExpired() {
		t.Error("IsExpired() = false, want true for expired session")
	}
}

func TestArgumentCollector(t *testing.T) {
	collector := NewArgumentCollector()

	// Test no session
	if collector.HasSession(123) {
		t.Error("HasSession() = true for non-existent session")
	}

	// Test cancel non-existent session (should not panic)
	collector.CancelSession(123)

	// Test process input with no session
	errMsg := collector.ProcessInput(123, "test")
	if errMsg == "" {
		t.Error("ProcessInput() should return error for non-existent session")
	}
}

func TestBuildChoiceKeyboard(t *testing.T) {
	// Test with few choices - should return keyboard
	arg := &command.ArgumentDef{
		Name:    "model",
		Type:    "choice",
		Choices: []string{"a", "b", "c"},
	}
	keyboard := BuildChoiceKeyboard(arg)
	if keyboard == nil {
		t.Error("BuildChoiceKeyboard() = nil, want keyboard for few choices")
	}

	// Test with many choices - should return nil
	arg.Choices = []string{"a", "b", "c", "d", "e", "f"}
	keyboard = BuildChoiceKeyboard(arg)
	if keyboard != nil {
		t.Error("BuildChoiceKeyboard() should return nil for many choices")
	}

	// Test with non-choice type - should return nil
	arg.Type = "string"
	keyboard = BuildChoiceKeyboard(arg)
	if keyboard != nil {
		t.Error("BuildChoiceKeyboard() should return nil for non-choice type")
	}
}

func TestBuildChoiceTextList(t *testing.T) {
	// Test with few choices - should return empty
	arg := &command.ArgumentDef{
		Name:        "model",
		Description: "Select a model",
		Type:        "choice",
		Choices:     []string{"a", "b", "c"},
	}
	text := BuildChoiceTextList(arg)
	if text != "" {
		t.Error("BuildChoiceTextList() should return empty for few choices")
	}

	// Test with many choices - should return text list
	arg.Choices = []string{"a", "b", "c", "d", "e", "f"}
	text = BuildChoiceTextList(arg)
	if text == "" {
		t.Error("BuildChoiceTextList() should return text list for many choices")
	}
	if !contains(text, "Select a model") {
		t.Error("BuildChoiceTextList() should contain description")
	}
	if !contains(text, "1. a") {
		t.Error("BuildChoiceTextList() should contain numbered options")
	}
}

func TestIsArgumentCallback(t *testing.T) {
	tests := []struct {
		data string
		want bool
	}{
		{"arg:value", true},
		{"arg:", true},
		{"menu:main", false},
		{"confirm:123", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.data, func(t *testing.T) {
			if got := IsArgumentCallback(tt.data); got != tt.want {
				t.Errorf("IsArgumentCallback(%q) = %v, want %v", tt.data, got, tt.want)
			}
		})
	}
}

func TestParseArgumentCallback(t *testing.T) {
	tests := []struct {
		data string
		want string
	}{
		{"arg:value", "value"},
		{"arg:hello world", "hello world"},
		{"arg:", ""},
	}

	for _, tt := range tests {
		t.Run(tt.data, func(t *testing.T) {
			if got := ParseArgumentCallback(tt.data); got != tt.want {
				t.Errorf("ParseArgumentCallback(%q) = %v, want %v", tt.data, got, tt.want)
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
