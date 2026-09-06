package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// RenderTable formats data into a clean, aligned Lipgloss terminal table
func RenderTable(headers []string, rows [][]string) string {
	if len(headers) == 0 {
		return ""
	}

	// Calculate maximum width per column
	colWidths := make([]int, len(headers))
	for i, h := range headers {
		colWidths[i] = len(h)
	}

	for _, row := range rows {
		for i, cell := range row {
			if i < len(colWidths) && len(cell) > colWidths[i] {
				colWidths[i] = len(cell)
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
		headerCells = append(headerCells, fmt.Sprintf("%-*s", colWidths[i], h))
	}
	sb.WriteString(headerStyle.Render(strings.Join(headerCells, "   ")) + "\n")

	// Data rows
	for _, row := range rows {
		var rowCells []string
		for i, cell := range row {
			width := colWidths[i]
			rowCells = append(rowCells, fmt.Sprintf("%-*s", width, cell))
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
	case "DRIFT", "WARNING", "MODIFIED":
		return StyleBadgeWarning.Render(" " + status + " ")
	case "ORPHAN", "UNTRACKED":
		return StyleBadgeInfo.Render(" " + status + " ")
	case "FAIL", "ERROR", "OFFLINE":
		return StyleBadgeDanger.Render(" " + status + " ")
	default:
		return StyleDim.Render("[" + status + "]")
	}
}
