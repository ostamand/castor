package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/ostamand/castor/internal/tui"
	"github.com/spf13/cobra"
)

var upgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: "Self-update the Castor binary in-place from GitHub Releases",
	Long: `Queries GitHub Releases for the latest version, downloads the precompiled binary for your
operating system and architecture, atomically swaps the binary in-place, and refreshes LLM agent skills.`,
	RunE: runUpgrade,
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
		fmt.Printf("Up to date or no GitHub release assets found (HTTP %d).\n", resp.StatusCode)
		refreshAgentSkills()
		return nil
	}

	var rel gitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return fmt.Errorf("failed to parse release metadata: %w", err)
	}

	cleanTag := strings.TrimPrefix(rel.TagName, "v")
	if cleanTag == version {
		fmt.Println(lipgloss.NewStyle().Foreground(tui.ColorSuccess).Bold(true).Render(
			fmt.Sprintf("✔ Castor is already up to date (v%s).", version),
		))
		refreshAgentSkills()
		return nil
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
		fmt.Printf("Release %s exists, but no precompiled binary found for %s/%s.\n", rel.TagName, runtime.GOOS, runtime.GOARCH)
		refreshAgentSkills()
		return nil
	}

	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to locate current executable: %w", err)
	}
	execPath, _ = filepath.EvalSymlinks(execPath)

	fmt.Printf("Downloading %s from %s...\n", rel.TagName, downloadURL)
	binResp, err := http.Get(downloadURL)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer binResp.Body.Close()

	tmpFile := filepath.Join(os.TempDir(), fmt.Sprintf("castor-update-%d", os.Getpid()))
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
	defer os.Remove(tmpFile)

	// Replace current executable
	if err := os.Rename(tmpFile, execPath); err != nil {
		// Attempt elevated replacement if permission denied (e.g. /usr/local/bin)
		if _, err := exec.LookPath("sudo"); err == nil {
			cmd := exec.Command("sudo", "install", "-m", "755", tmpFile, execPath)
			cmd.Stdin = os.Stdin
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("atomic in-place binary swap failed: %w (please run: sudo install -m 755 %s %s)", err, tmpFile, execPath)
			}
		} else {
			return fmt.Errorf("permission denied writing to %s: %w", execPath, err)
		}
	}

	refreshAgentSkills()

	fmt.Println(lipgloss.NewStyle().Foreground(tui.ColorSuccess).Bold(true).Render(
		fmt.Sprintf("✔ Successfully upgraded Castor to %s in-place!", rel.TagName),
	))
	return nil
}

func refreshAgentSkills() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	skillsDir := filepath.Join(home, ".gemini", "config", "skills")
	_ = os.MkdirAll(filepath.Join(skillsDir, "castor-cli"), 0755)
	_ = os.MkdirAll(filepath.Join(skillsDir, "castor-customizer"), 0755)

	rawURL := "https://raw.githubusercontent.com/ostamand/castor/main"
	downloadSkillFile(rawURL+"/skills/castor-cli/SKILL.md", filepath.Join(skillsDir, "castor-cli", "SKILL.md"))
	downloadSkillFile(rawURL+"/skills/castor-customizer/SKILL.md", filepath.Join(skillsDir, "castor-customizer", "SKILL.md"))
}

func downloadSkillFile(url, destPath string) {
	resp, err := http.Get(url)
	if err != nil || resp.StatusCode != http.StatusOK {
		return
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err == nil && len(data) > 0 {
		_ = os.WriteFile(destPath, data, 0644)
	}
}
