package sysinfo

import (
	"os/exec"
	"runtime"
)

// SendDesktopAlert sends a desktop notification on Linux/Unix using notify-send
func SendDesktopAlert(title, message string, critical bool) {
	if runtime.GOOS != "linux" {
		return
	}

	urgency := "normal"
	if critical {
		urgency = "critical"
	}

	// Check if notify-send is present in PATH
	if _, err := exec.LookPath("notify-send"); err != nil {
		return
	}

	_ = exec.Command(
		"notify-send",
		"-u", urgency,
		"-a", "Castor",
		"-i", "dialog-warning",
		title,
		message,
	).Run()
}
