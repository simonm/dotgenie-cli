package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/simonm/dotgenie/internal/config"
	"github.com/spf13/cobra"
)

var (
	syncPush bool
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync dotfiles with remote repository",
	Long: `Sync your dotfiles with the remote git repository.

By default, this will:
  1. Fetch from remote
  2. Show status (ahead/behind)
  3. Pull if behind and no local changes conflict
  4. Optionally push local commits

Use --push to also push local commits after pulling.`,
	RunE: runSync,
}

func init() {
	syncCmd.Flags().BoolVarP(&syncPush, "push", "p", false, "Push local commits after syncing")
}

func runSync(cmd *cobra.Command, args []string) error {
	paths := config.DefaultPaths()
	if dotfilesDir != "" {
		paths.DotfilesDir = dotfilesDir
	}

	// Check if dotfiles dir exists
	if _, err := os.Stat(paths.DotfilesDir); os.IsNotExist(err) {
		return fmt.Errorf("dotfiles directory not found: %s\nRun 'dotgenie init' first", paths.DotfilesDir)
	}

	// Ensure git is available
	if err := ensureDep("git", config.DetectOS()); err != nil {
		return err
	}

	// Check if it's a git repo
	gitDir := filepath.Join(paths.DotfilesDir, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		return fmt.Errorf("dotfiles directory is not a git repository")
	}

	fmt.Printf("Syncing %s\n\n", paths.DotfilesDir)

	// Check for remote
	remoteCmd := execCommand("git", "remote", "get-url", "origin")
	remoteCmd.Dir = paths.DotfilesDir
	remoteOutput, err := remoteCmd.Output()
	if err != nil {
		return fmt.Errorf("no remote 'origin' configured")
	}
	fmt.Printf("Remote: %s\n", strings.TrimSpace(string(remoteOutput)))

	// Fetch
	fmt.Println("Fetching...")
	fetchCmd := execCommand("git", "fetch", "--all", "--prune")
	fetchCmd.Dir = paths.DotfilesDir
	fetchCmd.Stdout = os.Stdout
	fetchCmd.Stderr = os.Stderr
	if err := fetchCmd.Run(); err != nil {
		return fmt.Errorf("fetch failed: %w", err)
	}

	// Get current branch
	branchCmd := execCommand("git", "rev-parse", "--abbrev-ref", "HEAD")
	branchCmd.Dir = paths.DotfilesDir
	branchOutput, _ := branchCmd.Output()
	currentBranch := strings.TrimSpace(string(branchOutput))
	fmt.Printf("\nBranch: %s\n", currentBranch)

	// Check ahead/behind
	statusCmd := execCommand("git", "rev-list", "--left-right", "--count", fmt.Sprintf("origin/%s...HEAD", currentBranch))
	statusCmd.Dir = paths.DotfilesDir
	statusOutput, err := statusCmd.Output()

	var behind, ahead int
	if err == nil {
		_, _ = fmt.Sscanf(strings.TrimSpace(string(statusOutput)), "%d\t%d", &behind, &ahead)
	}

	fmt.Printf("Status: %d commit(s) ahead, %d commit(s) behind\n", ahead, behind)

	// Check for local changes
	diffCmd := execCommand("git", "status", "--porcelain")
	diffCmd.Dir = paths.DotfilesDir
	diffOutput, _ := diffCmd.Output()
	hasLocalChanges := len(diffOutput) > 0

	if hasLocalChanges {
		fmt.Println("\nLocal changes:")
		fmt.Println(string(diffOutput))
	}

	// Pull if behind
	if behind > 0 {
		if hasLocalChanges {
			fmt.Println("\nWarning: You have local changes. Consider committing or stashing them first.")
			fmt.Println("Attempting to pull with rebase...")

			pullCmd := execCommand("git", "pull", "--rebase", "--autostash")
			pullCmd.Dir = paths.DotfilesDir
			pullCmd.Stdout = os.Stdout
			pullCmd.Stderr = os.Stderr
			if err := pullCmd.Run(); err != nil {
				return fmt.Errorf("pull failed: %w\nResolve conflicts and try again", err)
			}
		} else {
			fmt.Println("\nPulling...")
			pullCmd := execCommand("git", "pull", "--ff-only")
			pullCmd.Dir = paths.DotfilesDir
			pullCmd.Stdout = os.Stdout
			pullCmd.Stderr = os.Stderr
			if err := pullCmd.Run(); err != nil {
				return fmt.Errorf("pull failed: %w", err)
			}
		}
		fmt.Println("Pulled successfully")
	}

	// Push if requested and ahead
	if syncPush && ahead > 0 {
		fmt.Println("\nPushing...")
		pushCmd := execCommand("git", "push")
		pushCmd.Dir = paths.DotfilesDir
		pushCmd.Stdout = os.Stdout
		pushCmd.Stderr = os.Stderr
		if err := pushCmd.Run(); err != nil {
			return fmt.Errorf("push failed: %w", err)
		}
		fmt.Println("Pushed successfully")
	} else if ahead > 0 && !syncPush {
		fmt.Println("\nYou have local commits. Use 'dotgenie sync --push' to push them.")
	}

	fmt.Println("\nSync complete!")
	return nil
}

// execCommand is a wrapper to allow testing
var execCommand = exec.Command
