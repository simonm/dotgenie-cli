package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/simonm/dotgenie/internal/config"
	"github.com/simonm/dotgenie/internal/dotfiles"
	"github.com/spf13/cobra"
)

var (
	statusSystem bool
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show status of managed dotfiles",
	Long: `Show the current status of all managed dotfiles.

By default, only shows home/ dotfiles. Use --system to also check
system files (etc/, var/, usr/).`,
	RunE: runStatus,
}

func init() {
	statusCmd.Flags().BoolVar(&statusSystem, "system", false, "Also check system files (etc/, var/)")
}

func runStatus(cmd *cobra.Command, args []string) error {
	paths := config.DefaultPaths()
	if dotfilesDir != "" {
		paths.DotfilesDir = dotfilesDir
	}

	// Load config
	cfg, err := config.Load(filepath.Join(paths.DotfilesDir, "config.yml"))
	if err != nil {
		return fmt.Errorf("loading config: %w\nRun 'dotgenie init' first", err)
	}

	fmt.Println("─── Configuration ───")
	fmt.Printf("  Dotfiles: %s\n", paths.DotfilesDir)
	cfg.Print()

	fmt.Println("\n─── Dotfiles Status ───")

	mgr := dotfiles.NewManager(
		paths.DotfilesDir,
		paths.Home,
		cfg.SystemType,
		cfg.Hostname,
	)

	// Check home files
	homeTargets := []dotfiles.Target{
		{Name: "home", RootPath: paths.Home, NeedSudo: false},
	}

	actions, err := mgr.Status(homeTargets)
	if err != nil {
		return fmt.Errorf("checking status: %w", err)
	}

	if len(actions) > 0 {
		fmt.Println("\n  [home]")
		printStatusSummary(actions)
	}

	// Check system files if requested
	if statusSystem {
		systemTargets := []dotfiles.Target{
			{Name: "etc", RootPath: "/etc", NeedSudo: true},
			{Name: "var", RootPath: "/var", NeedSudo: true},
			{Name: "usr", RootPath: "/usr", NeedSudo: true},
		}

		sysActions, err := mgr.Status(systemTargets)
		if err != nil {
			return fmt.Errorf("checking system status: %w", err)
		}

		for _, target := range []string{"etc", "var", "usr"} {
			filtered := filterByTarget(sysActions, target)
			if len(filtered) > 0 {
				fmt.Printf("\n  [%s]\n", target)
				printStatusSummary(filtered)
			}
		}

		actions = append(actions, sysActions...)
	}

	// Overall summary
	var ok, missing, wrong, notLink, errors int
	for _, a := range actions {
		switch a.Action {
		case "ok":
			ok++
		case "missing":
			missing++
		case "wrong_link":
			wrong++
		case "not_symlink":
			notLink++
		case "error":
			errors++
		}
	}

	fmt.Printf("\n─── Summary ───\n")
	fmt.Printf("Total: %d files, %d ok", len(actions), ok)
	if missing > 0 {
		fmt.Printf(", %d missing", missing)
	}
	if wrong > 0 {
		fmt.Printf(", %d wrong", wrong)
	}
	if notLink > 0 {
		fmt.Printf(", %d not symlinks", notLink)
	}
	if errors > 0 {
		fmt.Printf(", %d errors", errors)
	}
	fmt.Println()

	if missing+wrong+notLink > 0 {
		fmt.Println("\nRun 'dotgenie apply' to fix issues")
		if statusSystem {
			fmt.Println("Use 'dotgenie apply --system' to also fix system files")
		}
	}

	// Git repo status (silent fail)
	printRepoStatus(paths.DotfilesDir)

	// Version check (silent fail)
	printVersionStatus()

	return nil
}

// printRepoStatus shows the git sync state of the dotfiles repo.
func printRepoStatus(dotfilesDir string) {
	gitDir := filepath.Join(dotfilesDir, ".git")
	if !fileExists(gitDir) {
		return
	}

	fmt.Println("\n--- Repo ---")

	// Check for uncommitted changes
	statusCmd := exec.Command("git", "status", "--porcelain")
	statusCmd.Dir = dotfilesDir
	statusOutput, err := statusCmd.Output()
	if err != nil {
		return
	}

	if len(statusOutput) > 0 {
		// Only trim the trailing newline -- preserve leading spaces in status codes.
		lines := strings.Split(strings.TrimRight(string(statusOutput), "\n"), "\n")
		fmt.Printf("  %d uncommitted change(s):\n", len(lines))
		for _, line := range lines {
			if len(line) < 3 {
				continue
			}
			code := line[:2]
			path := line[3:]
			fmt.Printf("    %s  %s\n", describeGitStatus(code), path)
		}
	} else {
		fmt.Println("  Working tree clean")
	}

	// Fetch (silent, quick timeout)
	fetchCmd := exec.Command("git", "fetch", "--quiet")
	fetchCmd.Dir = dotfilesDir
	if err := fetchCmd.Run(); err != nil {
		return // no network, skip remote status
	}

	// Get current branch
	branchCmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	branchCmd.Dir = dotfilesDir
	branchOutput, err := branchCmd.Output()
	if err != nil {
		return
	}
	branch := strings.TrimSpace(string(branchOutput))

	// Check ahead/behind
	revCmd := exec.Command("git", "rev-list", "--left-right", "--count", fmt.Sprintf("origin/%s...HEAD", branch))
	revCmd.Dir = dotfilesDir
	revOutput, err := revCmd.Output()
	if err != nil {
		return
	}

	var behind, ahead int
	_, _ = fmt.Sscanf(strings.TrimSpace(string(revOutput)), "%d\t%d", &behind, &ahead)

	if ahead > 0 || behind > 0 {
		parts := []string{}
		if ahead > 0 {
			parts = append(parts, fmt.Sprintf("%d ahead", ahead))
		}
		if behind > 0 {
			parts = append(parts, fmt.Sprintf("%d behind", behind))
		}
		fmt.Printf("  Remote: %s\n", strings.Join(parts, ", "))
	} else {
		fmt.Println("  Remote: up to date")
	}

	if len(statusOutput) > 0 || ahead > 0 || behind > 0 {
		fmt.Println("  Run 'dotgenie sync' to sync")
	}
}

// printVersionStatus checks if a newer dotgenie version is available.
func printVersionStatus() {
	latest, err := getLatestRelease()
	if err != nil {
		return
	}

	latestVersion := strings.TrimPrefix(latest.TagName, "v")
	currentVersion := strings.TrimPrefix(version, "v")

	fmt.Println("\n--- Version ---")
	if isNewerVersion(latestVersion, currentVersion) {
		fmt.Printf("  Current: %s, Latest: %s\n", version, latest.TagName)
		fmt.Println("  Run 'dotgenie upgrade' to update")
	} else {
		fmt.Printf("  %s (latest)\n", version)
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// describeGitStatus turns a two-char git porcelain status code into a short
// human label. First char is index state, second is worktree state.
func describeGitStatus(code string) string {
	if code == "??" {
		return "new    "
	}
	if code == "!!" {
		return "ignored"
	}
	// Prefer describing the worktree state; fall back to index state.
	c := code[1]
	if c == ' ' {
		c = code[0]
	}
	switch c {
	case 'M':
		return "modified"
	case 'A':
		return "added   "
	case 'D':
		return "deleted "
	case 'R':
		return "renamed "
	case 'C':
		return "copied  "
	case 'U':
		return "unmerged"
	case 'T':
		return "typechg "
	default:
		return code
	}
}

func printStatusSummary(actions []dotfiles.FileAction) {
	var ok, missing, wrong, notLink, errors int
	for _, a := range actions {
		switch a.Action {
		case "ok":
			ok++
		case "missing":
			missing++
			fmt.Printf("    ○ %s (not linked)\n", a.RelPath)
		case "wrong_link":
			wrong++
			fmt.Printf("    ! %s (wrong symlink)\n", a.RelPath)
		case "not_symlink":
			notLink++
			fmt.Printf("    ! %s (exists but not a symlink)\n", a.RelPath)
		case "error":
			errors++
			fmt.Printf("    ✗ %s: %v\n", a.RelPath, a.Error)
		}
	}

	fmt.Printf("    %d total, %d ok", len(actions), ok)
	if missing > 0 {
		fmt.Printf(", %d missing", missing)
	}
	if wrong > 0 {
		fmt.Printf(", %d wrong", wrong)
	}
	if notLink > 0 {
		fmt.Printf(", %d not symlinks", notLink)
	}
	if errors > 0 {
		fmt.Printf(", %d errors", errors)
	}
	fmt.Println()
}
