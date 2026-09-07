package tui

import (
	"fmt"
	"math/rand"

	"github.com/charmbracelet/lipgloss"
)

var didYouKnowTips = []string{
	"Compare commit drift against cloud archives in milliseconds without downloading using 'castor diff <target>'.",
	"Inspect archive files and permissions directly in memory with zero disk footprint using 'castor inspect <target>'.",
	"Extract a single file directly to stdout and stop the download immediately with 'castor cat <target> <path>'.",
	"Git targets preserve full commit history, branches, and active stashes via an embedded '.castor/repo.bundle'.",
	"Preview on-disk vs archive payload sizes without uploading data using 'castor push -n'.",
	"Turn on automated background backups on AC power with idle I/O priority via 'castor schedule on'.",
	"Preview and clean up remote archives that were removed from config.toml using 'castor prune -n'.",
	"Verify Age decryption and ciphertext SHA-256 integrity against the cloud anytime using 'castor verify'.",
	"Quickly scan and register multiple Git projects at once with 'castor add ~/Work/git -r --git-only'.",
	"Run 'castor doctor' to verify the health of tools, cloud credentials, systemd timers, and Age keys.",
}

// RandomTip returns a formatted single-line tip
func RandomTip() string {
	idx := rand.Intn(len(didYouKnowTips))
	return FormatTip(didYouKnowTips[idx])
}

// FormatTip formats a tip with the Castor orange badge and subtle text
func FormatTip(tip string) string {
	prefix := lipgloss.NewStyle().Bold(true).Foreground(ColorAccent).Render("💡 Did you know?")
	text := StyleDim.Render(tip)
	return fmt.Sprintf("%s %s", prefix, text)
}

// MaybePrintTip displays a helpful tip roughly 35% of the time in interactive terminals
func MaybePrintTip(enabled bool) {
	if !IsTTY() || !enabled {
		return
	}
	// Show roughly 35% of the time to avoid being spammy
	if rand.Float64() > 0.35 {
		return
	}
	fmt.Printf("\n%s\n", RandomTip())
}
