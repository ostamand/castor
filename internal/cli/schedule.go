package cli

import (
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/ostamand/castor/internal/config"
	"github.com/ostamand/castor/internal/sysinfo"
	"github.com/ostamand/castor/internal/tui"
	"github.com/spf13/cobra"
)

var scheduleCmd = &cobra.Command{
	Use:     "schedule [enable|disable|status|run|logs|history]",
	Aliases: []string{"timer"},
	Short:   "Manage background scheduled backups (systemd user timer)",
	Long: `Turns the background backup schedule on or off using Linux systemd user timers.
When enabled, Castor wakes up on schedule to evaluate target drift and stream archives to cold storage.`,
	RunE: runSchedule,
}

var (
	scheduleEnableCmd = &cobra.Command{
		Use:   "enable",
		Aliases: []string{"on", "start"},
		Short: "Enable and activate the scheduled background timer",
		RunE: func(cmd *cobra.Command, args []string) error {
			exe, _ := os.Executable()
			if err := sysinfo.EnableSystemdTimer(exe); err != nil {
				return err
			}
			fmt.Println(lipgloss.NewStyle().Foreground(tui.ColorSuccess).Bold(true).Render("✔ Castor background timer ENABLED."))
			fmt.Println("  Runs on schedule (default: daily at 03:00 AM, AC power only, idle I/O priority).")
			return printScheduleStatus()
		},
	}

	scheduleDisableCmd = &cobra.Command{
		Use:   "disable",
		Aliases: []string{"off", "stop"},
		Short: "Disable and deactivate the scheduled background timer",
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

	scheduleLogsLines  int
	scheduleLogsFollow bool

	scheduleLogsCmd = &cobra.Command{
		Use:   "logs",
		Short: "Show output from recent scheduled backup runs",
		Example: `  castor schedule logs
  castor schedule logs -n 100
  castor schedule logs -f`,
		RunE: runScheduleLogs,
	}

	scheduleHistoryLines int

	scheduleHistoryCmd = &cobra.Command{
		Use:   "history",
		Short: "Show a summary of recent backup runs",
		Example: `  castor schedule history
  castor schedule history -n 20`,
		RunE: runScheduleHistory,
	}
)

func init() {
	scheduleLogsCmd.Flags().IntVarP(&scheduleLogsLines, "lines", "n", 30, "Number of log lines to show")
	scheduleLogsCmd.Flags().BoolVarP(&scheduleLogsFollow, "follow", "f", false, "Follow live log output")
	scheduleHistoryCmd.Flags().IntVarP(&scheduleHistoryLines, "lines", "n", 10, "Number of recent runs to show")

	scheduleCmd.AddCommand(scheduleEnableCmd)
	scheduleCmd.AddCommand(scheduleDisableCmd)
	scheduleCmd.AddCommand(scheduleStatusCmd)
	scheduleCmd.AddCommand(scheduleRunCmd)
	scheduleCmd.AddCommand(scheduleLogsCmd)
	scheduleCmd.AddCommand(scheduleHistoryCmd)
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
	case "logs":
		return scheduleLogsCmd.RunE(cmd, args)
	case "history":
		return scheduleHistoryCmd.RunE(cmd, args)
	default:
		return fmt.Errorf("unknown schedule command '%s' (use enable, disable, status, run, logs, or history)", args[0])
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

	// Load last run from history if available
	configPath := cfgPath
	if configPath == "" {
		configPath = config.DefaultConfigPath()
	}
	historyPath := config.HistoryPathForConfig(configPath)
	hist, _ := config.LoadHistory(historyPath)
	lastRunLine := ""
	if hist != nil && len(hist.Runs) > 0 {
		last := hist.Runs[len(hist.Runs)-1]
		var badge string
		switch last.Status {
		case "success":
			badge = lipgloss.NewStyle().Bold(true).Foreground(tui.ColorSuccess).Render("✔ SUCCESS")
		case "partial":
			badge = lipgloss.NewStyle().Bold(true).Foreground(tui.ColorWarning).Render("⚠ PARTIAL")
		case "failed":
			badge = lipgloss.NewStyle().Bold(true).Foreground(tui.ColorDanger).Render("✖ FAILED")
		default:
			badge = last.Status
		}
		lastRunLine = fmt.Sprintf("Last Run     : %s (%s, duration: %s)\n", badge, last.StartedAt.Format("2006-01-02 15:04 UTC"), last.FinishedAt.Sub(last.StartedAt).Round(time.Second))
	}

	if st.Active {
		activeBadge := lipgloss.NewStyle().Bold(true).Foreground(tui.ColorSuccess).Render("ACTIVE (ON)")
		fmt.Printf("State        : %s\n", activeBadge)
		if lastRunLine != "" {
			fmt.Print(lastRunLine)
		}
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

func runScheduleLogs(cmd *cobra.Command, args []string) error {
	journalArgs := []string{"--user", "-u", "castor.service", "--no-pager", "-n", fmt.Sprintf("%d", scheduleLogsLines)}
	if scheduleLogsFollow {
		journalArgs = append(journalArgs, "-f")
	}

	execCmd := exec.Command("journalctl", journalArgs...)
	execCmd.Stdout = os.Stdout
	execCmd.Stderr = os.Stderr

	if err := execCmd.Run(); err != nil {
		fmt.Println("Could not run journalctl. Check ~/.config/castor/history.json or use 'castor schedule history'.")
	}
	return nil
}

func runScheduleHistory(cmd *cobra.Command, args []string) error {
	configPath := cfgPath
	if configPath == "" {
		configPath = config.DefaultConfigPath()
	}
	historyPath := config.HistoryPathForConfig(configPath)

	history, err := config.LoadHistory(historyPath)
	if err != nil {
		return err
	}

	if len(history.Runs) == 0 {
		fmt.Println("No run history yet. History is recorded after each 'castor push'.")
		return nil
	}

	fmt.Println(lipgloss.NewStyle().Bold(true).Foreground(tui.ColorPrimary).Render("🦫 Castor · Run History"))
	fmt.Println()

	headers := []string{"Run (UTC)", "Duration", "Synced", "Skipped", "Failed", "Status"}

	startIdx := len(history.Runs) - scheduleHistoryLines
	if startIdx < 0 {
		startIdx = 0
	}

	var rows [][]string
	for i := len(history.Runs) - 1; i >= startIdx; i-- {
		run := history.Runs[i]

		duration := run.FinishedAt.Sub(run.StartedAt).Round(time.Second)

		var status string
		switch run.Status {
		case "success":
			status = lipgloss.NewStyle().Bold(true).Foreground(tui.ColorSuccess).Render("✔ SUCCESS")
		case "partial":
			status = lipgloss.NewStyle().Bold(true).Foreground(tui.ColorWarning).Render("⚠ PARTIAL")
		case "failed":
			status = lipgloss.NewStyle().Bold(true).Foreground(tui.ColorDanger).Render("✖ FAILED")
		default:
			status = run.Status
		}

		rows = append(rows, []string{
			run.StartedAt.Format("2006-01-02 15:04"),
			duration.String(),
			fmt.Sprintf("%d", run.TargetsSynced),
			fmt.Sprintf("%d", run.TargetsSkipped),
			fmt.Sprintf("%d", run.TargetsFailed),
			status,
		})
	}

	fmt.Println(tui.RenderTable(headers, rows))
	return nil
}
