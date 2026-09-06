package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	cfgPath string
	verbose bool
	jsonOut bool
	noTUI   bool
)

const version = "0.4.0"

// RootCmd is the primary CLI command for Castor
var RootCmd = &cobra.Command{
	Use:   "castor",
	Short: "🦫 Castor — Fast, encrypted cloud archiving for developers",
	Long: `🦫 Castor (Castor canadensis)
Nature's engineer and lodge builder.

Castor protects your code and project workspaces by compressing, encrypting,
and streaming them directly into cloud storage — keeping your work safe without slowing you down.`,
	Version: version,
}

func init() {
	RootCmd.PersistentFlags().StringVarP(&cfgPath, "config", "c", "", "Path to config file (default: ~/.config/castor/config.toml)")
	RootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose debug output")
	RootCmd.PersistentFlags().BoolVar(&jsonOut, "json", false, "Output results in structured JSON format")
	RootCmd.PersistentFlags().BoolVar(&noTUI, "no-tui", false, "Disable interactive TUI formatting and spinners")

	RootCmd.AddCommand(initCmd)
	RootCmd.AddCommand(addCmd)
	RootCmd.AddCommand(pushCmd)
	RootCmd.AddCommand(pullCmd)
	RootCmd.AddCommand(lsCmd)
	RootCmd.AddCommand(statusCmd)
	RootCmd.AddCommand(diffCmd)
	RootCmd.AddCommand(inspectCmd)
	RootCmd.AddCommand(catCmd)
	RootCmd.AddCommand(verifyCmd)
	RootCmd.AddCommand(pruneCmd)
	RootCmd.AddCommand(authCmd)
	RootCmd.AddCommand(scheduleCmd)
	RootCmd.AddCommand(doctorCmd)
	RootCmd.AddCommand(upgradeCmd)
	RootCmd.AddCommand(uninstallCmd)
}

// Execute runs the root command
func Execute() {
	if err := RootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
