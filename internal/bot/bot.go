// Package bot handles Telegram updates and routes commands to handlers.
package bot

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/rashpile/pako-telegram/internal/auth"
	"github.com/rashpile/pako-telegram/internal/command"
	"github.com/rashpile/pako-telegram/internal/config"
	pkgcmd "github.com/rashpile/pako-telegram/pkg/command"
)

// Config holds dependencies for Bot construction.
type Config struct {
	Token      string
	Authorizer auth.Authorizer
	Registry   *command.Registry
	Defaults   config.DefaultsConfig
}

// Bot handles Telegram updates and routes commands to handlers.
type Bot struct {
	api          *tgbotapi.BotAPI
	authorizer   auth.Authorizer
	registry     *command.Registry
	defaults     config.DefaultsConfig
	confirmMgr   *ConfirmationManager
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
		api:        api,
		authorizer: cfg.Authorizer,
		registry:   registry,
		defaults:   cfg.Defaults,
		confirmMgr: NewConfirmationManager(),
	}, nil
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
			// Handle callback queries (confirmation buttons)
			if update.CallbackQuery != nil {
				go b.handleCallback(ctx, update.CallbackQuery)
				continue
			}

			// Handle command messages
			if update.Message != nil && update.Message.IsCommand() {
				go b.handleCommand(ctx, update.Message)
			}
		}
	}
}

// handleCallback processes confirmation button presses.
func (b *Bot) handleCallback(ctx context.Context, query *tgbotapi.CallbackQuery) {
	chatID := query.Message.Chat.ID
	logger := slog.With("chat_id", chatID, "callback", query.Data)

	// Check authorization
	if !b.authorizer.IsAllowed(chatID) {
		logger.Warn("unauthorized callback attempt")
		return
	}

	pending, confirmed := b.confirmMgr.HandleCallback(query.Data)

	// Answer the callback to remove loading state
	callback := tgbotapi.NewCallback(query.ID, "")
	b.api.Request(callback)

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
		}
	}
}

// handleCommand processes a single command message.
func (b *Bot) handleCommand(ctx context.Context, msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	cmdName := msg.Command()
	args := parseArgs(msg.CommandArguments())

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

	logger.Info("executing command", "args", args)
	b.executeCommand(ctx, chatID, cmd, args)
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

	if err := cmd.Execute(execCtx, args, streamer); err != nil {
		logger.Error("command execution failed", "error", err)
		streamer.WriteString(fmt.Sprintf("\n\nError: %v", err))
	}

	if err := streamer.Flush(); err != nil {
		logger.Error("failed to flush output", "error", err)
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
