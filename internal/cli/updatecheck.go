package cli

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/simonm/dotgenie/internal/config"
)

const defaultUpdateCheckDays = 14

// maybeCheckForUpdate checks if a newer version of dotgenie is available,
// but only if enough days have passed since the last check. Prints a
// one-liner if an update is available. Never errors -- failures are silent.
func maybeCheckForUpdate(paths config.Paths, cfg *config.Config) {
	checkDays := cfg.UpdateCheckDays
	if checkDays < 0 {
		return // disabled
	}
	if checkDays == 0 {
		checkDays = defaultUpdateCheckDays
	}

	// Check if enough time has passed
	if cfg.LastUpdateCheck != "" {
		lastCheck, err := time.Parse("2006-01-02", cfg.LastUpdateCheck)
		if err == nil && time.Since(lastCheck).Hours() < float64(checkDays*24) {
			return // too soon
		}
	}

	// Update the last check timestamp regardless of outcome
	cfg.LastUpdateCheck = time.Now().Format("2006-01-02")
	localPath := filepath.Join(paths.DotfilesDir, "config.local.yml")
	_ = cfg.SaveLocal(localPath)

	// Fetch latest release (reuses getLatestRelease from upgrade.go)
	latest, err := getLatestRelease()
	if err != nil {
		return // silent fail
	}

	latestVersion := strings.TrimPrefix(latest.TagName, "v")
	currentVersion := strings.TrimPrefix(version, "v")

	if isNewerVersion(latestVersion, currentVersion) {
		fmt.Printf("\nA new version of dotgenie is available (%s). Run 'dotgenie upgrade' to update.\n", latest.TagName)
	}
}
