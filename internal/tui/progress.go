package tui

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// TargetProgress tracks streaming status of an individual target
type TargetProgress struct {
	Name         string
	BytesStreamed int64
	TotalBytes   int64
	SpeedBytesSec float64
	Done         bool
	Error        error
}

type dashboardModel struct {
	targets map[string]*TargetProgress
	order   []string
	spinner spinner.Model
	bar     progress.Model
	mu      sync.Mutex
	done    bool
}

// LiveDashboard manages live terminal feedback for concurrent push streams
type LiveDashboard struct {
	model *dashboardModel
	prog  *tea.Program
	isTTY bool
}

type progressUpdateMsg struct {
	name   string
	bytes  int64
	done   bool
	err    error
}

// NewLiveDashboard initializes the live push dashboard
func NewLiveDashboard(targetNames []string) *LiveDashboard {
	if !IsTTY() {
		return &LiveDashboard{isTTY: false}
	}

	targetsMap := make(map[string]*TargetProgress)
	for _, name := range targetNames {
		targetsMap[name] = &TargetProgress{
			Name: name,
		}
	}

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(ColorAccent)

	bar := progress.New(
		progress.WithGradient(string(ColorPrimary), string(ColorAccent)),
		progress.WithWidth(28),
		progress.WithoutPercentage(),
	)

	m := &dashboardModel{
		targets: targetsMap,
		order:   targetNames,
		spinner: s,
		bar:     bar,
	}

	p := tea.NewProgram(m)
	go func() {
		_, _ = p.Run()
	}()

	return &LiveDashboard{
		model: m,
		prog:  p,
		isTTY: true,
	}
}

// Update sends an incremental byte count or completion signal for a target
func (ld *LiveDashboard) Update(name string, bytesStreamed int64, done bool, err error) {
	if !ld.isTTY {
		if done {
			if err != nil {
				fmt.Printf("✖ %s: failed: %v\n", name, err)
			} else {
				fmt.Printf("✔ %s: synced %s\n", name, FormatBytes(bytesStreamed))
			}
		}
		return
	}

	if ld.prog != nil {
		ld.prog.Send(progressUpdateMsg{
			name:  name,
			bytes: bytesStreamed,
			done:  done,
			err:   err,
		})
	}
}

// Stop finalizes and terminates the live dashboard
func (ld *LiveDashboard) Stop() {
	if ld.isTTY && ld.prog != nil {
		ld.prog.Send(tea.Quit())
		time.Sleep(50 * time.Millisecond)
	}
}

func (m *dashboardModel) Init() tea.Cmd {
	return m.spinner.Tick
}

func (m *dashboardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case progressUpdateMsg:
		m.mu.Lock()
		if t, ok := m.targets[msg.name]; ok {
			t.BytesStreamed = msg.bytes
			t.Done = msg.done
			t.Error = msg.err
		}
		m.mu.Unlock()
		return m, nil

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
	}

	return m, nil
}

func (m *dashboardModel) View() string {
	m.mu.Lock()
	defer m.mu.Unlock()

	var sb strings.Builder
	sb.WriteString(StyleTitle.Render("🦫 Castor · Streaming Targets to Cloud Vault") + "\n\n")

	for _, name := range m.order {
		t := m.targets[name]
		if t == nil {
			continue
		}

		statusIcon := m.spinner.View()
		detail := StyleDim.Render(fmt.Sprintf("%s streamed", FormatBytes(t.BytesStreamed)))

		if t.Done {
			if t.Error != nil {
				statusIcon = StyleBadgeDanger.Render("✖")
				detail = lipgloss.NewStyle().Foreground(ColorDanger).Render(t.Error.Error())
			} else {
				statusIcon = lipgloss.NewStyle().Foreground(ColorSuccess).Render("✔")
				detail = lipgloss.NewStyle().Foreground(ColorSuccess).Render(fmt.Sprintf("%s synced", FormatBytes(t.BytesStreamed)))
			}
		}

		line := fmt.Sprintf("  %s %-32s %s\n", statusIcon, name, detail)
		sb.WriteString(line)
	}

	return sb.String()
}
