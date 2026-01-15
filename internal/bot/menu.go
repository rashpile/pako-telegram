package bot

import (
	"fmt"
	"strings"
	"unicode"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/rashpile/pako-telegram/internal/command"
	pkgcmd "github.com/rashpile/pako-telegram/pkg/command"
)

// capitalize returns string with first letter uppercase.
func capitalize(s string) string {
	if s == "" {
		return s
	}
	runes := []rune(s)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

const (
	// Callback data prefixes for menu navigation
	menuPrefix     = "menu:"
	categoryPrefix = "cat:"
	commandPrefix  = "cmd:"
	backToMenu     = "menu:main"
)

// MenuBuilder creates inline keyboards for the interactive menu.
type MenuBuilder struct {
	registry *command.Registry
}

// NewMenuBuilder creates a menu builder.
func NewMenuBuilder(registry *command.Registry) *MenuBuilder {
	return &MenuBuilder{registry: registry}
}

// BuildMainMenu creates the main menu keyboard with category buttons.
func (m *MenuBuilder) BuildMainMenu() (string, tgbotapi.InlineKeyboardMarkup) {
	categories := m.registry.Categories()

	var rows [][]tgbotapi.InlineKeyboardButton
	var row []tgbotapi.InlineKeyboardButton

	for _, cat := range categories {
		label := cat.Name
		if cat.Icon != "" {
			label = cat.Icon + " " + cat.Name
		}
		// Capitalize the category name
		label = capitalize(label)

		btn := tgbotapi.NewInlineKeyboardButtonData(label, categoryPrefix+cat.Name)
		row = append(row, btn)

		// 2 buttons per row
		if len(row) == 2 {
			rows = append(rows, row)
			row = nil
		}
	}

	// Add remaining button if odd number
	if len(row) > 0 {
		rows = append(rows, row)
	}

	text := "Select a category:"
	keyboard := tgbotapi.NewInlineKeyboardMarkup(rows...)

	return text, keyboard
}

// BuildCategoryMenu creates a keyboard showing commands in a category.
func (m *MenuBuilder) BuildCategoryMenu(categoryName string) (string, tgbotapi.InlineKeyboardMarkup) {
	cmds := m.registry.ByCategory(categoryName)

	var rows [][]tgbotapi.InlineKeyboardButton

	for _, cmd := range cmds {
		label := "/" + cmd.Name()

		// Add icon if available
		if withCat, ok := cmd.(pkgcmd.WithCategory); ok {
			info := withCat.Category()
			if info.Icon != "" {
				label = info.Icon + " " + label
			}
		}

		// Add warning for confirmation-required commands
		if withMeta, ok := cmd.(pkgcmd.WithMetadata); ok {
			if withMeta.Metadata().RequireConfirm {
				label += " (!)"
			}
		}

		btn := tgbotapi.NewInlineKeyboardButtonData(label, commandPrefix+cmd.Name())
		rows = append(rows, []tgbotapi.InlineKeyboardButton{btn})
	}

	// Back button
	backBtn := tgbotapi.NewInlineKeyboardButtonData("<< Back to Menu", backToMenu)
	rows = append(rows, []tgbotapi.InlineKeyboardButton{backBtn})

	// Build header text with category info
	icon := ""
	for _, cat := range m.registry.Categories() {
		if cat.Name == categoryName {
			icon = cat.Icon
			break
		}
	}

	header := capitalize(categoryName)
	if icon != "" {
		header = icon + " " + header
	}

	text := fmt.Sprintf("%s commands:\n\nTap a command to run it.", header)
	keyboard := tgbotapi.NewInlineKeyboardMarkup(rows...)

	return text, keyboard
}

// BuildCommandConfirmMenu creates a confirmation menu for a command.
func (m *MenuBuilder) BuildCommandConfirmMenu(cmdName string) (string, tgbotapi.InlineKeyboardMarkup, bool) {
	cmd := m.registry.Get(cmdName)
	if cmd == nil {
		return "", tgbotapi.InlineKeyboardMarkup{}, false
	}

	// Check if command requires confirmation
	needsConfirm := false
	if withMeta, ok := cmd.(pkgcmd.WithMetadata); ok {
		needsConfirm = withMeta.Metadata().RequireConfirm
	}

	text := fmt.Sprintf("/%s - %s", cmd.Name(), cmd.Description())

	if needsConfirm {
		// Return empty keyboard, let the confirmation manager handle it
		return text, tgbotapi.InlineKeyboardMarkup{}, true
	}

	return text, tgbotapi.InlineKeyboardMarkup{}, false
}

// ParseCallback extracts the type and value from a callback data string.
func ParseCallback(data string) (callbackType, value string) {
	if strings.HasPrefix(data, categoryPrefix) {
		return "category", strings.TrimPrefix(data, categoryPrefix)
	}
	if strings.HasPrefix(data, commandPrefix) {
		return "command", strings.TrimPrefix(data, commandPrefix)
	}
	if data == backToMenu {
		return "menu", "main"
	}
	if strings.HasPrefix(data, menuPrefix) {
		return "menu", strings.TrimPrefix(data, menuPrefix)
	}
	return "", data
}

// IsMenuCallback checks if the callback is a menu-related callback.
func IsMenuCallback(data string) bool {
	return strings.HasPrefix(data, menuPrefix) ||
		strings.HasPrefix(data, categoryPrefix) ||
		strings.HasPrefix(data, commandPrefix)
}