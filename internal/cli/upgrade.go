package cli

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
)

var upgradeCheck bool

var upgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: "Upgrade dotgenie to the latest version",
	Long: `Check for and install the latest version of dotgenie.

Use --check to only check for updates without installing.`,
	RunE: runUpgrade,
}

func init() {
	upgradeCmd.Flags().BoolVarP(&upgradeCheck, "check", "c", false, "Only check for updates, don't install")
	rootCmd.AddCommand(upgradeCmd)
}

type githubRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

func runUpgrade(cmd *cobra.Command, args []string) error {
	fmt.Println("Checking for updates...")

	// Get latest release from GitHub
	latest, err := getLatestRelease()
	if err != nil {
		return fmt.Errorf("failed to check for updates: %w", err)
	}

	latestVersion := strings.TrimPrefix(latest.TagName, "v")
	currentVersion := strings.TrimPrefix(version, "v")

	if latestVersion == currentVersion {
		fmt.Printf("Already running the latest version (%s)\n", version)
		return nil
	}

	// Compare versions
	if !isNewerVersion(latestVersion, currentVersion) {
		fmt.Printf("Already running the latest version (%s)\n", version)
		return nil
	}

	fmt.Printf("New version available: %s (current: %s)\n", latest.TagName, version)

	if upgradeCheck {
		return nil
	}

	// Find the right asset for this platform
	osName := runtime.GOOS
	archName := runtime.GOARCH

	assetName := fmt.Sprintf("dotgenie_%s_%s_%s.tar.gz", latestVersion, osName, archName)
	var downloadURL string
	for _, asset := range latest.Assets {
		if asset.Name == assetName {
			downloadURL = asset.BrowserDownloadURL
			break
		}
	}

	if downloadURL == "" {
		return fmt.Errorf("no release found for %s/%s", osName, archName)
	}

	// Get current executable path
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	// Resolve symlinks to get the actual binary location
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		return fmt.Errorf("failed to resolve executable path: %w", err)
	}

	fmt.Printf("Downloading %s...\n", assetName)

	// Download to temp file
	tmpDir, err := os.MkdirTemp("", "dotgenie-upgrade")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	resp, err := http.Get(downloadURL) //nolint:gosec // URL is constructed from known GitHub API response
	if err != nil {
		return fmt.Errorf("failed to download: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}

	// Extract tarball
	gzr, err := gzip.NewReader(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to decompress: %w", err)
	}
	defer func() { _ = gzr.Close() }()

	tr := tar.NewReader(gzr)
	var newBinaryPath string

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read tarball: %w", err)
		}

		if header.Name == "dotgenie" {
			newBinaryPath = filepath.Join(tmpDir, "dotgenie")
			f, err := os.OpenFile(newBinaryPath, os.O_CREATE|os.O_WRONLY, 0755)
			if err != nil {
				return fmt.Errorf("failed to create binary: %w", err)
			}
			if _, err := io.Copy(f, tr); err != nil {
				_ = f.Close()
				return fmt.Errorf("failed to write binary: %w", err)
			}
			_ = f.Close()
			break
		}
	}

	if newBinaryPath == "" {
		return fmt.Errorf("binary not found in archive")
	}

	// Replace the current binary
	fmt.Printf("Installing to %s...\n", execPath)

	// Check if we can write to the directory (not the file itself, which is busy)
	execDir := filepath.Dir(execPath)
	if err := checkDirWritable(execDir); err != nil {
		return fmt.Errorf("cannot write to %s: %w\nTry running with sudo", execDir, err)
	}

	// Rename the running binary first (this works even while running)
	// Then write the new binary to the original path
	backupPath := execPath + ".old"

	// Remove any existing backup first
	os.Remove(backupPath)

	// Rename current binary to backup (allowed even while running)
	if err := os.Rename(execPath, backupPath); err != nil {
		return fmt.Errorf("failed to rename old binary: %w", err)
	}

	// Copy new binary to original path (not rename, since it might be cross-device)
	if err := copyFileForUpgrade(newBinaryPath, execPath); err != nil {
		// Try to restore backup
		os.Rename(backupPath, execPath)
		return fmt.Errorf("failed to install new binary: %w", err)
	}

	// Remove backup (might fail on Windows, that's ok)
	os.Remove(backupPath)

	fmt.Printf("Successfully upgraded to %s\n", latest.TagName)
	return nil
}

func getLatestRelease() (*githubRelease, error) {
	resp, err := http.Get("https://api.github.com/repos/simonm/dotgenie-cli/releases/latest")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, err
	}

	return &release, nil
}

// isNewerVersion returns true if v1 is newer than v2
// Simple semver comparison (major.minor.patch)
func isNewerVersion(v1, v2 string) bool {
	v1Parts := strings.Split(v1, ".")
	v2Parts := strings.Split(v2, ".")

	for i := 0; i < 3; i++ {
		var n1, n2 int
		if i < len(v1Parts) {
			fmt.Sscanf(v1Parts[i], "%d", &n1)
		}
		if i < len(v2Parts) {
			fmt.Sscanf(v2Parts[i], "%d", &n2)
		}
		if n1 > n2 {
			return true
		}
		if n1 < n2 {
			return false
		}
	}
	return false
}

func checkDirWritable(dir string) error {
	// Try to create a temp file in the directory to check write access
	testFile := filepath.Join(dir, ".dotgenie-upgrade-test")
	f, err := os.Create(testFile)
	if err != nil {
		return err
	}
	f.Close()
	os.Remove(testFile)
	return nil
}

func copyFileForUpgrade(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}

	return out.Chmod(0755)
}
