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
	applyDotfilesOnly   bool
	applyPackagesOnly   bool
	applyDryRun         bool
	applyContinueOnError bool
	applyVerbose        bool
)

var applyCmd = &cobra.Command{
	Use:   "apply",
	Short: "Apply dotfiles and install packages",
	Long: `Apply your dotfiles configuration:
  1. Link dotfiles from common/, workstation/, and hosts/<hostname>/
  2. Install packages via Ansible

Use --dotfiles-only or --packages-only to run just one step.`,
	RunE: runApply,
}

func init() {
	applyCmd.Flags().BoolVar(&applyDotfilesOnly, "dotfiles-only", false, "Only link dotfiles, skip packages")
	applyCmd.Flags().BoolVar(&applyPackagesOnly, "packages-only", false, "Only install packages, skip dotfiles")
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
			fmt.Printf("⚠ Warning: %v\n", err)
		}
	}

	// Apply dotfiles
	if !applyPackagesOnly {
		if err := applyDotfiles(paths, cfg); err != nil {
			return err
		}
	}

	// Install packages
	if !applyDotfilesOnly {
		if err := applyPackages(paths, cfg); err != nil {
			return err
		}
	}

	fmt.Println("\n✓ Apply complete!")
	return nil
}

func applyDotfiles(paths config.Paths, cfg *config.Config) error {
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

	actions, err := mgr.Apply(applyDryRun)
	if err != nil {
		return fmt.Errorf("applying dotfiles: %w", err)
	}

	dotfiles.PrintActions(actions, applyVerbose)
	fmt.Printf("Completed in %v\n", time.Since(start).Round(time.Millisecond))

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

	ansibleCmd := exec.Command("ansible-playbook", ansibleArgs...)
	ansibleCmd.Stdout = os.Stdout
	ansibleCmd.Stderr = os.Stderr
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
	fetchCmd.Run()

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
