package builtin

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/rashpile/pako-telegram/internal/msgstore"
)

// CleanupOption represents a cleanup time range option.
type CleanupOption string

const (
	CleanupAll             CleanupOption = "all"
	CleanupLastHour        CleanupOption = "last_hour"
	CleanupLastDay         CleanupOption = "last_day"
	CleanupBeforeLastDay   CleanupOption = "before_last_day"
	CleanupBeforeLastWeek  CleanupOption = "before_last_week"
	CleanupBeforeLastMonth CleanupOption = "before_last_month"
	// All messages (text + files) options
	CleanupAllMsgsLastHour CleanupOption = "all_msgs_last_hour"
	CleanupAllMsgsLastDay  CleanupOption = "all_msgs_last_day"
)

// CleanupOptionInfo contains display information for a cleanup option.
type CleanupOptionInfo struct {
	Option      CleanupOption
	Label       string
	Description string
}

// CleanupOptions returns all available cleanup options with labels.
func CleanupOptions() []CleanupOptionInfo {
	return []CleanupOptionInfo{
		{CleanupAllMsgsLastHour, "All messages (1h)", "Delete ALL messages from the last hour"},
		{CleanupAllMsgsLastDay, "All messages (24h)", "Delete ALL messages from the last 24 hours"},
		{CleanupLastHour, "Files (1h)", "Delete files sent in the last hour"},
		{CleanupLastDay, "Files (24h)", "Delete files sent in the last 24 hours"},
		{CleanupBeforeLastDay, "Files older 1d", "Delete files sent more than 24 hours ago"},
		{CleanupBeforeLastWeek, "Files older 1w", "Delete files sent more than 7 days ago"},
		{CleanupBeforeLastMonth, "Files older 1mo", "Delete files sent more than 30 days ago"},
		{CleanupAll, "All files", "Delete all tracked files"},
	}
}

// MessageDeleter is the interface for deleting Telegram messages.
type MessageDeleter interface {
	DeleteMessage(chatID int64, messageID int) error
}

// CleanupCommand handles deletion of previously sent file messages.
type CleanupCommand struct {
	store   *msgstore.Store
	deleter MessageDeleter
}

// NewCleanupCommand creates a cleanup command.
func NewCleanupCommand(store *msgstore.Store, deleter MessageDeleter) *CleanupCommand {
	return &CleanupCommand{
		store:   store,
		deleter: deleter,
	}
}

// Name returns "cleanup".
func (c *CleanupCommand) Name() string {
	return "cleanup"
}

// Description returns the cleanup command description.
func (c *CleanupCommand) Description() string {
	return "Delete previously sent files from chat"
}

// Enabled returns true if cleanup functionality is available.
func (c *CleanupCommand) Enabled() bool {
	return c.store != nil && c.store.Enabled()
}

// GetEntriesToDelete returns entries matching the specified cleanup option.
func (c *CleanupCommand) GetEntriesToDelete(chatID int64, option CleanupOption) []msgstore.Entry {
	if c.store == nil {
		return nil
	}

	now := time.Now()

	switch option {
	// All messages (text + files) options
	case CleanupAllMsgsLastHour:
		return c.store.GetAfter(chatID, now.Add(-time.Hour))
	case CleanupAllMsgsLastDay:
		return c.store.GetAfter(chatID, now.Add(-24*time.Hour))
	// File-only options (backwards compatible - GetAfterByType filters by type)
	case CleanupAll:
		return c.store.GetAll(chatID) // All tracked (mostly files for backwards compat)
	case CleanupLastHour:
		return c.store.GetAfterByType(chatID, now.Add(-time.Hour), msgstore.TypeFile)
	case CleanupLastDay:
		return c.store.GetAfterByType(chatID, now.Add(-24*time.Hour), msgstore.TypeFile)
	case CleanupBeforeLastDay:
		return c.store.GetBefore(chatID, now.Add(-24*time.Hour))
	case CleanupBeforeLastWeek:
		return c.store.GetBefore(chatID, now.Add(-7*24*time.Hour))
	case CleanupBeforeLastMonth:
		return c.store.GetBefore(chatID, now.Add(-30*24*time.Hour))
	default:
		return nil
	}
}

// Execute shows cleanup status (actual deletion is handled via callbacks).
func (c *CleanupCommand) Execute(ctx context.Context, args []string, output io.Writer) error {
	if !c.Enabled() {
		fmt.Fprintln(output, "Cleanup is not enabled. Set message_store_path in config.")
		return nil
	}

	// This is a placeholder - actual cleanup is done via interactive menu
	fmt.Fprintln(output, "Use the cleanup menu to select what to delete.")
	return nil
}

// ExecuteCleanup performs the actual deletion for the specified option.
func (c *CleanupCommand) ExecuteCleanup(chatID int64, option CleanupOption) (deleted int, failed int, err error) {
	if !c.Enabled() {
		return 0, 0, fmt.Errorf("cleanup not enabled")
	}

	entries := c.GetEntriesToDelete(chatID, option)
	if len(entries) == 0 {
		return 0, 0, nil
	}

	var deletedIDs []int
	for _, entry := range entries {
		if err := c.deleter.DeleteMessage(entry.ChatID, entry.MessageID); err != nil {
			// Message might already be deleted or too old
			failed++
		} else {
			deleted++
		}
		deletedIDs = append(deletedIDs, entry.MessageID)
	}

	// Remove all entries regardless of deletion success
	// (if message doesn't exist, we don't need to track it)
	if err := c.store.Remove(chatID, deletedIDs); err != nil {
		return deleted, failed, fmt.Errorf("failed to update store: %w", err)
	}

	return deleted, failed, nil
}

// Count returns the number of tracked messages for a chat.
func (c *CleanupCommand) Count(chatID int64) int {
	if c.store == nil {
		return 0
	}
	return c.store.Count(chatID)
}