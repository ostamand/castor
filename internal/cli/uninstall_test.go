package cli

import (
	"testing"
)

func TestUninstallCommandFlags(t *testing.T) {
	if uninstallCmd == nil {
		t.Fatal("expected uninstallCmd to be initialized")
	}

	yesFlag := uninstallCmd.Flags().Lookup("yes")
	if yesFlag == nil || yesFlag.Shorthand != "y" {
		t.Error("expected --yes / -y flag on uninstallCmd")
	}

	purgeFlag := uninstallCmd.Flags().Lookup("purge")
	if purgeFlag == nil {
		t.Error("expected --purge flag on uninstallCmd")
	}
}
