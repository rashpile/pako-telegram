package bot

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

const (
	// confirmationTTL is how long a confirmation request remains valid.
	confirmationTTL = 5 * time.Minute

	// callbackConfirm is the prefix for confirm callbacks.
	callbackConfirm = "confirm:"

	// callbackCancel is the prefix for cancel callbacks.
	callbackCancel = "cancel:"
)

// PendingConfirmation tracks a command awaiting user confirmation.
type PendingConfirmation struct {
	ChatID    int64
	MessageID int
	Command   string
	Args      []string
	ExpiresAt time.Time
}

// ConfirmationManager handles confirmation dialogs.
type ConfirmationManager struct {
	mu      sync.Mutex
	pending map[string]*PendingConfirmation // key: unique ID
}

// NewConfirmationManager creates a confirmation manager.
func NewConfirmationManager() *ConfirmationManager {
	cm := &ConfirmationManager{
		pending: make(map[string]*PendingConfirmation),
	}

	// Start cleanup goroutine
	go cm.cleanupLoop()

	return cm
}

// RequestConfirmation sends an inline keyboard and stores pending state.
func (cm *ConfirmationManager) RequestConfirmation(
	api *tgbotapi.BotAPI,
	chatID int64,
	cmdName string,
	args []string,
) error {
	id := generateID()

	// Create inline keyboard
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Confirm", callbackConfirm+id),
			tgbotapi.NewInlineKeyboardButtonData("Cancel", callbackCancel+id),
		),
	)

	text := fmt.Sprintf("Confirm execution of `/%s`?", cmdName)
	if len(args) > 0 {
		text = fmt.Sprintf("Confirm execution of `/%s %v`?", cmdName, args)
	}

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard

	sent, err := api.Send(msg)
	if err != nil {
		return err
	}

	// Store pending confirmation
	cm.mu.Lock()
	cm.pending[id] = &PendingConfirmation{
		ChatID:    chatID,
		MessageID: sent.MessageID,
		Command:   cmdName,
		Args:      args,
		ExpiresAt: time.Now().Add(confirmationTTL),
	}
	cm.mu.Unlock()

	return nil
}

// HandleCallback processes a confirmation button press.
// Returns the pending confirmation if confirmed, nil if cancelled or not found.
func (cm *ConfirmationManager) HandleCallback(callbackData string) (*PendingConfirmation, bool) {
	var id string
	var confirmed bool

	switch {
	case len(callbackData) > len(callbackConfirm) && callbackData[:len(callbackConfirm)] == callbackConfirm:
		id = callbackData[len(callbackConfirm):]
		confirmed = true
	case len(callbackData) > len(callbackCancel) && callbackData[:len(callbackCancel)] == callbackCancel:
		id = callbackData[len(callbackCancel):]
		confirmed = false
	default:
		return nil, false
	}

	cm.mu.Lock()
	pending, ok := cm.pending[id]
	if ok {
		delete(cm.pending, id)
	}
	cm.mu.Unlock()

	if !ok || time.Now().After(pending.ExpiresAt) {
		return nil, false
	}

	if !confirmed {
		return nil, false
	}

	return pending, true
}

// cleanupLoop removes expired confirmations.
func (cm *ConfirmationManager) cleanupLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		cm.mu.Lock()
		now := time.Now()
		for id, pending := range cm.pending {
			if now.After(pending.ExpiresAt) {
				delete(cm.pending, id)
			}
		}
		cm.mu.Unlock()
	}
}

// generateID creates a random ID for callback tracking.
func generateID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}
