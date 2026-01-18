// Package bot provides argument collection for commands with interactive prompts.
package bot

import (
	"bytes"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/rashpile/pako-telegram/internal/command"
)

const (
	defaultArgumentTimeout = 120 * time.Second
	maxInlineChoices       = 4
)

// ArgumentSession tracks in-progress argument collection for a chat.
type ArgumentSession struct {
	ChatID          int64
	Command         *command.YAMLCommand
	Arguments       []command.ArgumentDef
	Collected       map[string]string
	CurrentIdx      int
	StartedAt       time.Time
	TimeoutDur      time.Duration
	LastPromptMsgID int // Message ID of the last prompt (for editing)
}

// CurrentArg returns the argument currently being collected.
func (s *ArgumentSession) CurrentArg() *command.ArgumentDef {
	if s.CurrentIdx >= len(s.Arguments) {
		return nil
	}
	return &s.Arguments[s.CurrentIdx]
}

// IsComplete returns true if all arguments have been collected.
func (s *ArgumentSession) IsComplete() bool {
	return s.CurrentIdx >= len(s.Arguments)
}

// IsExpired returns true if the session has timed out.
func (s *ArgumentSession) IsExpired() bool {
	return time.Since(s.StartedAt) > s.TimeoutDur
}

// ArgumentCollector manages argument collection sessions.
type ArgumentCollector struct {
	mu             sync.RWMutex
	sessions       map[int64]*ArgumentSession
	defaultTimeout time.Duration
}

// NewArgumentCollector creates a new argument collector.
func NewArgumentCollector() *ArgumentCollector {
	return &ArgumentCollector{
		sessions:       make(map[int64]*ArgumentSession),
		defaultTimeout: defaultArgumentTimeout,
	}
}

// StartSession begins argument collection for a command.
// Returns the list of arguments to prompt for (skipping those with defaults).
func (c *ArgumentCollector) StartSession(chatID int64, cmd *command.YAMLCommand) *ArgumentSession {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Cancel any existing session for this chat
	delete(c.sessions, chatID)

	args := cmd.Arguments()
	timeout := cmd.ArgumentTimeout()
	if timeout == 0 {
		timeout = c.defaultTimeout
	}

	// Filter to arguments that need prompting (required without default, or choice types)
	var toPrompt []command.ArgumentDef
	collected := make(map[string]string)

	for _, arg := range args {
		// If has default and not required, use default
		if arg.Default != "" && !arg.Required {
			collected[arg.Name] = arg.Default
		} else {
			toPrompt = append(toPrompt, arg)
		}
	}

	session := &ArgumentSession{
		ChatID:     chatID,
		Command:    cmd,
		Arguments:  toPrompt,
		Collected:  collected,
		CurrentIdx: 0,
		StartedAt:  time.Now(),
		TimeoutDur: timeout,
	}

	c.sessions[chatID] = session
	return session
}

// GetSession returns the active session for a chat, or nil if none exists.
func (c *ArgumentCollector) GetSession(chatID int64) *ArgumentSession {
	c.mu.RLock()
	defer c.mu.RUnlock()

	session := c.sessions[chatID]
	if session != nil && session.IsExpired() {
		return nil
	}
	return session
}

// HasSession returns true if there's an active session for the chat.
func (c *ArgumentCollector) HasSession(chatID int64) bool {
	return c.GetSession(chatID) != nil
}

// CancelSession removes the session for a chat.
func (c *ArgumentCollector) CancelSession(chatID int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.sessions, chatID)
}

// ProcessInput validates and stores user input for the current argument.
// Returns error message if validation fails, empty string on success.
func (c *ArgumentCollector) ProcessInput(chatID int64, input string) (errMsg string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	session := c.sessions[chatID]
	if session == nil || session.IsExpired() {
		return "No active argument collection session."
	}

	arg := session.CurrentArg()
	if arg == nil {
		return ""
	}

	// Validate input
	if err := validateArgument(arg, input); err != nil {
		return err.Error()
	}

	// Store the value
	session.Collected[arg.Name] = input
	session.CurrentIdx++

	return ""
}

// CompleteSession finalizes the session and returns collected arguments.
// Removes the session from active tracking.
func (c *ArgumentCollector) CompleteSession(chatID int64) (map[string]string, *command.YAMLCommand) {
	c.mu.Lock()
	defer c.mu.Unlock()

	session := c.sessions[chatID]
	if session == nil {
		return nil, nil
	}

	collected := session.Collected
	cmd := session.Command
	delete(c.sessions, chatID)

	return collected, cmd
}

// SetLastPromptMsgID stores the message ID of the last prompt sent.
func (c *ArgumentCollector) SetLastPromptMsgID(chatID int64, msgID int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if session := c.sessions[chatID]; session != nil {
		session.LastPromptMsgID = msgID
	}
}

// GetLastPromptMsgID returns the message ID of the last prompt.
func (c *ArgumentCollector) GetLastPromptMsgID(chatID int64) int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if session := c.sessions[chatID]; session != nil {
		return session.LastPromptMsgID
	}
	return 0
}

// validateArgument checks if the input is valid for the argument type.
func validateArgument(arg *command.ArgumentDef, input string) error {
	// Check required
	if arg.Required && strings.TrimSpace(input) == "" {
		return fmt.Errorf("This field is required")
	}

	// Allow empty for optional
	if input == "" {
		return nil
	}

	switch arg.Type {
	case "int":
		if _, err := strconv.Atoi(input); err != nil {
			return fmt.Errorf("Please enter a valid integer")
		}

	case "bool":
		lower := strings.ToLower(strings.TrimSpace(input))
		valid := map[string]bool{
			"true": true, "false": true,
			"yes": true, "no": true,
			"1": true, "0": true,
		}
		if !valid[lower] {
			return fmt.Errorf("Please enter yes/no, true/false, or 1/0")
		}

	case "choice":
		if len(arg.Choices) > 0 && !slices.Contains(arg.Choices, input) {
			return fmt.Errorf("Please select one of: %s", strings.Join(arg.Choices, ", "))
		}

	case "string", "":
		// String type accepts anything non-empty (if required)
	}

	return nil
}

// RenderCommand applies collected arguments to the command template.
func RenderCommand(cmdTemplate string, args map[string]string) (string, error) {
	tmpl, err := template.New("cmd").Parse(cmdTemplate)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, args); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}

	return buf.String(), nil
}

// BuildArgumentPrompt creates a message for prompting an argument.
func BuildArgumentPrompt(arg *command.ArgumentDef) string {
	return arg.Description
}

// BuildChoiceKeyboard creates an inline keyboard for choice arguments.
// Returns nil if there are too many choices (use text list instead).
func BuildChoiceKeyboard(arg *command.ArgumentDef) *tgbotapi.InlineKeyboardMarkup {
	if arg.Type != "choice" || len(arg.Choices) == 0 || len(arg.Choices) > maxInlineChoices {
		return nil
	}

	var rows [][]tgbotapi.InlineKeyboardButton
	for _, choice := range arg.Choices {
		btn := tgbotapi.NewInlineKeyboardButtonData(choice, "arg:"+choice)
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(btn))
	}

	keyboard := tgbotapi.NewInlineKeyboardMarkup(rows...)
	return &keyboard
}

// BuildChoiceTextList creates a text list for choice arguments with many options.
func BuildChoiceTextList(arg *command.ArgumentDef) string {
	if arg.Type != "choice" || len(arg.Choices) <= maxInlineChoices {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(arg.Description)
	sb.WriteString("\n\nOptions:\n")
	for i, choice := range arg.Choices {
		fmt.Fprintf(&sb, "%d. %s\n", i+1, choice)
	}
	return sb.String()
}

// IsArgumentCallback checks if a callback is an argument selection.
func IsArgumentCallback(data string) bool {
	return strings.HasPrefix(data, "arg:")
}

// ParseArgumentCallback extracts the selected value from an argument callback.
func ParseArgumentCallback(data string) string {
	return strings.TrimPrefix(data, "arg:")
}

// CleanupExpiredSessions removes expired sessions. Call periodically.
func (c *ArgumentCollector) CleanupExpiredSessions() {
	c.mu.Lock()
	defer c.mu.Unlock()

	for chatID, session := range c.sessions {
		if session.IsExpired() {
			delete(c.sessions, chatID)
		}
	}
}
