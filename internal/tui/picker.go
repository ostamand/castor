package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ArchiveItem represents a remote cloud archive available for restoration
type ArchiveItem struct {
	CanonicalKey string
	ArchiveName  string
	Namespace    string
	Size         int64
	Updated      time.Time
	OriginPath   string
	GitRef       string
	Status       string
}

type pickerModel struct {
	items            []ArchiveItem
	filtered         []ArchiveItem
	currentNamespace string
	filter           string
	cursor           int
	selected         *ArchiveItem
	quitting         bool
}

// RunArchivePicker launches the interactive archive explorer for castor pull
func RunArchivePicker(items []ArchiveItem, currentNamespace string) (*ArchiveItem, error) {
	if len(items) == 0 {
		return nil, fmt.Errorf("no cloud archives found")
	}

	p := tea.NewProgram(pickerModel{
		items:            items,
		filtered:         items,
		currentNamespace: currentNamespace,
		cursor:           0,
	})

	m, err := p.Run()
	if err != nil {
		return nil, err
	}

	finalModel := m.(pickerModel)
	if finalModel.quitting || finalModel.selected == nil {
		return nil, nil
	}

	return finalModel.selected, nil
}

func (m pickerModel) Init() tea.Cmd {
	return nil
}

func (m pickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			m.quitting = true
			return m, tea.Quit

		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}

		case "down", "j":
			if m.cursor < len(m.filtered)-1 {
				m.cursor++
			}

		case "enter":
			if len(m.filtered) > 0 {
				item := m.filtered[m.cursor]
				m.selected = &item
			}
			return m, tea.Quit

		case "backspace":
			if len(m.filter) > 0 {
				m.filter = m.filter[:len(m.filter)-1]
				m.applyFilter()
			}

		default:
			if len(msg.String()) == 1 {
				m.filter += msg.String()
				m.applyFilter()
			}
		}
	}

	return m, nil
}

func (m *pickerModel) applyFilter() {
	if m.filter == "" {
		m.filtered = m.items
		m.cursor = 0
		return
	}

	query := strings.ToLower(m.filter)
	var matching []ArchiveItem
	for _, it := range m.items {
		if strings.Contains(strings.ToLower(it.CanonicalKey), query) ||
			strings.Contains(strings.ToLower(it.GitRef), query) ||
			strings.Contains(strings.ToLower(it.Namespace), query) {
			matching = append(matching, it)
		}
	}
	m.filtered = matching
	if m.cursor >= len(m.filtered) {
		m.cursor = 0
	}
}

func (m pickerModel) View() string {
	if m.quitting {
		return StyleDim.Render("Restore cancelled.\n")
	}

	var sb strings.Builder

	title := StyleTitle.Render(fmt.Sprintf("🦫 Castor · Cloud Archive Explorer (Namespace: %s)", m.currentNamespace))
	sb.WriteString(title + "\n\n")

	// Search bar
	searchBar := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorPrimary).
		Padding(0, 1).
		Render(fmt.Sprintf("Search: %s█", m.filter))
	sb.WriteString(searchBar + "\n\n")

	if len(m.filtered) == 0 {
		sb.WriteString(StyleDim.Render("  No archives match current search.\n"))
		return sb.String()
	}

	// Archive items
	for i, item := range m.filtered {
		cursor := "  "
		itemStyle := lipgloss.NewStyle()
		if i == m.cursor {
			cursor = lipgloss.NewStyle().Foreground(ColorPrimary).Bold(true).Render("▸ ")
			itemStyle = itemStyle.Bold(true).Foreground(ColorAccent)
		}

		timeAgo := item.Updated.Format("2006-01-02 15:04")
		sizeStr := FormatBytes(item.Size)

		line := fmt.Sprintf("%s%-38s %-10s %-16s",
			cursor, itemStyle.Render(item.CanonicalKey), sizeStr, StyleDim.Render(timeAgo))

		if item.GitRef != "" {
			line += " " + StyleBadgeInfo.Render(item.GitRef)
		}

		sb.WriteString(line + "\n")
	}

	// Preview pane of currently selected item
	if len(m.filtered) > 0 && m.cursor < len(m.filtered) {
		curr := m.filtered[m.cursor]
		details := fmt.Sprintf("Key: %s\nSize: %s · Last Updated: %s\nOrigin: %s",
			curr.CanonicalKey, FormatBytes(curr.Size), curr.Updated.Format(time.RFC822), curr.OriginPath)
		sb.WriteString("\n" + StyleCard.Render(details) + "\n")
	}

	sb.WriteString(StyleDim.Render("[↑/↓] Navigate · [Type] Search · [Enter] Restore Selected · [Esc] Quit\n"))

	return sb.String()
}
