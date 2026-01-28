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
	"github.com/rashpile/pako-telegram/internal/command/builtin"
	"github.com/rashpile/pako-telegram/internal/config"
	"github.com/rashpile/pako-telegram/internal/fileref"
	"github.com/rashpile/pako-telegram/internal/msgstore"
	"github.com/rashpile/pako-telegram/internal/scheduler"
	pkgcmd "github.com/rashpile/pako-telegram/pkg/command"
)

// Config holds dependencies for Bot construction.
type Config struct {
	Token          string
	Authorizer     auth.Authorizer
	Registry       *command.Registry
	Defaults       config.DefaultsConfig
	AllowedChatIDs []int64 // Chat IDs to notify on startup
	MessageStore   *msgstore.Store
}

// Bot handles Telegram updates and routes commands to handlers.
type Bot struct {
	api            *tgbotapi.BotAPI
	authorizer     auth.Authorizer
	registry       *command.Registry
	defaults       config.DefaultsConfig
	confirmMgr     *ConfirmationManager
	menuBuilder    *MenuBuilder
	argCollector   *ArgumentCollector
	allowedChatIDs []int64
	msgStore       *msgstore.Store
	cleanupCmd     *builtin.CleanupCommand
	scheduler      *scheduler.Scheduler
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

	menuBuilder := NewMenuBuilder(registry)

	b := &Bot{
		api:            api,
		authorizer:     cfg.Authorizer,
		registry:       registry,
		defaults:       cfg.Defaults,
		confirmMgr:     NewConfirmationManager(),
		menuBuilder:    menuBuilder,
		argCollector:   NewArgumentCollector(),
		allowedChatIDs: cfg.AllowedChatIDs,
		msgStore:       cfg.MessageStore,
	}

	// Create cleanup command if message store is enabled
	if cfg.MessageStore != nil && cfg.MessageStore.Enabled() {
		b.cleanupCmd = builtin.NewCleanupCommand(cfg.MessageStore, b)
		menuBuilder.SetCleanupEnabled(true)
	}

	return b, nil
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

// SetScheduler sets the scheduler reference for pause/resume functionality.
func (b *Bot) SetScheduler(s *scheduler.Scheduler) {
	b.scheduler = s
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
			// Handle callback queries (menu navigation, confirmation buttons, argument selection)
			if update.CallbackQuery != nil {
				go b.handleCallback(ctx, update.CallbackQuery)
				continue
			}

			// Handle messages
			if update.Message != nil {
				chatID := update.Message.Chat.ID

				// Handle command messages
				if update.Message.IsCommand() {
					cmdName := update.Message.Command()
					// Handle /cancel to abort argument collection
					if cmdName == "cancel" {
						go b.handleCancelCommand(update.Message)
						continue
					}
					// Handle /start and /menu specially to show interactive menu
					if cmdName == "start" || cmdName == "menu" {
						go b.handleMenuCommand(update.Message)
						continue
					}
					go b.handleCommand(ctx, update.Message)
					continue
				}

				// Handle non-command text messages for argument collection
				if b.argCollector.HasSession(chatID) {
					go b.handleArgumentInput(ctx, update.Message)
				}
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

	// Check if this is an argument selection callback
	if IsArgumentCallback(query.Data) {
		b.handleArgumentCallback(ctx, query)
		return
	}

	// Check if this is a menu callback
	if IsMenuCallback(query.Data) {
		b.handleMenuCallback(ctx, query)
		return
	}

	// Check if this is a schedule callback
	if IsScheduleCallback(query.Data) {
		b.handleScheduleCallback(ctx, query)
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
			// Check if this is a rendered command (from argument collection)
			if pending.RenderedCommand != "" {
				if yamlCmd, ok := cmd.(*command.YAMLCommand); ok {
					b.executeRenderedCommand(ctx, chatID, yamlCmd, pending.RenderedCommand)
				}
			} else {
				b.executeCommand(ctx, chatID, cmd, pending.Args)
			}
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

	case "cleanup":
		// Handle cleanup option selection
		b.handleCleanupCallback(chatID, messageID, value, logger)

	case "command":
		// Check if this is the cleanup command
		if value == "cleanup" {
			b.showCleanupMenu(chatID, messageID)
			return
		}
		// Execute the command
		cmd := b.registry.Get(value)
		if cmd == nil {
			logger.Warn("command not found from menu", "command", value)
			return
		}

		// Check if command is a scheduled/interval command - show schedule menu
		if yamlCmd, ok := cmd.(*command.YAMLCommand); ok {
			if len(yamlCmd.Schedule()) > 0 || yamlCmd.Interval() > 0 {
				deleteMsg := tgbotapi.NewDeleteMessage(chatID, messageID)
				b.api.Request(deleteMsg)
				b.showScheduleMenu(chatID, yamlCmd)
				return
			}
		}

		// Check if command is a YAMLCommand with arguments
		if yamlCmd, ok := cmd.(*command.YAMLCommand); ok && yamlCmd.HasArguments() {
			// Delete the menu message and start argument collection
			deleteMsg := tgbotapi.NewDeleteMessage(chatID, messageID)
			b.api.Request(deleteMsg)

			logger.Info("starting argument collection from menu", "command", value)
			session := b.argCollector.StartSession(chatID, yamlCmd)
			if session != nil && !session.IsComplete() {
				b.promptNextArgument(chatID, session)
				return
			}
			// All arguments have defaults, proceed with execution
			b.executeWithArguments(ctx, chatID)
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

		// Check if quiet mode
		quiet := false
		if yamlCmd, ok := cmd.(*command.YAMLCommand); ok {
			quiet = yamlCmd.Quiet()
		}

		if quiet {
			// In quiet mode, delete menu immediately
			deleteMsg := tgbotapi.NewDeleteMessage(chatID, messageID)
			b.api.Request(deleteMsg)
		} else {
			// Update message to show execution
			edit := tgbotapi.NewEditMessageText(chatID, messageID, fmt.Sprintf("Running /%s...", value))
			b.api.Send(edit)
		}

		// Execute command
		logger.Info("executing command from menu", "command", value)
		b.executeCommandWithOptions(ctx, chatID, cmd, nil, quiet)

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

	// Check if command is a scheduled/interval command - show menu if so
	if yamlCmd, ok := cmd.(*command.YAMLCommand); ok {
		if len(yamlCmd.Schedule()) > 0 || yamlCmd.Interval() > 0 {
			b.showScheduleMenu(chatID, yamlCmd)
			return
		}
	}

	// Check if command is a YAMLCommand with arguments that need collection
	if yamlCmd, ok := cmd.(*command.YAMLCommand); ok && yamlCmd.HasArguments() {
		logger.Info("starting argument collection")
		session := b.argCollector.StartSession(chatID, yamlCmd)
		if session != nil && !session.IsComplete() {
			b.promptNextArgument(chatID, session)
			return
		}
		// All arguments have defaults, proceed with execution
		b.executeWithArguments(ctx, chatID)
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
	b.executeCommandWithOptions(ctx, chatID, cmd, args, false)
}

// executeCommandWithOptions runs a command with optional quiet mode.
// In quiet mode, no "Running..." message is shown and file-only output is silent.
func (b *Bot) executeCommandWithOptions(ctx context.Context, chatID int64, cmd pkgcmd.Command, args []string, quiet bool) {
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
	var streamer *MessageStreamer
	if quiet {
		streamer = NewQuietMessageStreamer(b.api, chatID)
	} else {
		streamer = NewMessageStreamer(b.api, chatID)
	}
	if err := streamer.Start(ctx); err != nil {
		logger.Error("failed to start streamer", "error", err)
		return
	}

	// Track the output message for cleanup (only if message was created)
	if streamer.MessageID() != 0 {
		b.trackMessage(chatID, streamer.MessageID(), msgstore.TypeText)
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

	// Get workdir if this is a YAML command
	workdir := ""
	if yamlCmd, ok := cmd.(*command.YAMLCommand); ok {
		workdir = yamlCmd.Workdir()
	}

	// Handle file references in output (if any)
	if execErr == nil {
		output := streamer.Content()
		if fileref.HasFiles(output) {
			result := fileref.ParseOutput(output, workdir)

			// In quiet mode with file-only output, delete the streamer message if it exists
			if quiet && strings.TrimSpace(result.Text) == "" && len(result.Files) > 0 && streamer.MessageID() != 0 {
				deleteMsg := tgbotapi.NewDeleteMessage(chatID, streamer.MessageID())
				b.api.Request(deleteMsg)
			}

			// Send files
			b.handleFileReferencesWithResult(chatID, result)
		}
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

// sendMediaGroup sends files as a Telegram media group.
// Caption is applied to the first item in the group.
func (b *Bot) sendMediaGroup(chatID int64, files []fileref.FileRef, caption string) error {
	if len(files) == 0 {
		return nil
	}

	logger := slog.With("chat_id", chatID, "file_count", len(files))

	media := make([]any, len(files))
	for i, f := range files {
		var itemCaption string
		if i == 0 {
			itemCaption = caption
		}

		switch f.Type {
		case fileref.FileTypePhoto:
			m := tgbotapi.NewInputMediaPhoto(tgbotapi.FilePath(f.Path))
			m.Caption = itemCaption
			media[i] = m
		case fileref.FileTypeVideo:
			m := tgbotapi.NewInputMediaVideo(tgbotapi.FilePath(f.Path))
			m.Caption = itemCaption
			media[i] = m
		case fileref.FileTypeAudio:
			m := tgbotapi.NewInputMediaAudio(tgbotapi.FilePath(f.Path))
			m.Caption = itemCaption
			media[i] = m
		default:
			m := tgbotapi.NewInputMediaDocument(tgbotapi.FilePath(f.Path))
			m.Caption = itemCaption
			media[i] = m
		}
	}

	mediaGroup := tgbotapi.NewMediaGroup(chatID, media)
	msgs, err := b.api.SendMediaGroup(mediaGroup)
	if err != nil {
		logger.Error("failed to send media group", "error", err)
		return err
	}

	// Track message IDs for cleanup
	if b.msgStore != nil && b.msgStore.Enabled() {
		var msgIDs []int
		for _, m := range msgs {
			msgIDs = append(msgIDs, m.MessageID)
		}
		if err := b.msgStore.AddBatch(chatID, msgIDs); err != nil {
			logger.Warn("failed to store message IDs", "error", err)
		}
	}

	logger.Info("media group sent successfully")
	return nil
}

// DeleteMessage deletes a message from a chat. Implements builtin.MessageDeleter.
func (b *Bot) DeleteMessage(chatID int64, messageID int) error {
	deleteMsg := tgbotapi.NewDeleteMessage(chatID, messageID)
	_, err := b.api.Request(deleteMsg)
	return err
}

// handleFileReferences processes command output for file references and sends them.
// workdir is used to resolve relative file paths.
func (b *Bot) handleFileReferences(chatID int64, output string, workdir string) {
	if !fileref.HasFiles(output) {
		return
	}

	result := fileref.ParseOutput(output, workdir)
	b.handleFileReferencesWithResult(chatID, result)
}

// handleFileReferencesWithResult sends files from a pre-parsed result.
func (b *Bot) handleFileReferencesWithResult(chatID int64, result fileref.ParseResult) {
	logger := slog.With("chat_id", chatID)

	// If there are errors (missing files), send them as a message
	if len(result.Errors) > 0 {
		errorText := strings.Join(result.Errors, "\n")
		b.sendText(chatID, errorText)
	}

	// If no valid files, nothing more to do
	if len(result.Files) == 0 {
		return
	}

	// Group files and send each group
	groups := fileref.GroupFiles(result.Files, b.defaults.MaxFilesPerGroup)
	for i, group := range groups {
		caption := ""
		if i == 0 && result.Text != "" {
			// First group gets the caption (cleaned text)
			caption = result.Text
		}

		if err := b.sendMediaGroup(chatID, group, caption); err != nil {
			logger.Error("failed to send media group", "group", i, "error", err)
		}
	}
}

// sendText sends a simple text message and tracks it for cleanup.
func (b *Bot) sendText(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	sent, err := b.api.Send(msg)
	if err != nil {
		slog.Error("failed to send message", "error", err, "chat_id", chatID)
		return
	}

	// Track message for cleanup
	b.trackMessage(chatID, sent.MessageID, msgstore.TypeText)
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

// handleCancelCommand handles the /cancel command to abort argument collection.
func (b *Bot) handleCancelCommand(msg *tgbotapi.Message) {
	chatID := msg.Chat.ID

	if !b.authorizer.IsAllowed(chatID) {
		return
	}

	if b.argCollector.HasSession(chatID) {
		b.argCollector.CancelSession(chatID)
		b.sendText(chatID, "Command cancelled.")
	} else {
		b.sendText(chatID, "No active command to cancel.")
	}
}

// handleArgumentInput processes text input for argument collection.
func (b *Bot) handleArgumentInput(ctx context.Context, msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	logger := slog.With("chat_id", chatID)

	session := b.argCollector.GetSession(chatID)
	if session == nil {
		return
	}

	// Check if session expired
	if session.IsExpired() {
		b.argCollector.CancelSession(chatID)
		b.sendText(chatID, "Argument collection timed out. Please try again.")
		return
	}

	// Get current argument for sensitive check before processing
	currentArg := session.CurrentArg()

	// Process the input
	errMsg := b.argCollector.ProcessInput(chatID, msg.Text)
	if errMsg != "" {
		// Validation failed, re-prompt
		b.sendText(chatID, fmt.Sprintf("Invalid input: %s\n\n%s", errMsg, currentArg.Description))
		return
	}

	// Delete sensitive message after capturing
	if currentArg != nil && currentArg.Sensitive {
		deleteMsg := tgbotapi.NewDeleteMessage(chatID, msg.MessageID)
		b.api.Request(deleteMsg)
	}

	// Check if all arguments collected
	session = b.argCollector.GetSession(chatID)
	if session == nil || session.IsComplete() {
		b.executeWithArguments(ctx, chatID)
		return
	}

	// Prompt for next argument
	b.promptNextArgument(chatID, session)
	logger.Debug("prompted for next argument")
}

// handleArgumentCallback processes inline keyboard selection for arguments.
func (b *Bot) handleArgumentCallback(ctx context.Context, query *tgbotapi.CallbackQuery) {
	chatID := query.Message.Chat.ID
	messageID := query.Message.MessageID

	session := b.argCollector.GetSession(chatID)
	if session == nil {
		edit := tgbotapi.NewEditMessageText(chatID, messageID, "Session expired. Please start over.")
		b.api.Send(edit)
		return
	}

	// Extract selected value
	value := ParseArgumentCallback(query.Data)

	// Process the input
	errMsg := b.argCollector.ProcessInput(chatID, value)
	if errMsg != "" {
		// Shouldn't happen with button selection, but handle it
		edit := tgbotapi.NewEditMessageText(chatID, messageID, fmt.Sprintf("Invalid selection: %s", errMsg))
		b.api.Send(edit)
		return
	}

	// Update the message to show selection
	currentArg := session.CurrentArg()
	argName := "argument"
	if currentArg != nil {
		argName = currentArg.Name
	}
	edit := tgbotapi.NewEditMessageText(chatID, messageID, fmt.Sprintf("Selected %s: %s", argName, value))
	b.api.Send(edit)

	// Check if all arguments collected
	session = b.argCollector.GetSession(chatID)
	if session == nil || session.IsComplete() {
		b.executeWithArguments(ctx, chatID)
		return
	}

	// Prompt for next argument
	b.promptNextArgument(chatID, session)
}

// promptNextArgument sends the prompt for the current argument.
func (b *Bot) promptNextArgument(chatID int64, session *ArgumentSession) {
	arg := session.CurrentArg()
	if arg == nil {
		return
	}

	// Build prompt text and keyboard
	var text string
	var keyboard *tgbotapi.InlineKeyboardMarkup

	if arg.Type == "choice" && len(arg.Choices) > 0 {
		if len(arg.Choices) <= maxInlineChoices {
			text = BuildArgumentPrompt(arg)
			keyboard = BuildChoiceKeyboard(arg)
		} else {
			text = BuildChoiceTextList(arg)
		}
	} else {
		text = BuildArgumentPrompt(arg)
	}

	msg := tgbotapi.NewMessage(chatID, text)
	if keyboard != nil {
		msg.ReplyMarkup = keyboard
	}

	if sent, err := b.api.Send(msg); err == nil {
		b.argCollector.SetLastPromptMsgID(chatID, sent.MessageID)
	}
}

// executeWithArguments executes a command with collected arguments.
func (b *Bot) executeWithArguments(ctx context.Context, chatID int64) {
	collected, cmd := b.argCollector.CompleteSession(chatID)
	if cmd == nil {
		return
	}

	logger := slog.With("chat_id", chatID, "command", cmd.Name())

	// Render command template with arguments
	rendered, err := RenderCommand(cmd.CommandTemplate(), collected)
	if err != nil {
		logger.Error("failed to render command template", "error", err)
		b.sendText(chatID, fmt.Sprintf("Failed to process command: %v", err))
		return
	}

	logger.Info("executing command with arguments", "args", collected)

	// Check if command requires confirmation
	if cmd.Metadata().RequireConfirm {
		// Store rendered command for execution after confirmation
		if err := b.confirmMgr.RequestConfirmationWithRendered(b.api, chatID, cmd.Name(), rendered); err != nil {
			logger.Error("failed to request confirmation", "error", err)
		}
		return
	}

	// Execute the rendered command
	b.executeRenderedCommand(ctx, chatID, cmd, rendered)
	b.sendMenu(chatID)
}

// executeRenderedCommand runs a command with a pre-rendered command string.
func (b *Bot) executeRenderedCommand(ctx context.Context, chatID int64, cmd *command.YAMLCommand, rendered string) {
	logger := slog.With("chat_id", chatID, "command", cmd.Name())

	// Get timeout from metadata or use default
	timeout := b.defaults.Timeout
	meta := cmd.Metadata()
	if meta.Timeout > 0 {
		timeout = meta.Timeout
	}

	// Execute command with streaming output
	streamer := NewMessageStreamer(b.api, chatID)
	if err := streamer.Start(ctx); err != nil {
		logger.Error("failed to start streamer", "error", err)
		return
	}

	// Track the output message for cleanup
	b.trackMessage(chatID, streamer.MessageID(), msgstore.TypeText)

	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Execute with rendered command
	execErr := cmd.ExecuteRendered(execCtx, rendered, streamer)
	if execErr != nil {
		logger.Error("command execution failed", "error", execErr)
		fmt.Fprintf(streamer, "\n\nError: %v", execErr)
	}

	if err := streamer.Flush(); err != nil {
		logger.Error("failed to flush output", "error", err)
	}

	// Handle file references in output (if any)
	if execErr == nil {
		b.handleFileReferences(chatID, streamer.Content(), cmd.Workdir())
	}

	// Handle file response if command supports it
	if execErr == nil {
		if resp := cmd.FileResponse(); resp != nil && resp.Path != "" {
			b.sendAudioFile(chatID, resp)
		}
	}
}

// ExecuteScheduled runs a command for scheduled execution.
// Used by the scheduler for timed command execution.
func (b *Bot) ExecuteScheduled(ctx context.Context, chatID int64, cmd pkgcmd.Command) error {
	logger := slog.With("chat_id", chatID, "command", cmd.Name(), "trigger", "schedule")
	logger.Info("executing scheduled command")

	// Check if quiet mode
	quiet := false
	if yamlCmd, ok := cmd.(*command.YAMLCommand); ok {
		quiet = yamlCmd.Quiet()
	}

	// Send notification unless quiet
	if !quiet {
		b.sendText(chatID, fmt.Sprintf("Scheduled: Running /%s...", cmd.Name()))
	}

	// Execute command (confirmation is skipped for scheduled runs)
	b.executeCommandWithOptions(ctx, chatID, cmd, nil, quiet)

	return nil
}

// showCleanupMenu displays the cleanup options menu.
func (b *Bot) showCleanupMenu(chatID int64, messageID int) {
	if b.cleanupCmd == nil || !b.cleanupCmd.Enabled() {
		edit := tgbotapi.NewEditMessageText(chatID, messageID, "Cleanup is not enabled. Set message_store_path in config.")
		b.api.Send(edit)
		return
	}

	// Get tracked message count
	count := b.cleanupCmd.Count(chatID)

	text := fmt.Sprintf("Cleanup tracked files\n\nTracked messages: %d\n\nSelect what to delete:", count)

	// Build options keyboard
	options := builtin.CleanupOptions()
	var rows [][]tgbotapi.InlineKeyboardButton

	for _, opt := range options {
		// Get count for this option
		entries := b.cleanupCmd.GetEntriesToDelete(chatID, opt.Option)
		label := fmt.Sprintf("%s (%d)", opt.Label, len(entries))
		btn := tgbotapi.NewInlineKeyboardButtonData(label, CleanupCallbackData(string(opt.Option)))
		rows = append(rows, []tgbotapi.InlineKeyboardButton{btn})
	}

	// Back button
	backBtn := tgbotapi.NewInlineKeyboardButtonData("<< Back to Menu", backToMenu)
	rows = append(rows, []tgbotapi.InlineKeyboardButton{backBtn})

	keyboard := tgbotapi.NewInlineKeyboardMarkup(rows...)

	edit := tgbotapi.NewEditMessageText(chatID, messageID, text)
	edit.ReplyMarkup = &keyboard
	b.api.Send(edit)
}

// handleCleanupCallback processes a cleanup option selection.
func (b *Bot) handleCleanupCallback(chatID int64, messageID int, option string, logger *slog.Logger) {
	if b.cleanupCmd == nil || !b.cleanupCmd.Enabled() {
		edit := tgbotapi.NewEditMessageText(chatID, messageID, "Cleanup is not enabled.")
		b.api.Send(edit)
		return
	}

	// Execute cleanup
	cleanupOption := builtin.CleanupOption(option)
	deleted, failed, err := b.cleanupCmd.ExecuteCleanup(chatID, cleanupOption)

	var resultText string
	if err != nil {
		resultText = fmt.Sprintf("Cleanup failed: %v", err)
		logger.Error("cleanup failed", "error", err)
	} else if deleted == 0 && failed == 0 {
		resultText = "No messages to delete."
	} else {
		resultText = fmt.Sprintf("Cleanup complete.\n\nDeleted: %d messages", deleted)
		if failed > 0 {
			resultText += fmt.Sprintf("\nFailed: %d (messages may already be deleted or too old)", failed)
		}
	}

	edit := tgbotapi.NewEditMessageText(chatID, messageID, resultText)
	b.api.Send(edit)

	// Show menu again after a moment
	b.sendMenu(chatID)
}

// showScheduleMenu displays options for a scheduled command.
func (b *Bot) showScheduleMenu(chatID int64, cmd *command.YAMLCommand) {
	// Build status text
	var statusParts []string
	if len(cmd.Schedule()) > 0 {
		statusParts = append(statusParts, fmt.Sprintf("Schedule: %s", strings.Join(cmd.Schedule(), ", ")))
	}
	if cmd.Interval() > 0 {
		statusParts = append(statusParts, fmt.Sprintf("Interval: %s", cmd.Interval()))
	}

	// Check pause state
	isPaused := false
	if b.scheduler != nil {
		isPaused = b.scheduler.IsPaused(cmd.Name())
	}

	if isPaused {
		statusParts = append(statusParts, "Status: Paused")
	} else {
		statusParts = append(statusParts, "Status: Running")
	}

	text := fmt.Sprintf("/%s\n\n%s\n\nSelect action:", cmd.Name(), strings.Join(statusParts, "\n"))

	// Build keyboard
	var rows [][]tgbotapi.InlineKeyboardButton

	// Run now button
	runBtn := tgbotapi.NewInlineKeyboardButtonData("▶ Run now", ScheduleCallbackData("run", cmd.Name()))
	rows = append(rows, []tgbotapi.InlineKeyboardButton{runBtn})

	// Pause/Resume button
	if isPaused {
		resumeBtn := tgbotapi.NewInlineKeyboardButtonData("▶ Resume schedule", ScheduleCallbackData("resume", cmd.Name()))
		rows = append(rows, []tgbotapi.InlineKeyboardButton{resumeBtn})
	} else {
		pauseBtn := tgbotapi.NewInlineKeyboardButtonData("⏸ Pause schedule", ScheduleCallbackData("pause", cmd.Name()))
		rows = append(rows, []tgbotapi.InlineKeyboardButton{pauseBtn})
	}

	// Back button
	backBtn := tgbotapi.NewInlineKeyboardButtonData("<< Back to Menu", backToMenu)
	rows = append(rows, []tgbotapi.InlineKeyboardButton{backBtn})

	keyboard := tgbotapi.NewInlineKeyboardMarkup(rows...)

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ReplyMarkup = keyboard
	b.api.Send(msg)
}

// handleScheduleCallback processes schedule menu callbacks.
func (b *Bot) handleScheduleCallback(ctx context.Context, query *tgbotapi.CallbackQuery) {
	chatID := query.Message.Chat.ID
	messageID := query.Message.MessageID
	logger := slog.With("chat_id", chatID, "callback", query.Data)

	action, cmdName := ParseScheduleCallback(query.Data)
	if cmdName == "" {
		logger.Warn("invalid schedule callback data")
		return
	}

	cmd := b.registry.Get(cmdName)
	if cmd == nil {
		logger.Warn("command not found", "command", cmdName)
		edit := tgbotapi.NewEditMessageText(chatID, messageID, "Command not found.")
		b.api.Send(edit)
		return
	}

	yamlCmd, ok := cmd.(*command.YAMLCommand)
	if !ok {
		logger.Warn("command is not a YAML command", "command", cmdName)
		return
	}

	switch action {
	case "run":
		// Delete menu and execute command
		deleteMsg := tgbotapi.NewDeleteMessage(chatID, messageID)
		b.api.Request(deleteMsg)

		logger.Info("executing scheduled command manually", "command", cmdName)
		quiet := yamlCmd.Quiet()
		if !quiet {
			b.sendText(chatID, fmt.Sprintf("Running /%s...", cmdName))
		}
		b.executeCommandWithOptions(ctx, chatID, cmd, nil, quiet)
		b.sendMenu(chatID)

	case "pause":
		if b.scheduler != nil {
			b.scheduler.SetPaused(cmdName, true)
		}
		logger.Info("paused scheduled command", "command", cmdName)

		// Refresh the schedule menu
		deleteMsg := tgbotapi.NewDeleteMessage(chatID, messageID)
		b.api.Request(deleteMsg)
		b.showScheduleMenu(chatID, yamlCmd)

	case "resume":
		if b.scheduler != nil {
			b.scheduler.SetPaused(cmdName, false)
		}
		logger.Info("resumed scheduled command", "command", cmdName)

		// Refresh the schedule menu
		deleteMsg := tgbotapi.NewDeleteMessage(chatID, messageID)
		b.api.Request(deleteMsg)
		b.showScheduleMenu(chatID, yamlCmd)
	}
}

// trackMessage stores a message ID for later cleanup.
func (b *Bot) trackMessage(chatID int64, messageID int, msgType msgstore.MessageType) {
	if b.msgStore == nil || !b.msgStore.Enabled() {
		return
	}
	if err := b.msgStore.AddWithType(chatID, messageID, msgType); err != nil {
		slog.Warn("failed to track message", "chat_id", chatID, "message_id", messageID, "error", err)
	}
}
