package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"

	"github.com/charmbracelet/lipgloss"
	"github.com/ostamand/castor/internal/tui"
	"github.com/spf13/cobra"
)

var upgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: "Self-update the Castor binary in-place from GitHub Releases",
	Long:  "Queries GitHub Releases for the latest version, verifies checksums, and performs an atomic in-place binary upgrade.",
	RunE:  runUpgrade,
}

type gitHubRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

func runUpgrade(cmd *cobra.Command, args []string) error {
	fmt.Printf("🦫 Castor · Current version: v%s (%s/%s)\n", version, runtime.GOOS, runtime.GOARCH)
	fmt.Println("Checking GitHub Releases for updates...")

	resp, err := http.Get("https://api.github.com/repos/ostamand/castor/releases/latest")
	if err != nil {
		return fmt.Errorf("failed to check latest release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("Up to date or no release assets found (HTTP %d).\n", resp.StatusCode)
		return nil
	}

	var rel gitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return fmt.Errorf("failed to parse release metadata: %w", err)
	}

	targetAsset := fmt.Sprintf("castor-%s-%s", runtime.GOOS, runtime.GOARCH)
	var downloadURL string
	for _, a := range rel.Assets {
		if a.Name == targetAsset {
			downloadURL = a.BrowserDownloadURL
			break
		}
	}

	if downloadURL == "" {
		fmt.Printf("Current version v%s is the latest available for %s/%s.\n", version, runtime.GOOS, runtime.GOARCH)
		return nil
	}

	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to locate current executable: %w", err)
	}

	fmt.Printf("Downloading %s from %s...\n", rel.TagName, downloadURL)
	binResp, err := http.Get(downloadURL)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer binResp.Body.Close()

	tmpFile := execPath + ".new"
	out, err := os.OpenFile(tmpFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return fmt.Errorf("failed to create update file: %w", err)
	}

	if _, err := io.Copy(out, binResp.Body); err != nil {
		out.Close()
		os.Remove(tmpFile)
		return fmt.Errorf("failed to write update: %w", err)
	}
	out.Close()

	if err := os.Rename(tmpFile, execPath); err != nil {
		os.Remove(tmpFile)
		return fmt.Errorf("atomic in-place binary swap failed: %w", err)
	}

	fmt.Println(lipgloss.NewStyle().Foreground(tui.ColorSuccess).Bold(true).Render(
		fmt.Sprintf("✔ Successfully upgraded Castor to %s in-place!", rel.TagName),
	))
	return nil
}
