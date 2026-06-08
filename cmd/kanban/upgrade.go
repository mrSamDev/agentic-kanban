package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const repoOwner = "mrSamDev"
const repoName = "agentic-kanban"

type ghRelease struct {
	TagName string `json:"tag_name"`
}

func upgradeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Check for and install kanban upgrades",
		Long: `Check for and install the latest kanban release from GitHub.

  kanban upgrade          — check and install latest version
  kanban upgrade --check  — only check, don't install`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			checkOnly, _ := cmd.Flags().GetBool("check")

			release, err := latestRelease()
			if err != nil {
				return fmt.Errorf("check upgrade: %w", err)
			}

			newer := isNewerVersion(release.TagName)
			if !newer {
				fmt.Printf("kanban %s — up to date\n", version)
				return nil
			}

			fmt.Printf("kanban %s available (you have %s)\n", release.TagName, version)

			if checkOnly {
				fmt.Println("Run 'kanban upgrade' to install.")
				return nil
			}

			return installUpgrade(release)
		},
	}
	cmd.Flags().Bool("check", false, "only check for updates, don't install")
	return cmd
}

func latestRelease() (*ghRelease, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", repoOwner, repoName)

	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "kanban-upgrade")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var release ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("decode release: %w", err)
	}
	return &release, nil
}

func installUpgrade(release *ghRelease) error {
	osName := runtime.GOOS
	arch := runtime.GOARCH
	switch arch {
	case "x86_64", "amd64":
		arch = "amd64"
	case "aarch64", "arm64":
		arch = "arm64"
	default:
		return fmt.Errorf("unsupported arch: %s", arch)
	}

	url := fmt.Sprintf("https://github.com/%s/%s/releases/download/%s/kanban_%s_%s",
		repoOwner, repoName, release.TagName, osName, arch)

	fmt.Printf("Downloading %s (this may take a few seconds)...\n", url)

	tmpFile := os.Args[0] + ".new"
	f, err := os.OpenFile(tmpFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		f.Close()
		os.Remove(tmpFile)
		return fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		f.Close()
		os.Remove(tmpFile)
		return fmt.Errorf("download returned %d (expected 200)", resp.StatusCode)
	}

	if _, err := f.ReadFrom(resp.Body); err != nil {
		f.Close()
		os.Remove(tmpFile)
		return fmt.Errorf("write binary: %w", err)
	}
	f.Close()

	if err := os.Rename(tmpFile, os.Args[0]); err != nil {
		return fmt.Errorf("replace binary (try with sudo): %w", err)
	}

	fmt.Println("Upgraded successfully! Restart to use the new version.")
	return nil
}

func isNewerVersion(tag string) bool {
	current := strings.TrimPrefix(version, "v")
	latest := strings.TrimPrefix(tag, "v")
	return current != latest
}

// autoVersionCheck pings GitHub once per 24h so users learn about new releases.
// Why advisory-only: a transient network failure must not block CLI startup.
func autoVersionCheck() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	cacheDir := filepath.Join(home, ".kanban")
	cacheFile := filepath.Join(cacheDir, ".last-version-check")

	data, err := os.ReadFile(cacheFile)
	if err == nil {
		last, parseErr := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
		if parseErr == nil && time.Since(time.Unix(last, 0)) < 24*time.Hour {
			return
		}
	}

	// Fire goroutine so --help and other fast commands are never blocked.
	// sync.Once prevents duplicate spawns, and the goroutine owns its lifecycle
	// (no shared state with the caller).
	go func() {
		release, err := latestRelease()
		if err != nil {
			return
		}
		if isNewerVersion(release.TagName) {
			fmt.Fprintf(os.Stderr, "kanban %s available (you have %s) — run 'kanban upgrade'\n", release.TagName, version)
		}
		// Write cache timestamp even on failure so transient errors don't retry every command.
		os.MkdirAll(cacheDir, 0755)
		os.WriteFile(cacheFile, []byte(fmt.Sprintf("%d", time.Now().Unix())), 0644)
	}()
}

func versionCheck() {
	release, err := latestRelease()
	if err != nil {
		fmt.Fprintf(os.Stderr, "version check failed: %v\n", err)
		return
	}
	if isNewerVersion(release.TagName) {
		fmt.Fprintf(os.Stderr, "kanban %s available (you have %s) — run 'kanban upgrade'\n", release.TagName, version)
	}
}