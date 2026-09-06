package tui

import (
	"fmt"
	"os"

	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

// IsTTY returns true if stdout is an interactive terminal
func IsTTY() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

// Color Palette - Castor Beaver & Cold Vault Theme
var (
	ColorPrimary   = lipgloss.Color("#D97706") // Warm Beaver Amber
	ColorSecondary = lipgloss.Color("#92400E") // Deep Timber Wood
	ColorAccent    = lipgloss.Color("#F59E0B") // Golden Lodge Glow
	ColorSuccess   = lipgloss.Color("#10B981") // Forest Green
	ColorWarning   = lipgloss.Color("#F97316") // Safety Orange
	ColorDanger    = lipgloss.Color("#EF4444") // Coral Red
	ColorMuted     = lipgloss.Color("#6B7280") // Slate Gray
	ColorHighlight = lipgloss.Color("#06B6D4") // Cold Storage Cyan
	ColorDarkBg    = lipgloss.Color("#1F2937") // Night Slate
)

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
