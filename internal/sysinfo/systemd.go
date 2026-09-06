package sysinfo

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// SystemdStatus reports the state of the user systemd timer
type SystemdStatus struct {
	Available   bool   `json:"available"`
	Active      bool   `json:"active"`
	NextRun     string `json:"next_run"`
	ServiceFile string `json:"service_file"`
	TimerFile   string `json:"timer_file"`
}

// CheckSystemdTimer inspects systemctl --user for castor.timer
func CheckSystemdTimer() SystemdStatus {
	home, _ := os.UserHomeDir()
	unitDir := filepath.Join(home, ".config", "systemd", "user")
	serviceFile := filepath.Join(unitDir, "castor.service")
	timerFile := filepath.Join(unitDir, "castor.timer")

	if _, err := exec.LookPath("systemctl"); err != nil {
		return SystemdStatus{Available: false}
	}

	cmd := exec.Command("systemctl", "--user", "is-active", "castor.timer")
	out, err := cmd.Output()
	active := err == nil && strings.TrimSpace(string(out)) == "active"

	nextRun := ""
	if active {
		listCmd := exec.Command("systemctl", "--user", "list-timers", "--no-legend", "castor.timer")
		if listOut, err := listCmd.Output(); err == nil {
			fields := strings.Fields(string(listOut))
			if len(fields) >= 5 {
				nextRun = fmt.Sprintf("%s %s %s", fields[0], fields[1], fields[2])
			}
		}
	}

	return SystemdStatus{
		Available:   true,
		Active:      active,
		NextRun:     nextRun,
		ServiceFile: serviceFile,
		TimerFile:   timerFile,
	}
}

// GenerateSystemdUnits writes the castor.service and castor.timer user files
func GenerateSystemdUnits(castorBinaryPath string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	unitDir := filepath.Join(home, ".config", "systemd", "user")
	if err := os.MkdirAll(unitDir, 0755); err != nil {
		return err
	}

	if castorBinaryPath == "" {
		if path, err := exec.LookPath("castor"); err == nil {
			castorBinaryPath = path
		} else {
			castorBinaryPath = "/usr/local/bin/castor"
		}
	}

	serviceContent := fmt.Sprintf(`[Unit]
Description=Castor Cold-Storage Vault Archiver
ConditionACPower=true
After=network-online.target

[Service]
Type=oneshot
ExecStart=%s push
Nice=19
IOSchedulingClass=idle
StandardOutput=journal
StandardError=journal
`, castorBinaryPath)

	timerContent := `[Unit]
Description=Run Castor Archiver Nightly

[Timer]
OnCalendar=*-*-* 03:00:00
RandomizedDelaySec=1800
Persistent=true

[Install]
WantedBy=timers.target
`

	servicePath := filepath.Join(unitDir, "castor.service")
	if err := os.WriteFile(servicePath, []byte(serviceContent), 0644); err != nil {
		return err
	}

	timerPath := filepath.Join(unitDir, "castor.timer")
	if err := os.WriteFile(timerPath, []byte(timerContent), 0644); err != nil {
		return err
	}

	// Reload systemd daemon
	_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
	return nil
}
