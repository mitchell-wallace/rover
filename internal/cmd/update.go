package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const githubAPI = "https://api.github.com/repos/mitchell-wallace/rover/releases/latest"

var (
	updateYes              bool
	fetchLatestVersionFunc = fetchLatestVersion
	installLatestVersionFn = installLatestVersion
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Check for a newer version and optionally update",
	Long: `Check the GitHub releases page for a newer version of rover.

Prints the current and latest versions. If a newer version is available,
prompts for confirmation before running the install script unless --yes is set.`,
	RunE: func(_ *cobra.Command, _ []string) error {
		if version == "" || version == "dev" {
			fmt.Println("Current version: dev (cannot check for updates)")
			return nil
		}

		latest, err := fetchLatestVersionFunc()
		if err != nil {
			return fmt.Errorf("update: %w", err)
		}

		cmp, err := compareVersions(version, latest)
		if err != nil {
			return fmt.Errorf("update: %w", err)
		}

		if cmp >= 0 {
			fmt.Printf("Current version: %s\nLatest version:  %s\n", version, latest)
			fmt.Println("You are up to date.")
			return nil
		}

		fmt.Printf("Current version: %s\nLatest version:  %s\n", version, latest)

		if !updateYes {
			fmt.Print("Update to latest version? [Y/n] ")
			reader := bufio.NewReader(os.Stdin)
			response, err := reader.ReadString('\n')
			if err != nil {
				return fmt.Errorf("update: read confirmation: %w", err)
			}
			response = strings.TrimSpace(strings.ToLower(response))
			if response != "" && response != "y" && response != "yes" {
				fmt.Println("Update cancelled.")
				return nil
			}
		}

		if err := installLatestVersionFn(); err != nil {
			return fmt.Errorf("update: install failed: %w", err)
		}
		return nil
	},
}

func init() {
	updateCmd.Flags().BoolVarP(&updateYes, "yes", "y", false, "install without prompting when an update is available")
	rootCmd.AddCommand(updateCmd)
}

func fetchLatestVersion() (string, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", githubAPI, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github API returned %s", resp.Status)
	}

	var payload struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("parse release: %w", err)
	}

	return strings.TrimPrefix(payload.TagName, "v"), nil
}

func installLatestVersion() error {
	var installCmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		installCmd = exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", "irm https://raw.githubusercontent.com/mitchell-wallace/rover/main/install.ps1 | iex")
	default:
		installCmd = exec.Command("sh", "-c", "curl -fsSL https://raw.githubusercontent.com/mitchell-wallace/rover/main/install.sh | bash")
	}
	installCmd.Stdout = os.Stdout
	installCmd.Stderr = os.Stderr
	return installCmd.Run()
}

func compareVersions(a, b string) (int, error) {
	a = strings.TrimPrefix(a, "v")
	b = strings.TrimPrefix(b, "v")
	aParts := strings.Split(a, ".")
	bParts := strings.Split(b, ".")
	maxLen := len(aParts)
	if len(bParts) > maxLen {
		maxLen = len(bParts)
	}
	for i := 0; i < maxLen; i++ {
		var av, bv int
		if i < len(aParts) {
			v, err := strconv.Atoi(aParts[i])
			if err != nil {
				return 0, fmt.Errorf("invalid version %q", a)
			}
			av = v
		}
		if i < len(bParts) {
			v, err := strconv.Atoi(bParts[i])
			if err != nil {
				return 0, fmt.Errorf("invalid version %q", b)
			}
			bv = v
		}
		if av < bv {
			return -1, nil
		}
		if av > bv {
			return 1, nil
		}
	}
	return 0, nil
}
