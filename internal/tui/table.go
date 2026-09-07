package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// RenderTable formats data into a clean, aligned Lipgloss terminal table
func RenderTable(headers []string, rows [][]string) string {
	if len(headers) == 0 {
		return ""
	}

	// Calculate maximum width per column taking ANSI styling into account
	colWidths := make([]int, len(headers))
	for i, h := range headers {
		colWidths[i] = lipgloss.Width(h)
	}

	for _, row := range rows {
		for i, cell := range row {
			w := lipgloss.Width(cell)
			if i < len(colWidths) && w > colWidths[i] {
				colWidths[i] = w
			}
		}
	}

	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorPrimary).
		BorderBottom(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(ColorSecondary)

	var sb strings.Builder

	// Header row
	var headerCells []string
	for i, h := range headers {
		padding := colWidths[i] - lipgloss.Width(h)
		if padding < 0 {
			padding = 0
		}
		headerCells = append(headerCells, h+strings.Repeat(" ", padding))
	}
	sb.WriteString(headerStyle.Render(strings.Join(headerCells, "   ")) + "\n")

	// Data rows
	for _, row := range rows {
		var rowCells []string
		for i, cell := range row {
			padding := colWidths[i] - lipgloss.Width(cell)
			if padding < 0 {
				padding = 0
			}
			rowCells = append(rowCells, cell+strings.Repeat(" ", padding))
		}
		sb.WriteString(strings.Join(rowCells, "   ") + "\n")
	}

	return sb.String()
}

// BadgeStatus returns a colored status badge for vault health
func BadgeStatus(status string) string {
	switch strings.ToUpper(status) {
	case "SYNCED", "ONLINE", "ACTIVE", "HEALTHY":
		return StyleBadgeSuccess.Render(" " + status + " ")
	case "DRIFT", "WARNING", "MODIFIED", "DESYNC", "PARTIAL":
		return StyleBadgeWarning.Render(" " + status + " ")
	case "ORPHAN", "UNTRACKED":
		return StyleBadgeInfo.Render(" " + status + " ")
	case "FAIL", "ERROR", "OFFLINE":
		return StyleBadgeDanger.Render(" " + status + " ")
	default:
		return StyleDim.Render("[" + status + "]")
	}
}
