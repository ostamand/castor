package cli

import (
	"fmt"
	"os"

	"github.com/charmbracelet/lipgloss"
	"github.com/ostamand/castor/internal/sysinfo"
	"github.com/ostamand/castor/internal/tui"
	"github.com/spf13/cobra"
)

var scheduleCmd = &cobra.Command{
	Use:     "schedule [enable|disable|status|run]",
	Aliases: []string{"timer"},
	Short:   "Manage background scheduled backups (systemd user timer)",
	Long: `Turns the background backup schedule on or off using Linux systemd user timers.
When enabled, Castor wakes up nightly (03:00 AM) to evaluate target drift and stream archives to cold storage.`,
	RunE: runSchedule,
}

var (
	scheduleEnableCmd = &cobra.Command{
		Use:   "enable",
		Aliases: []string{"on", "start"},
		Short: "Enable and activate the nightly background timer",
		RunE: func(cmd *cobra.Command, args []string) error {
			exe, _ := os.Executable()
			if err := sysinfo.EnableSystemdTimer(exe); err != nil {
				return err
			}
			fmt.Println(lipgloss.NewStyle().Foreground(tui.ColorSuccess).Bold(true).Render("✔ Castor background timer ENABLED."))
			fmt.Println("  Runs nightly at 03:00 AM (AC power only, idle I/O priority).")
			return printScheduleStatus()
		},
	}

	scheduleDisableCmd = &cobra.Command{
		Use:   "disable",
		Aliases: []string{"off", "stop"},
		Short: "Disable and deactivate the nightly background timer",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := sysinfo.DisableSystemdTimer(); err != nil {
				return err
			}
			fmt.Println(lipgloss.NewStyle().Foreground(tui.ColorWarning).Bold(true).Render("✔ Castor background timer DISABLED."))
			fmt.Println("  No automated background backups will run until re-enabled.")
			return nil
		},
	}

	scheduleStatusCmd = &cobra.Command{
		Use:   "status",
		Short: "Check background timer state and next scheduled execution",
		RunE: func(cmd *cobra.Command, args []string) error {
			return printScheduleStatus()
		},
	}

	scheduleRunCmd = &cobra.Command{
		Use:   "run",
		Short: "Trigger a backup run immediately in the background via systemd",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := sysinfo.TriggerSystemdRun(); err != nil {
				return err
			}
			fmt.Println(lipgloss.NewStyle().Foreground(tui.ColorSuccess).Bold(true).Render("✔ Castor service triggered."))
			fmt.Println("  Inspect live logs with: journalctl --user -u castor.service -f")
			return nil
		},
	}
)

func init() {
	scheduleCmd.AddCommand(scheduleEnableCmd)
	scheduleCmd.AddCommand(scheduleDisableCmd)
	scheduleCmd.AddCommand(scheduleStatusCmd)
	scheduleCmd.AddCommand(scheduleRunCmd)
}

func runSchedule(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return printScheduleStatus()
	}

	switch args[0] {
	case "enable", "on", "start":
		return scheduleEnableCmd.RunE(cmd, args)
	case "disable", "off", "stop":
		return scheduleDisableCmd.RunE(cmd, args)
	case "status":
		return scheduleStatusCmd.RunE(cmd, args)
	case "run":
		return scheduleRunCmd.RunE(cmd, args)
	default:
		return fmt.Errorf("unknown schedule command '%s' (use enable, disable, status, or run)", args[0])
	}
}

func printScheduleStatus() error {
	st := sysinfo.CheckSystemdTimer()
	if !st.Available {
		fmt.Println("Systemd user daemon is not available on this system.")
		fmt.Println("You can run scheduled backups using cron:")
		fmt.Println("  0 3 * * * castor push --no-tui >> ~/.config/castor/backup.log 2>&1")
		return nil
	}

	title := lipgloss.NewStyle().Bold(true).Foreground(tui.ColorPrimary).Render("🦫 Castor Background Timer Status")
	fmt.Println(title)

	if st.Active {
		activeBadge := lipgloss.NewStyle().Bold(true).Foreground(tui.ColorSuccess).Render("ACTIVE (ON)")
		fmt.Printf("State        : %s\n", activeBadge)
		if st.NextRun != "" {
			fmt.Printf("Next Run     : %s\n", st.NextRun)
		}
		fmt.Println("Schedule     : Daily at 03:00 AM (randomized delay 30m, AC power only)")
		fmt.Printf("Service Unit : %s\n", st.ServiceFile)
		fmt.Printf("Timer Unit   : %s\n", st.TimerFile)
		fmt.Println("\nTo turn off : castor schedule disable")
		fmt.Println("To test run : castor schedule run")
	} else {
		inactiveBadge := lipgloss.NewStyle().Bold(true).Foreground(tui.ColorWarning).Render("INACTIVE (OFF)")
		fmt.Printf("State        : %s\n", inactiveBadge)
		fmt.Println("No automated background backups are currently scheduled.")
		fmt.Println("\nTo turn on  : castor schedule enable")
	}

	return nil
}
