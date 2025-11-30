package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/simonm/dotgenie/internal/config"
	"github.com/simonm/dotgenie/internal/dotfiles"
	"github.com/spf13/cobra"
)

var (
	forgetScope    string
	forgetKeepRepo bool
	forgetYes      bool
)

var forgetCmd = &cobra.Command{
	Use:   "forget <path>...",
	Short: "Remove dotfiles from management",
	Long: `Remove configuration files from dotgenie management.

By default, the file is copied back to its original location (replacing the
symlink) and removed from the dotfiles repository.

You must specify which layer to forget from:
  - common:      Shared across all systems
  - workstation: Desktop/laptop systems only
  - host:        This specific host only

If a file exists in multiple layers (e.g., common and host), forgetting from
one layer allows the other layer to take effect on next apply.

Examples:
  dotgenie forget --scope host ~/.config/monitors.xml    # Forget host override
  dotgenie forget --scope common ~/.bashrc               # Forget from common
  dotgenie forget --scope host /etc/modprobe.d/iwlwifi.conf  # Forget system file
  dotgenie forget --keep-repo ~/.config/nvim             # Unlink but keep in repo`,
	Args: cobra.MinimumNArgs(1),
	RunE: runForget,
}

func init() {
	forgetCmd.Flags().StringVarP(&forgetScope, "scope", "s", "", "Layer to forget from (common, workstation, host) - required")
	forgetCmd.Flags().BoolVar(&forgetKeepRepo, "keep-repo", false, "Keep file in repo, only remove symlink")
	forgetCmd.Flags().BoolVarP(&forgetYes, "yes", "y", false, "Skip confirmation prompts")
	_ = forgetCmd.MarkFlagRequired("scope")
}

func runForget(cmd *cobra.Command, args []string) error {
	paths := config.DefaultPaths()
	if dotfilesDir != "" {
		paths.DotfilesDir = dotfilesDir
	}

	// Load config
	cfg, err := config.Load(filepath.Join(paths.DotfilesDir, "config.yml"))
	if err != nil {
		return fmt.Errorf("loading config: %w\nRun 'dotgenie init' first", err)
	}

	// Determine layer directory
	layer := dotfiles.GetLayerForPath(forgetScope, cfg.Hostname)

	fmt.Printf("Forgetting from layer: %s\n", layer)

	// Process each path
	for _, targetPath := range args {
		// Expand ~ to home directory
		if strings.HasPrefix(targetPath, "~/") {
			targetPath = filepath.Join(paths.Home, targetPath[2:])
		}

		if err := forgetPath(targetPath, layer, paths, cfg); err != nil {
			fmt.Printf("Error forgetting %s: %v\n", targetPath, err)
		}
	}

	// Auto-commit if enabled and we removed files
	if cfg.AutoCommitAfterAdopt && !forgetKeepRepo {
		if err := gitCommitForgotten(paths.DotfilesDir); err != nil {
			fmt.Printf("Warning: auto-commit failed: %v\n", err)
		}
	}

	return nil
}

func forgetPath(targetPath, layer string, paths config.Paths, cfg *config.Config) error {
	// Get absolute path
	absTarget, err := filepath.Abs(targetPath)
	if err != nil {
		return err
	}

	// Detect target (home, etc, var, usr) based on path
	targetName, relPath := dotfiles.GetTargetForPath(absTarget, paths.Home)

	// Find the source in the dotfiles repo
	sourcePath := filepath.Join(paths.DotfilesDir, "dotfiles", layer, targetName, relPath)

	// Check if it exists in the repo
	sourceInfo, err := os.Stat(sourcePath)
	if os.IsNotExist(err) {
		return fmt.Errorf("not found in %s/%s: %s", layer, targetName, relPath)
	}
	if err != nil {
		return err
	}

	// Show what will happen
	if sourceInfo.IsDir() {
		fmt.Printf("\nForgetting directory: %s\n", absTarget)
	} else {
		fmt.Printf("\nForgetting file: %s\n", absTarget)
	}
	fmt.Printf("  Source: %s\n", sourcePath)
	fmt.Printf("  Target: [%s] %s\n", targetName, relPath)

	if forgetKeepRepo {
		fmt.Println("  (keeping in repo, only removing symlink)")
	}

	// Check if there's a fallback in another layer
	fallback := findFallbackLayer(relPath, targetName, layer, paths, cfg)
	if fallback != "" {
		fmt.Printf("  Note: %s/%s will take effect after next apply\n", fallback, targetName)
	}

	// Confirm unless -y flag
	if !forgetYes {
		fmt.Print("Proceed? [y/N] ")
		reader := bufio.NewReader(os.Stdin)
		response, _ := reader.ReadString('\n')
		response = strings.TrimSpace(strings.ToLower(response))
		if response != "y" && response != "yes" {
			fmt.Println("Skipped")
			return nil
		}
	}

	// Check current state of target
	targetInfo, err := os.Lstat(absTarget)

	if err == nil && targetInfo.Mode()&os.ModeSymlink != 0 {
		// It's a symlink - check if it points to our source
		linkTarget, _ := os.Readlink(absTarget)
		if linkTarget == sourcePath {
			// Copy content back and remove symlink
			if err := os.Remove(absTarget); err != nil {
				return fmt.Errorf("removing symlink: %w", err)
			}

			if sourceInfo.IsDir() {
				if err := copyDir(sourcePath, absTarget); err != nil {
					return fmt.Errorf("copying directory back: %w", err)
				}
			} else {
				if err := copyFile(sourcePath, absTarget); err != nil {
					return fmt.Errorf("copying file back: %w", err)
				}
			}
			fmt.Printf("Restored: %s\n", absTarget)
		}
	}

	// Remove from repo unless --keep-repo
	if !forgetKeepRepo {
		if err := os.RemoveAll(sourcePath); err != nil {
			return fmt.Errorf("removing from repo: %w", err)
		}

		// Clean up empty parent directories
		cleanEmptyDirs(filepath.Dir(sourcePath), filepath.Join(paths.DotfilesDir, "dotfiles", layer))

		fmt.Printf("Removed from repo: %s\n", sourcePath)
	}

	return nil
}

func findFallbackLayer(relPath, targetName, currentLayer string, paths config.Paths, cfg *config.Config) string {
	// Check layers in reverse priority order
	layers := []struct {
		name string
		path string
	}{
		{"common", "common"},
		{"workstation", "workstation"},
		{"host", filepath.Join("hosts", cfg.Hostname)},
	}

	for _, l := range layers {
		if l.path == currentLayer {
			continue
		}

		checkPath := filepath.Join(paths.DotfilesDir, "dotfiles", l.path, targetName, relPath)
		if _, err := os.Stat(checkPath); err == nil {
			return l.name
		}
	}

	return ""
}

func cleanEmptyDirs(dir, stopAt string) {
	for dir != stopAt && dir != "." && dir != "/" {
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) > 0 {
			break
		}
		if err := os.Remove(dir); err != nil {
			break
		}
		dir = filepath.Dir(dir)
	}
}

func gitCommitForgotten(dotfilesDir string) error {
	// Check if it's a git repo
	gitDir := filepath.Join(dotfilesDir, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		return nil
	}

	// Check for changes
	cmd := execCommand("git", "status", "--porcelain")
	cmd.Dir = dotfilesDir
	output, err := cmd.Output()
	if err != nil || len(output) == 0 {
		return nil
	}

	// Add and commit
	addCmd := execCommand("git", "add", "-A")
	addCmd.Dir = dotfilesDir
	if err := addCmd.Run(); err != nil {
		return err
	}

	commitCmd := execCommand("git", "commit", "-m", "Forget dotfiles via dotgenie")
	commitCmd.Dir = dotfilesDir
	return commitCmd.Run()
}
