package tui

import (
	"strings"
	"testing"
)

func TestFormatTip(t *testing.T) {
	tip := "Test tip message for castor archiver."
	formatted := FormatTip(tip)

	if !strings.Contains(formatted, "💡 Did you know?") {
		t.Errorf("expected '💡 Did you know?' prefix, got: %s", formatted)
	}
	if !strings.Contains(formatted, tip) {
		t.Errorf("expected tip content in formatted output, got: %s", formatted)
	}
}

func TestRandomTip(t *testing.T) {
	for i := 0; i < 20; i++ {
		tip := RandomTip()
		if !strings.Contains(tip, "💡 Did you know?") {
			t.Fatalf("iteration %d: expected '💡 Did you know?', got: %s", i, tip)
		}
	}
}
