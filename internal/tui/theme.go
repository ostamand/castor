package tui

import (
	"fmt"
	"os"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

// IsTTY returns true if stdout is an interactive terminal
func IsTTY() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

// Color Palette - Castor Beaver & Cold Vault Theme
var (
	ColorPrimary   = lipgloss.Color("#EA580C") // Deep Beaver Orange
	ColorSecondary = lipgloss.Color("#9A3412") // Deep Timber Wood
	ColorAccent    = lipgloss.Color("#F97316") // Vibrant Castor Orange
	ColorSuccess   = lipgloss.Color("#10B981") // Forest Green
	ColorWarning   = lipgloss.Color("#FB923C") // Warm Safety Amber-Orange
	ColorDanger    = lipgloss.Color("#EF4444") // Coral Red
	ColorMuted     = lipgloss.Color("#6B7280") // Slate Gray
	ColorHighlight = lipgloss.Color("#06B6D4") // Cold Storage Cyan
	ColorDarkBg    = lipgloss.Color("#1F2937") // Night Slate
)

// ThemeCastor returns a custom Huh theme matching Castor's orange accent and timber palette
func ThemeCastor() *huh.Theme {
	t := huh.ThemeCharm()
	t.Focused.Base = t.Focused.Base.BorderForeground(ColorAccent)
	t.Focused.Title = t.Focused.Title.Foreground(ColorAccent)
	t.Focused.SelectSelector = t.Focused.SelectSelector.Foreground(ColorAccent)
	t.Focused.MultiSelectSelector = t.Focused.MultiSelectSelector.Foreground(ColorAccent)
	t.Focused.SelectedOption = t.Focused.SelectedOption.Foreground(ColorAccent)
	t.Focused.SelectedPrefix = t.Focused.SelectedPrefix.Foreground(ColorAccent)
	t.Focused.FocusedButton = t.Focused.FocusedButton.Background(ColorAccent).Foreground(lipgloss.Color("#FFFFFF"))
	t.Focused.TextInput.Cursor = t.Focused.TextInput.Cursor.Foreground(ColorAccent)
	t.Focused.TextInput.Prompt = t.Focused.TextInput.Prompt.Foreground(ColorAccent)
	return t
}

// Base Lipgloss Styles
var (
	StyleTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorPrimary).
			MarginBottom(1)

	StyleSubtitle = lipgloss.NewStyle().
			Foreground(ColorMuted).
			Italic(true)

	StyleCard = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorSecondary).
			Padding(1, 2)

	StyleBadgeSuccess = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#FFFFFF")).
				Background(ColorSuccess).
				Padding(0, 1)

	StyleBadgeWarning = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#FFFFFF")).
				Background(ColorWarning).
				Padding(0, 1)

	StyleBadgeDanger = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#FFFFFF")).
				Background(ColorDanger).
				Padding(0, 1)

	StyleBadgeInfo = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(ColorHighlight).
			Padding(0, 1)

	StyleDim = lipgloss.NewStyle().
			Foreground(ColorMuted)

	StyleBold = lipgloss.NewStyle().
			Bold(true)
)

// FormatBytes converts raw byte count into clean human readable string
func FormatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	units := []string{"KB", "MB", "GB", "TB"}
	if exp >= len(units) {
		exp = len(units) - 1
	}
	return fmt.Sprintf("%.1f %s", float64(b)/float64(div), units[exp])
}
