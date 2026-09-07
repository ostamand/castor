package tui

import (
	"strings"
	"testing"
)

func TestLiveDashboardViewFormatting(t *testing.T) {
	targets := []string{"castor", "coloring-pages"}
	totals := map[string]int64{
		"castor":         766 * 1024,
		"coloring-pages": 171 * 1024 * 1024,
	}

	m := &dashboardModel{
		targets: map[string]*TargetProgress{
			"castor": {
				Name:          "castor",
				BytesStreamed: 245 * 1024,
				TotalBytes:    totals["castor"],
				Done:          true,
			},
			"coloring-pages": {
				Name:          "coloring-pages",
				Destinations:  "local-nas",
				BytesStreamed: 128 * 1024 * 1024,
				TotalBytes:    totals["coloring-pages"],
				Done:          false,
			},
		},
		order: targets,
	}

	view := m.View()

	// castor should be marked synced
	if !strings.Contains(view, "castor") || !strings.Contains(view, "synced") {
		t.Errorf("expected castor to show synced, got:\n%s", view)
	}

	// coloring-pages should show streamed out of total bytes and destination
	if !strings.Contains(view, "coloring-pages") {
		t.Errorf("expected coloring-pages in view, got:\n%s", view)
	}
	if !strings.Contains(view, "local-nas") {
		t.Errorf("expected 'local-nas' destination in view, got:\n%s", view)
	}
	if !strings.Contains(view, "128.0 MB / 171.0 MB streamed") {
		t.Errorf("expected '128.0 MB / 171.0 MB streamed' in view, got:\n%s", view)
	}
}
