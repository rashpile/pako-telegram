package bot

import (
	"bytes"
	"context"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

const (
	// throttleInterval limits message edits to respect Telegram rate limits.
	throttleInterval = time.Second

	// maxMessageLength is Telegram's limit for message text.
	maxMessageLength = 4096
)

// MessageStreamer handles progressive message updates for command output.
type MessageStreamer struct {
	api       *tgbotapi.BotAPI
	chatID    int64
	messageID int

	mu       sync.Mutex
	buffer   bytes.Buffer
	lastEdit time.Time
	dirty    bool
}

// NewMessageStreamer creates a streamer that edits a message progressively.
func NewMessageStreamer(api *tgbotapi.BotAPI, chatID int64) *MessageStreamer {
	return &MessageStreamer{
		api:    api,
		chatID: chatID,
	}
}

// Start sends an initial "Running..." message and stores its ID.
func (ms *MessageStreamer) Start(ctx context.Context) error {
	msg := tgbotapi.NewMessage(ms.chatID, "```\nRunning...\n```")
	msg.ParseMode = "Markdown"

	sent, err := ms.api.Send(msg)
	if err != nil {
		return err
	}

	ms.messageID = sent.MessageID
	return nil
}

// Write implements io.Writer, buffering output for throttled edits.
func (ms *MessageStreamer) Write(p []byte) (n int, err error) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	n, err = ms.buffer.Write(p)
	ms.dirty = true

	// Throttle edits
	if time.Since(ms.lastEdit) >= throttleInterval {
		ms.editMessage()
	}

	return n, err
}

// WriteString is a convenience method for writing strings.
func (ms *MessageStreamer) WriteString(s string) (n int, err error) {
	return ms.Write([]byte(s))
}

// Flush sends the final message content.
func (ms *MessageStreamer) Flush() error {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	if ms.dirty {
		ms.editMessage()
	}
	return nil
}

// editMessage updates the Telegram message with current buffer contents.
// Must be called with mutex held.
func (ms *MessageStreamer) editMessage() {
	content := ms.buffer.String()
	if content == "" {
		content = "(no output)"
	}

	// Truncate if too long
	if len(content) > maxMessageLength-20 {
		content = content[:maxMessageLength-30] + "\n\n[truncated]"
	}

	// Wrap in code block
	text := "```\n" + content + "\n```"

	edit := tgbotapi.NewEditMessageText(ms.chatID, ms.messageID, text)
	edit.ParseMode = "Markdown"

	_, _ = ms.api.Send(edit) // Ignore edit errors (rate limits, etc.)

	ms.lastEdit = time.Now()
	ms.dirty = false
}
