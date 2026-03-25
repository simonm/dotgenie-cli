package cli

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/simonm/dotgenie/internal/config"
	"github.com/spf13/cobra"
)

var (
	syncNoPush bool
	syncYes    bool
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync dotfiles with remote repository",
	Long: `Sync your dotfiles with the remote git repository.

This will:
  1. Check for uncommitted local changes and offer to commit them
  2. Fetch from remote
  3. Pull if behind (stashing local changes if needed)
  4. Push local commits

Use --no-push to skip pushing after syncing.`,
	RunE: runSync,
}

func init() {
	syncCmd.Flags().BoolVarP(&syncNoPush, "no-push", "n", false, "Skip pushing after syncing")
	syncCmd.Flags().BoolVarP(&syncYes, "yes", "y", false, "Auto-accept all prompts")
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
	if err := ensureDep("git", "git", config.DetectOS()); err != nil {
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
	if _, err := remoteCmd.Output(); err != nil {
		return fmt.Errorf("no remote 'origin' configured")
	}

	// Step 1: Handle uncommitted local changes
	diffCmd := execCommand("git", "status", "--porcelain")
	diffCmd.Dir = paths.DotfilesDir
	diffOutput, _ := diffCmd.Output()
	hasLocalChanges := len(diffOutput) > 0

	if hasLocalChanges {
		fmt.Println("Uncommitted changes:")
		for _, line := range strings.Split(strings.TrimSpace(string(diffOutput)), "\n") {
			if line != "" {
				fmt.Printf("  %s\n", line)
			}
		}

		commit := syncYes
		if !commit {
			fmt.Print("\nCommit these changes? [Y/n] ")
			reader := bufio.NewReader(os.Stdin)
			response, _ := reader.ReadString('\n')
			response = strings.TrimSpace(strings.ToLower(response))
			commit = response == "" || response == "y" || response == "yes"
		}

		if commit {
			// Generate commit message from changed files
			msg := generateCommitMessage(diffOutput)
			fmt.Printf("Committing: %s\n", msg)

			addCmd := execCommand("git", "add", "-A")
			addCmd.Dir = paths.DotfilesDir
			if err := addCmd.Run(); err != nil {
				return fmt.Errorf("git add failed: %w", err)
			}

			commitCmd := execCommand("git", "commit", "-m", msg)
			commitCmd.Dir = paths.DotfilesDir
			if err := commitCmd.Run(); err != nil {
				return fmt.Errorf("git commit failed: %w", err)
			}
			fmt.Println("Committed")
			hasLocalChanges = false
		}
	}

	// Step 2: Fetch
	fmt.Print("Fetching... ")
	fetchCmd := execCommand("git", "fetch", "--all", "--prune")
	fetchCmd.Dir = paths.DotfilesDir
	if err := fetchCmd.Run(); err != nil {
		fmt.Println("failed")
		return fmt.Errorf("fetch failed: %w", err)
	}
	fmt.Println("done")

	// Get current branch
	branchCmd := execCommand("git", "rev-parse", "--abbrev-ref", "HEAD")
	branchCmd.Dir = paths.DotfilesDir
	branchOutput, _ := branchCmd.Output()
	currentBranch := strings.TrimSpace(string(branchOutput))

	// Check ahead/behind
	statusCmd := execCommand("git", "rev-list", "--left-right", "--count", fmt.Sprintf("origin/%s...HEAD", currentBranch))
	statusCmd.Dir = paths.DotfilesDir
	statusOutput, err := statusCmd.Output()

	var behind, ahead int
	if err == nil {
		_, _ = fmt.Sscanf(strings.TrimSpace(string(statusOutput)), "%d\t%d", &behind, &ahead)
	}

	// Step 3: Pull if behind
	if behind > 0 {
		fmt.Printf("Behind by %d commit(s), pulling...\n", behind)

		if hasLocalChanges {
			// Uncommitted changes that user chose not to commit -- stash, pull, unstash
			fmt.Println("Stashing local changes...")
			stashCmd := execCommand("git", "stash", "push", "-m", "dotgenie sync: auto-stash")
			stashCmd.Dir = paths.DotfilesDir
			if err := stashCmd.Run(); err != nil {
				return fmt.Errorf("git stash failed: %w", err)
			}

			pullCmd := execCommand("git", "pull", "--ff-only")
			pullCmd.Dir = paths.DotfilesDir
			pullCmd.Stdout = os.Stdout
			pullCmd.Stderr = os.Stderr
			if err := pullCmd.Run(); err != nil {
				// Try to restore stash on failure
				popCmd := execCommand("git", "stash", "pop")
				popCmd.Dir = paths.DotfilesDir
				_ = popCmd.Run()
				return fmt.Errorf("pull failed: %w", err)
			}

			fmt.Println("Restoring local changes...")
			popCmd := execCommand("git", "stash", "pop")
			popCmd.Dir = paths.DotfilesDir
			if err := popCmd.Run(); err != nil {
				return fmt.Errorf("stash pop failed (your changes are in 'git stash'): %w", err)
			}
		} else {
			pullCmd := execCommand("git", "pull", "--ff-only")
			pullCmd.Dir = paths.DotfilesDir
			pullCmd.Stdout = os.Stdout
			pullCmd.Stderr = os.Stderr
			if err := pullCmd.Run(); err != nil {
				return fmt.Errorf("pull failed: %w", err)
			}
		}
		fmt.Println("Pulled successfully")

		// Re-check ahead count after pull
		statusCmd2 := execCommand("git", "rev-list", "--left-right", "--count", fmt.Sprintf("origin/%s...HEAD", currentBranch))
		statusCmd2.Dir = paths.DotfilesDir
		statusOutput2, err2 := statusCmd2.Output()
		if err2 == nil {
			_, _ = fmt.Sscanf(strings.TrimSpace(string(statusOutput2)), "%d\t%d", &behind, &ahead)
		}
	} else {
		fmt.Println("Already up to date")
	}

	// Step 4: Push if there are local commits
	if ahead > 0 && !syncNoPush {
		fmt.Printf("Ahead by %d commit(s), pushing...\n", ahead)
		pushCmd := execCommand("git", "push")
		pushCmd.Dir = paths.DotfilesDir
		pushCmd.Stdout = os.Stdout
		pushCmd.Stderr = os.Stderr
		if err := pushCmd.Run(); err != nil {
			return fmt.Errorf("push failed: %w", err)
		}
		fmt.Println("Pushed successfully")
	} else if ahead > 0 && syncNoPush {
		fmt.Printf("Ahead by %d commit(s) (not pushing, --no-push specified)\n", ahead)
	}

	fmt.Println("\nSync complete!")
	return nil
}

// generateCommitMessage creates a commit message from git status output
func generateCommitMessage(statusOutput []byte) string {
	lines := strings.Split(strings.TrimSpace(string(statusOutput)), "\n")
	files := make([]string, 0, len(lines))
	for _, line := range lines {
		if len(line) > 3 {
			files = append(files, strings.TrimSpace(line[2:]))
		}
	}
	if len(files) == 1 {
		return fmt.Sprintf("Update %s", filepath.Base(files[0]))
	}
	return fmt.Sprintf("Update %d dotfiles", len(files))
}

// execCommand is a wrapper to allow testing
var execCommand = exec.Command
