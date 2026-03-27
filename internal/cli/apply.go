package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/simonm/dotgenie/internal/config"
	"github.com/simonm/dotgenie/internal/dotfiles"
	"github.com/spf13/cobra"
)

var (
	applyWithPackages    bool
	applyDryRun          bool
	applyContinueOnError bool
	applyVerbose         bool
)

var applyCmd = &cobra.Command{
	Use:   "apply",
	Short: "Apply dotfiles and optionally install packages",
	Long: `Apply your dotfiles configuration by linking dotfiles from
common/, workstation/, and hosts/<hostname>/.

Both home (~/) and system files (/etc, /var, /usr) are checked.
If system files need changes, you'll be prompted before sudo runs.

Use --packages to also install system packages and mise tools.`,
	RunE: runApply,
}

func init() {
	applyCmd.Flags().BoolVarP(&applyWithPackages, "packages", "p", false, "Also install packages")
	applyCmd.Flags().BoolVarP(&applyDryRun, "dry-run", "n", false, "Show what would be done without making changes")
	applyCmd.Flags().BoolVarP(&applyContinueOnError, "continue-on-error", "k", false, "Continue even if some packages fail")
	applyCmd.Flags().BoolVarP(&applyVerbose, "verbose", "v", false, "Show detailed output")
}

func runApply(cmd *cobra.Command, args []string) error {
	paths := config.DefaultPaths()
	if dotfilesDir != "" {
		paths.DotfilesDir = dotfilesDir
	}

	// Load config
	cfg, err := config.Load(filepath.Join(paths.DotfilesDir, "config.yml"))
	if err != nil {
		return fmt.Errorf("loading config: %w\nRun 'dotgenie init' first", err)
	}

	// Check if repo infrastructure needs upgrading
	if cfg.RepoVersion < currentRepoVersion {
		// Update repo version
		cfg.RepoVersion = currentRepoVersion
		configPath := filepath.Join(paths.DotfilesDir, "config.yml")
		if err := cfg.SaveShared(configPath); err != nil {
			return fmt.Errorf("saving updated config: %w", err)
		}
	}

	// Auto-pull if enabled (check git is available first)
	if cfg.AutoPullBeforeApply && !applyDryRun {
		if err := ensureDep("git", "git", cfg.OS); err != nil {
			fmt.Printf("Warning: %v (skipping auto-pull)\n", err)
		} else {
			if err := gitPullIfNeeded(paths.DotfilesDir); err != nil {
				fmt.Printf("Warning: %v\n", err)
			}
		}
	}

	// Apply dotfiles (home + system)
	if err := applyAllDotfiles(paths, cfg); err != nil {
		return err
	}

	// Install packages (only if --packages flag)
	if applyWithPackages {
		if err := applyPackages(paths, cfg); err != nil {
			return err
		}
	}

	fmt.Println("\n✓ Apply complete!")
	return nil
}

func applyAllDotfiles(paths config.Paths, cfg *config.Config) error {
	fmt.Println("\n─── Dotfiles ───")
	start := time.Now()

	mgr := dotfiles.NewManager(
		paths.DotfilesDir,
		paths.Home,
		cfg.SystemType,
		cfg.Hostname,
	)

	if applyDryRun {
		fmt.Println("(dry run - no changes will be made)")
	}

	// Apply home files
	homeTargets := []dotfiles.Target{
		{Name: "home", RootPath: paths.Home, NeedSudo: false},
	}

	actions, err := mgr.Apply(homeTargets, applyDryRun)
	if err != nil {
		return fmt.Errorf("applying home dotfiles: %w", err)
	}

	if len(actions) > 0 {
		fmt.Println("\n  [home]")
		dotfiles.PrintActions(filterByTarget(actions, "home"), applyVerbose)
	}

	// Check system files
	systemTargets := []dotfiles.Target{
		{Name: "etc", RootPath: "/etc", NeedSudo: true},
		{Name: "var", RootPath: "/var", NeedSudo: true},
		{Name: "usr", RootPath: "/usr", NeedSudo: true},
	}

	// Check if there are any system files that need changes
	systemActions, _ := mgr.Status(systemTargets)
	needsSystemChanges := false
	for _, a := range systemActions {
		if a.Action != "ok" {
			needsSystemChanges = true
			break
		}
	}

	if needsSystemChanges {
		fmt.Println("\n  [system] - changes needed:")
		for _, target := range []string{"etc", "var", "usr"} {
			filtered := filterByTarget(systemActions, target)
			needsChange := false
			for _, a := range filtered {
				if a.Action != "ok" {
					needsChange = true
					break
				}
			}
			if needsChange {
				fmt.Printf("    [%s]\n", target)
				dotfiles.PrintActions(filtered, applyVerbose)
			}
		}

		if applyDryRun {
			fmt.Println("\n  (dry run - no system changes made)")
		} else {
			// Prompt user before running sudo
			fmt.Print("\nSystem files need updating. Run with sudo? [y/N] ")
			var response string
			fmt.Scanln(&response)
			if response == "y" || response == "Y" || response == "yes" {
				if err := applySystemFilesWithSudo(paths, cfg, systemTargets); err != nil {
					return err
				}
			} else {
				fmt.Println("Skipped system files")
			}
		}
	}

	fmt.Printf("\nCompleted in %v\n", time.Since(start).Round(time.Millisecond))
	return nil
}

func filterByTarget(actions []dotfiles.FileAction, target string) []dotfiles.FileAction {
	var filtered []dotfiles.FileAction
	for _, a := range actions {
		if a.Target == target {
			filtered = append(filtered, a)
		}
	}
	return filtered
}

func applySystemFilesWithSudo(paths config.Paths, cfg *config.Config, targets []dotfiles.Target) error {
	// Re-run dotgenie with sudo for system files
	// This is cleaner than trying to sudo individual operations

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("getting executable path: %w", err)
	}

	args := []string{exe, "apply-system-internal",
		"--dotfiles", paths.DotfilesDir,
	}
	if applyVerbose {
		args = append(args, "--verbose")
	}

	sudoCmd := exec.Command("sudo", args...)
	sudoCmd.Stdout = os.Stdout
	sudoCmd.Stderr = os.Stderr
	sudoCmd.Stdin = os.Stdin

	return sudoCmd.Run()
}

// Internal command for applying system files (called via sudo)
var applySystemInternalCmd = &cobra.Command{
	Use:    "apply-system-internal",
	Hidden: true,
	RunE:   runApplySystemInternal,
}

func init() {
	applySystemInternalCmd.Flags().BoolVarP(&applyVerbose, "verbose", "v", false, "Show detailed output")
	rootCmd.AddCommand(applySystemInternalCmd)
}

func runApplySystemInternal(cmd *cobra.Command, args []string) error {
	paths := config.DefaultPaths()
	if dotfilesDir != "" {
		paths.DotfilesDir = dotfilesDir
	}

	cfg, err := config.Load(filepath.Join(paths.DotfilesDir, "config.yml"))
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	mgr := dotfiles.NewManager(
		paths.DotfilesDir,
		paths.Home,
		cfg.SystemType,
		cfg.Hostname,
	)

	systemTargets := []dotfiles.Target{
		{Name: "etc", RootPath: "/etc", NeedSudo: true},
		{Name: "var", RootPath: "/var", NeedSudo: true},
		{Name: "usr", RootPath: "/usr", NeedSudo: true},
	}

	actions, err := mgr.Apply(systemTargets, false)
	if err != nil {
		return fmt.Errorf("applying system dotfiles: %w", err)
	}

	for _, target := range []string{"etc", "var", "usr"} {
		filtered := filterByTarget(actions, target)
		if len(filtered) > 0 {
			fmt.Printf("\n    [%s]\n", target)
			dotfiles.PrintActions(filtered, applyVerbose)
		}
	}

	return nil
}

func applyPackages(paths config.Paths, cfg *config.Config) error {
	return applyPackagesNew(paths, cfg, applyDryRun, applyContinueOnError, applyVerbose)
}

func gitPullIfNeeded(dotfilesDir string) error {
	// Check if it's a git repo
	gitDir := filepath.Join(dotfilesDir, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		return nil
	}

	// Check for remote
	remoteCmd := exec.Command("git", "remote", "get-url", "origin")
	remoteCmd.Dir = dotfilesDir
	if err := remoteCmd.Run(); err != nil {
		return nil // No remote, skip
	}

	fmt.Print("─── Git Sync ───\nFetching remote changes... ")

	// Fetch
	fetchCmd := exec.Command("git", "fetch")
	fetchCmd.Dir = dotfilesDir
	if err := fetchCmd.Run(); err != nil {
		fmt.Println("failed (continuing without sync)")
		return nil
	}

	// Check if behind
	statusCmd := exec.Command("git", "status", "-uno", "--porcelain=v2", "--branch")
	statusCmd.Dir = dotfilesDir
	output, _ := statusCmd.Output()

	if len(output) > 0 && contains(string(output), "behind") {
		fmt.Println("updates available")

		// Check for local changes
		diffCmd := exec.Command("git", "diff", "--quiet")
		diffCmd.Dir = dotfilesDir
		if err := diffCmd.Run(); err != nil {
			return fmt.Errorf("remote has updates but you have local changes - run 'dotgenie sync' first")
		}

		// Show what changed
		logCmd := exec.Command("git", "log", "--oneline", "HEAD..origin/HEAD", "--")
		logCmd.Dir = dotfilesDir
		logOutput, _ := logCmd.Output()
		if len(logOutput) > 0 {
			fmt.Println("  Incoming changes:")
			for _, line := range strings.Split(strings.TrimSpace(string(logOutput)), "\n") {
				if line != "" {
					fmt.Printf("    %s\n", line)
				}
			}
		}

		fmt.Println("  Pulling...")
		pullCmd := exec.Command("git", "pull", "--no-rebase")
		pullCmd.Dir = dotfilesDir
		pullCmd.Stdout = os.Stdout
		pullCmd.Stderr = os.Stderr
		if err := pullCmd.Run(); err != nil {
			return fmt.Errorf("git pull failed: %w", err)
		}
		fmt.Println("  ✓ Updated from remote")
	} else {
		fmt.Println("up to date")
	}

	return nil
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
