package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	applyAskBecomePass   bool
)

var applyCmd = &cobra.Command{
	Use:   "apply",
	Short: "Apply dotfiles and optionally install packages",
	Long: `Apply your dotfiles configuration by linking dotfiles from
common/, workstation/, and hosts/<hostname>/.

Both home (~/) and system files (/etc, /var, /usr) are checked.
If system files need changes, you'll be prompted before sudo runs.

Use --packages to also install packages via Ansible.
Use -K to prompt for sudo password when installing packages.`,
	RunE: runApply,
}

func init() {
	applyCmd.Flags().BoolVarP(&applyWithPackages, "packages", "p", false, "Also install packages via Ansible")
	applyCmd.Flags().BoolVarP(&applyAskBecomePass, "ask-become-pass", "K", false, "Prompt for sudo password (for Ansible)")
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

	// Auto-pull if enabled
	if cfg.AutoPullBeforeApply && !applyDryRun {
		if err := gitPullIfNeeded(paths.DotfilesDir); err != nil {
			fmt.Printf("Warning: %v\n", err)
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
	fmt.Println("\n─── Packages ───")

	playbookPath := filepath.Join(paths.DotfilesDir, "ansible", "playbook.yml")
	if _, err := os.Stat(playbookPath); os.IsNotExist(err) {
		fmt.Println("No ansible/playbook.yml found, skipping packages")
		return nil
	}

	if applyDryRun {
		fmt.Println("(dry run - no changes will be made)")
	}

	// Build ansible-playbook command
	ansibleArgs := []string{
		playbookPath,
		"-i", filepath.Join(paths.DotfilesDir, "ansible", "inventory", "localhost.yml"),
		"-e", fmt.Sprintf("dotgenie_os=%s", cfg.OS),
		"-e", fmt.Sprintf("dotgenie_type=%s", cfg.SystemType),
		"-e", fmt.Sprintf("dotgenie_hostname=%s", cfg.Hostname),
		"-e", fmt.Sprintf("dotgenie_dir=%s", paths.DotfilesDir),
		"--tags", "packages",
	}

	if applyDryRun {
		ansibleArgs = append(ansibleArgs, "--check")
	}
	if applyContinueOnError {
		ansibleArgs = append(ansibleArgs, "-e", "continue_on_error=true")
	}
	if applyVerbose {
		ansibleArgs = append(ansibleArgs, "-v")
	} else {
		ansibleArgs = append(ansibleArgs, "--diff")
	}
	if applyAskBecomePass {
		ansibleArgs = append(ansibleArgs, "-K")
	}

	ansibleCmd := exec.Command("ansible-playbook", ansibleArgs...)
	ansibleCmd.Stdout = os.Stdout
	ansibleCmd.Stderr = os.Stderr
	ansibleCmd.Stdin = os.Stdin
	ansibleCmd.Dir = filepath.Join(paths.DotfilesDir, "ansible")

	if err := ansibleCmd.Run(); err != nil {
		return fmt.Errorf("ansible-playbook failed: %w", err)
	}

	return nil
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

	// Fetch
	fetchCmd := exec.Command("git", "fetch", "--quiet")
	fetchCmd.Dir = dotfilesDir
	_ = fetchCmd.Run() // Ignore error - fetch failure is non-fatal

	// Check if behind
	statusCmd := exec.Command("git", "status", "-uno", "--porcelain=v2", "--branch")
	statusCmd.Dir = dotfilesDir
	output, _ := statusCmd.Output()

	if len(output) > 0 && contains(string(output), "behind") {
		// Check for local changes
		diffCmd := exec.Command("git", "diff", "--quiet")
		diffCmd.Dir = dotfilesDir
		if err := diffCmd.Run(); err != nil {
			return fmt.Errorf("remote has updates but you have local changes - run 'dotgenie sync' first")
		}

		fmt.Println("Pulling remote updates...")
		pullCmd := exec.Command("git", "pull", "--ff-only")
		pullCmd.Dir = dotfilesDir
		pullCmd.Stdout = os.Stdout
		pullCmd.Stderr = os.Stderr
		if err := pullCmd.Run(); err != nil {
			return fmt.Errorf("git pull failed: %w", err)
		}
		fmt.Println("✓ Updated from remote")
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
