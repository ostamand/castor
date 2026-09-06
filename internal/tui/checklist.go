package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// DiscoveredTarget represents a candidate directory found during discovery
type DiscoveredTarget struct {
	Name           string
	Path           string
	Type           string
	EstimatedBytes int64
	Selected       bool
	Conflict       bool
	ConflictDetail string
}

type checklistModel struct {
	namespace  string
	candidates []DiscoveredTarget
	cursor     int
	filter     string
	filtering  bool
	confirmed  bool
	quitting   bool
	width      int
	height     int
}

// RunChecklist launches an interactive Bubble Tea checklist for reviewing discovered targets
func RunChecklist(candidates []DiscoveredTarget, namespace string) ([]DiscoveredTarget, bool, error) {
	if len(candidates) == 0 {
		return nil, false, nil
	}

	p := tea.NewProgram(checklistModel{
		namespace:  namespace,
		candidates: candidates,
		cursor:     0,
		width:      80,
		height:     24,
	})

	m, err := p.Run()
	if err != nil {
		return nil, false, err
	}

	finalModel := m.(checklistModel)
	if !finalModel.confirmed {
		return nil, false, nil
	}

	return finalModel.candidates, true, nil
}

func (m checklistModel) Init() tea.Cmd {
	return nil
}

func (m checklistModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tea.KeyMsg:
		if m.filtering {
			switch msg.String() {
			case "enter", "esc":
				m.filtering = false
			case "backspace":
				if len(m.filter) > 0 {
					m.filter = m.filter[:len(m.filter)-1]
				}
			default:
				if len(msg.String()) == 1 {
					m.filter += msg.String()
				}
			}
			return m, nil
		}

		switch msg.String() {
		case "ctrl+c", "q":
			m.quitting = true
			return m, tea.Quit

		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}

		case "down", "j":
			if m.cursor < len(m.candidates)-1 {
				m.cursor++
			}

		case " ":
			if len(m.candidates) > 0 && !m.candidates[m.cursor].Conflict {
				m.candidates[m.cursor].Selected = !m.candidates[m.cursor].Selected
			}

		case "a": // Select all non-conflicting
			for i := range m.candidates {
				if !m.candidates[i].Conflict {
					m.candidates[i].Selected = true
				}
			}

		case "n": // Deselect all
			for i := range m.candidates {
				m.candidates[i].Selected = false
			}

		case "/":
			m.filtering = true

		case "enter":
			m.confirmed = true
			return m, tea.Quit
		}
	}

	return m, nil
}

func (m checklistModel) View() string {
	if m.quitting {
		return StyleDim.Render("Checklist cancelled.\n")
	}

	var sb strings.Builder

	title := StyleTitle.Render(fmt.Sprintf("🦫 Castor · Review Discovered Targets (Namespace: %s)", m.namespace))
	sb.WriteString(title + "\n\n")

	if m.filtering {
		sb.WriteString(lipgloss.NewStyle().Foreground(ColorAccent).Render(fmt.Sprintf("Filter: %s█\n\n", m.filter)))
	} else if m.filter != "" {
		sb.WriteString(StyleDim.Render(fmt.Sprintf("Active Filter: '%s' (press / to edit)\n\n", m.filter)))
	}

	// Render candidates list
	for i, c := range m.candidates {
		cursor := "  "
		if i == m.cursor {
			cursor = lipgloss.NewStyle().Foreground(ColorPrimary).Bold(true).Render("▸ ")
		}

		checkbox := "[ ]"
		if c.Conflict {
			checkbox = StyleBadgeWarning.Render("!")
		} else if c.Selected {
			checkbox = lipgloss.NewStyle().Foreground(ColorSuccess).Bold(true).Render("[X]")
		}

		typeTag := StyleDim.Render(fmt.Sprintf("(%s · %s)", c.Type, FormatBytes(c.EstimatedBytes)))
		nameStyle := lipgloss.NewStyle()
		if i == m.cursor {
			nameStyle = nameStyle.Bold(true).Foreground(ColorAccent)
		}

		line := fmt.Sprintf("%s%s %-24s %s", cursor, checkbox, nameStyle.Render(c.Name), typeTag)
		if c.Conflict {
			line += " " + lipgloss.NewStyle().Foreground(ColorWarning).Render("• "+c.ConflictDetail)
		}

		sb.WriteString(line + "\n")
	}

	// Selected count summary
	selectedCount := 0
	for _, c := range m.candidates {
		if c.Selected {
			selectedCount++
		}
	}

	sb.WriteString("\n" + StyleCard.Render(
		fmt.Sprintf("Selected: %d/%d targets\nControls: [Space] Toggle · [a] All · [n] None · [/] Filter · [Enter] Confirm · [q] Quit",
			selectedCount, len(m.candidates)),
	) + "\n")

	return sb.String()
}
