// Package bot handles Telegram updates and routes commands to handlers.
package bot

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/rashpile/pako-telegram/internal/auth"
	"github.com/rashpile/pako-telegram/internal/command"
	"github.com/rashpile/pako-telegram/internal/config"
	pkgcmd "github.com/rashpile/pako-telegram/pkg/command"
)

// Config holds dependencies for Bot construction.
type Config struct {
	Token          string
	Authorizer     auth.Authorizer
	Registry       *command.Registry
	Defaults       config.DefaultsConfig
	AllowedChatIDs []int64 // Chat IDs to notify on startup
}

// Bot handles Telegram updates and routes commands to handlers.
type Bot struct {
	api            *tgbotapi.BotAPI
	authorizer     auth.Authorizer
	registry       *command.Registry
	defaults       config.DefaultsConfig
	confirmMgr     *ConfirmationManager
	menuBuilder    *MenuBuilder
	allowedChatIDs []int64
}

// New creates a Bot with the given dependencies.
func New(cfg Config) (*Bot, error) {
	api, err := tgbotapi.NewBotAPI(cfg.Token)
	if err != nil {
		return nil, fmt.Errorf("create telegram bot: %w", err)
	}

	slog.Info("authorized on telegram", "username", api.Self.UserName)

	registry := cfg.Registry
	if registry == nil {
		registry = command.NewRegistry()
	}

	return &Bot{
		api:            api,
		authorizer:     cfg.Authorizer,
		registry:       registry,
		defaults:       cfg.Defaults,
		confirmMgr:     NewConfirmationManager(),
		menuBuilder:    NewMenuBuilder(registry),
		allowedChatIDs: cfg.AllowedChatIDs,
	}, nil
}

// NotifyStartup sends a startup message with menu to all allowed chats.
func (b *Bot) NotifyStartup() {
	for _, chatID := range b.allowedChatIDs {
		b.sendText(chatID, "Bot restarted")
		b.sendMenu(chatID)
	}
}

// Registry returns the command registry for registration.
func (b *Bot) Registry() *command.Registry {
	return b.registry
}

// Run starts the bot's update loop. Blocks until context is cancelled.
func (b *Bot) Run(ctx context.Context) error {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 30

	updates := b.api.GetUpdatesChan(u)

	for {
		select {
		case <-ctx.Done():
			b.api.StopReceivingUpdates()
			slog.Info("bot stopped")
			return nil

		case update := <-updates:
			// Handle callback queries (menu navigation and confirmation buttons)
			if update.CallbackQuery != nil {
				go b.handleCallback(ctx, update.CallbackQuery)
				continue
			}

			// Handle command messages
			if update.Message != nil && update.Message.IsCommand() {
				cmdName := update.Message.Command()
				// Handle /start and /menu specially to show interactive menu
				if cmdName == "start" || cmdName == "menu" {
					go b.handleMenuCommand(update.Message)
					continue
				}
				go b.handleCommand(ctx, update.Message)
			}
		}
	}
}

// handleMenuCommand shows the interactive menu.
func (b *Bot) handleMenuCommand(msg *tgbotapi.Message) {
	chatID := msg.Chat.ID

	// Check authorization
	if !b.authorizer.IsAllowed(chatID) {
		slog.Warn("unauthorized access attempt", "chat_id", chatID)
		b.sendText(chatID, fmt.Sprintf("Unauthorized. Your chat ID (%d) is not in the allowlist.", chatID))
		return
	}

	b.sendMenu(chatID)
}

// handleCallback processes menu navigation and confirmation button presses.
func (b *Bot) handleCallback(ctx context.Context, query *tgbotapi.CallbackQuery) {
	chatID := query.Message.Chat.ID
	logger := slog.With("chat_id", chatID, "callback", query.Data)

	// Check authorization
	if !b.authorizer.IsAllowed(chatID) {
		logger.Warn("unauthorized callback attempt")
		return
	}

	// Answer the callback to remove loading state
	callback := tgbotapi.NewCallback(query.ID, "")
	b.api.Request(callback)

	// Check if this is a menu callback
	if IsMenuCallback(query.Data) {
		b.handleMenuCallback(ctx, query)
		return
	}

	// Handle confirmation callbacks
	pending, confirmed := b.confirmMgr.HandleCallback(query.Data)

	// Update the message to show result
	var resultText string
	if pending == nil {
		resultText = "Confirmation expired or invalid."
	} else if !confirmed {
		resultText = "Command cancelled."
	} else {
		resultText = fmt.Sprintf("Executing /%s...", pending.Command)
	}

	edit := tgbotapi.NewEditMessageText(chatID, query.Message.MessageID, resultText)
	b.api.Send(edit)

	// Execute if confirmed
	if confirmed && pending != nil {
		cmd := b.registry.Get(pending.Command)
		if cmd != nil {
			b.executeCommand(ctx, chatID, cmd, pending.Args)
			b.sendMenu(chatID)
		}
	}
}

// handleMenuCallback processes menu navigation callbacks.
func (b *Bot) handleMenuCallback(ctx context.Context, query *tgbotapi.CallbackQuery) {
	chatID := query.Message.Chat.ID
	messageID := query.Message.MessageID
	logger := slog.With("chat_id", chatID, "callback", query.Data)

	callbackType, value := ParseCallback(query.Data)

	switch callbackType {
	case "menu":
		// Show main menu
		text, keyboard := b.menuBuilder.BuildMainMenu()
		edit := tgbotapi.NewEditMessageText(chatID, messageID, text)
		edit.ReplyMarkup = &keyboard
		if _, err := b.api.Send(edit); err != nil {
			logger.Error("failed to edit menu", "error", err)
		}

	case "category":
		// Show category commands
		text, keyboard := b.menuBuilder.BuildCategoryMenu(value)
		edit := tgbotapi.NewEditMessageText(chatID, messageID, text)
		edit.ReplyMarkup = &keyboard
		if _, err := b.api.Send(edit); err != nil {
			logger.Error("failed to show category", "error", err)
		}

	case "command":
		// Execute the command
		cmd := b.registry.Get(value)
		if cmd == nil {
			logger.Warn("command not found from menu", "command", value)
			return
		}

		// Check if command requires confirmation
		if withMeta, ok := cmd.(pkgcmd.WithMetadata); ok {
			meta := withMeta.Metadata()
			if meta.RequireConfirm {
				// Delete the menu message and request confirmation
				deleteMsg := tgbotapi.NewDeleteMessage(chatID, messageID)
				b.api.Request(deleteMsg)

				logger.Info("requesting confirmation from menu", "command", value)
				if err := b.confirmMgr.RequestConfirmation(b.api, chatID, value, nil); err != nil {
					logger.Error("failed to request confirmation", "error", err)
				}
				return
			}
		}

		// Update message to show execution
		edit := tgbotapi.NewEditMessageText(chatID, messageID, fmt.Sprintf("Running /%s...", value))
		b.api.Send(edit)

		// Execute command
		logger.Info("executing command from menu", "command", value)
		b.executeCommand(ctx, chatID, cmd, nil)

		// Show menu again for quick access to next command
		b.sendMenu(chatID)
	}
}

// sendMenu sends the interactive menu to a chat.
func (b *Bot) sendMenu(chatID int64) {
	text, keyboard := b.menuBuilder.BuildMainMenu()
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ReplyMarkup = keyboard
	b.api.Send(msg)
}

// handleCommand processes a single command message.
func (b *Bot) handleCommand(ctx context.Context, msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	cmdName := msg.Command()

	logger := slog.With("chat_id", chatID, "command", cmdName)

	// Check authorization
	if !b.authorizer.IsAllowed(chatID) {
		logger.Warn("unauthorized access attempt")
		b.sendText(chatID, fmt.Sprintf("Unauthorized. Your chat ID (%d) is not in the allowlist.", chatID))
		return
	}

	// Look up command
	cmd := b.registry.Get(cmdName)
	if cmd == nil {
		logger.Debug("unknown command")
		b.sendText(chatID, fmt.Sprintf("Unknown command: /%s\nUse /help to see available commands.", cmdName))
		return
	}

	// Determine args based on command type
	// For commands that implement WithFileResponse, preserve raw text (including newlines)
	var args []string
	if _, ok := cmd.(pkgcmd.WithFileResponse); ok {
		// Extract raw text after command, preserving newlines
		rawText := extractRawText(msg.Text, cmdName)
		if rawText != "" {
			args = []string{rawText}
		}
	} else {
		args = parseArgs(msg.CommandArguments())
	}

	// Check if command requires confirmation
	if withMeta, ok := cmd.(pkgcmd.WithMetadata); ok {
		meta := withMeta.Metadata()
		if meta.RequireConfirm {
			logger.Info("requesting confirmation", "args", args)
			if err := b.confirmMgr.RequestConfirmation(b.api, chatID, cmdName, args); err != nil {
				logger.Error("failed to request confirmation", "error", err)
			}
			return
		}
	}

	logger.Info("executing command", "args_count", len(args))
	b.executeCommand(ctx, chatID, cmd, args)

	// Show menu for quick access to next command
	b.sendMenu(chatID)
}

// executeCommand runs a command and streams output.
func (b *Bot) executeCommand(ctx context.Context, chatID int64, cmd pkgcmd.Command, args []string) {
	logger := slog.With("chat_id", chatID, "command", cmd.Name())

	// Get timeout from metadata or use default
	timeout := b.defaults.Timeout
	if withMeta, ok := cmd.(pkgcmd.WithMetadata); ok {
		meta := withMeta.Metadata()
		if meta.Timeout > 0 {
			timeout = meta.Timeout
		}
	}

	// Execute command with streaming output
	streamer := NewMessageStreamer(b.api, chatID)
	if err := streamer.Start(ctx); err != nil {
		logger.Error("failed to start streamer", "error", err)
		return
	}

	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	execErr := cmd.Execute(execCtx, args, streamer)
	if execErr != nil {
		logger.Error("command execution failed", "error", execErr)
		fmt.Fprintf(streamer, "\n\nError: %v", execErr)
	}

	if err := streamer.Flush(); err != nil {
		logger.Error("failed to flush output", "error", err)
	}

	// Handle file response if command supports it
	if execErr == nil {
		if withFile, ok := cmd.(pkgcmd.WithFileResponse); ok {
			if resp := withFile.FileResponse(); resp != nil && resp.Path != "" {
				b.sendAudioFile(chatID, resp)
			}
		}
	}
}

// sendAudioFile sends an audio file to the chat.
func (b *Bot) sendAudioFile(chatID int64, resp *pkgcmd.FileResponse) {
	logger := slog.With("chat_id", chatID, "file", resp.Path)

	audio := tgbotapi.NewAudio(chatID, tgbotapi.FilePath(resp.Path))
	if resp.Caption != "" {
		audio.Caption = resp.Caption
	}

	if _, err := b.api.Send(audio); err != nil {
		logger.Error("failed to send audio file", "error", err)
		b.sendText(chatID, fmt.Sprintf("Failed to send audio: %v", err))
	} else {
		logger.Info("audio file sent successfully")
	}

	// Cleanup if requested
	if resp.Cleanup {
		if err := os.Remove(resp.Path); err != nil {
			logger.Warn("failed to cleanup file", "error", err)
		}
	}
}

// sendText sends a simple text message.
func (b *Bot) sendText(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	if _, err := b.api.Send(msg); err != nil {
		slog.Error("failed to send message", "error", err, "chat_id", chatID)
	}
}

// parseArgs splits command arguments into a slice.
func parseArgs(argString string) []string {
	if argString == "" {
		return nil
	}
	return strings.Fields(argString)
}

// extractRawText extracts text after the command, preserving newlines.
// Example: "/podcast hello\nworld" with cmdName "podcast" returns "hello\nworld"
func extractRawText(fullText, cmdName string) string {
	// Find the end of the command (after /cmdName or /cmdName@botname)
	prefix := "/" + cmdName
	idx := strings.Index(fullText, prefix)
	if idx == -1 {
		return ""
	}

	// Move past the command
	rest := fullText[idx+len(prefix):]

	// Skip @botname if present
	if len(rest) > 0 && rest[0] == '@' {
		spaceIdx := strings.IndexAny(rest, " \n")
		if spaceIdx == -1 {
			return ""
		}
		rest = rest[spaceIdx:]
	}

	// Skip leading space/newline after command
	if len(rest) > 0 && (rest[0] == ' ' || rest[0] == '\n') {
		rest = rest[1:]
	}

	return rest
}
