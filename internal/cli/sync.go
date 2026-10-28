package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/simonm/dotgenie/internal/config"
	"github.com/spf13/cobra"
)

var (
	syncNoPush   bool
	syncOffline  bool
	syncYes      bool // accepted for old scripts; sync no longer asks
	syncApply    bool // accepted for old scripts; sync always links
	syncPackages bool
)

var syncCmd = &cobra.Command{
	Use:     "sync",
	Aliases: []string{"apply"},
	Short:   "Bring this machine's dotfiles up to date, both ways",
	Long: `Bring this machine's dotfiles up to date, both ways:

  1. Commit local changes (your dotfiles are symlinks into the repo, so
     editing ~/.config/foo edits the repo)
  2. Fetch, and rebase onto the remote if it has new commits
  3. Push local commits
  4. Link dotfiles from each layer (common, system type, host)
  5. Install packages the first time, and again whenever the package files
     change (--packages installs regardless)

A rebase conflict stops before linking and names the files; nothing is
resolved by guessing. A failed push (no access from this machine) is a
warning: the commit is kept and the next sync from anywhere with access
pushes it. With no network, or --offline, it links from the local repo.

"apply" is the same command.`,
	RunE: runSync,
}

func init() {
	f := syncCmd.Flags()
	f.BoolVarP(&applyDryRun, "dry-run", "n", false, "Show what would happen; change nothing")
	f.BoolVar(&syncNoPush, "no-push", false, "Commit and pull, but don't push")
	f.BoolVar(&syncOffline, "offline", false, "Skip git: link and install from the local repo only")
	f.BoolVarP(&syncPackages, "packages", "p", false, "Install packages even if the package files haven't changed")
	f.BoolVarP(&applyContinueOnError, "continue-on-error", "k", false, "Continue even if some packages fail")
	f.BoolVarP(&applyVerbose, "verbose", "v", false, "Show detailed output")
	f.BoolVarP(&syncYes, "yes", "y", false, "No effect: sync no longer asks")
	f.BoolVarP(&syncApply, "apply", "a", false, "No effect: sync always links")
	_ = f.MarkHidden("yes")
	_ = f.MarkHidden("apply")
}

func runSync(cmd *cobra.Command, args []string) error {
	paths := config.DefaultPaths()
	if dotfilesDir != "" {
		paths.DotfilesDir = dotfilesDir
	}

	cfg, err := config.Load(filepath.Join(paths.DotfilesDir, "config.yml"))
	if err != nil {
		return fmt.Errorf("loading config: %w\nRun 'dotgenie init' first", err)
	}

	if applyDryRun {
		fmt.Println("(dry run - no changes will be made)")
	}

	// Repo infrastructure upgrade (shared config format).
	if cfg.RepoVersion < currentRepoVersion && !applyDryRun {
		cfg.RepoVersion = currentRepoVersion
		if err := cfg.SaveShared(filepath.Join(paths.DotfilesDir, "config.yml")); err != nil {
			return fmt.Errorf("saving updated config: %w", err)
		}
	}

	// auto_pull_before_apply: false in config.yml means the same as --offline.
	if syncOffline || !cfg.AutoPullBeforeApply {
		fmt.Println("--- Git ---\nSkipped (offline)")
	} else {
		if err := ensureDep("git", "git", cfg.OS); err != nil {
			fmt.Printf("Warning: %v (skipping git)\n", err)
		} else {
			fmt.Println("--- Git ---")
			res, err := gitSync(paths.DotfilesDir, gitSyncOpts{
				DryRun: applyDryRun,
				NoPush: syncNoPush,
				Host:   cfg.Hostname,
			})
			for _, n := range res.Notes {
				fmt.Println(n)
			}
			if err != nil {
				return err
			}
		}
	}

	if err := applyAllDotfiles(paths, cfg); err != nil {
		return err
	}

	if err := syncPackagesIfChanged(paths, cfg); err != nil {
		return err
	}

	fmt.Println("\nSync complete")
	maybeCheckForUpdate(paths, cfg)
	return nil
}

// syncPackagesIfChanged installs packages if --packages is set, on a
// machine's first sync after init or new, or if the package files (or OS, or
// system type) changed since the last install. The record is a hash in
// config.local.yml, written only after a clean install. A machine with no
// record at all was set up before it existed: sync doesn't reinstall
// everything there, it says how to start.
func syncPackagesIfChanged(paths config.Paths, cfg *config.Config) error {
	h := packagesHash(paths.DotfilesDir, cfg)
	why := ""
	switch {
	case syncPackages:
		why = "--packages"
	case cfg.PackagesHash == "":
		fmt.Println("\n--- Packages ---\nNo record of a package install on this machine (set up before dotgenie kept one).")
		fmt.Println("Run 'dotgenie sync --packages' once to install and start tracking changes.")
		return nil
	case cfg.PackagesHash == config.PackagesNeverInstalled:
		why = "first sync on this machine"
	case cfg.PackagesHash != h:
		why = "package files changed"
	}
	if why == "" {
		fmt.Println("\n--- Packages ---\nUp to date (package files unchanged since the last install)")
		return nil
	}
	fmt.Printf("\nInstalling packages: %s\n", why)
	if err := applyPackagesNew(paths, cfg, applyDryRun, applyContinueOnError, applyVerbose); err != nil {
		return err
	}
	if applyDryRun {
		return nil
	}
	cfg.PackagesHash = h
	if err := cfg.SaveLocal(filepath.Join(paths.DotfilesDir, "config.local.yml")); err != nil {
		return fmt.Errorf("recording the package install: %w", err)
	}
	return nil
}

// packagesHash identifies what an install would do on this machine: the OS,
// the system type, and the two package files' contents.
func packagesHash(dotfilesDir string, cfg *config.Config) string {
	h := sha256.New()
	fmt.Fprintf(h, "os=%s\ntype=%s\n", cfg.OS, cfg.SystemType)
	files := []string{"common.yml"}
	if cfg.SystemType != "" {
		files = append(files, cfg.SystemType+".yml")
	}
	for _, f := range files {
		data, err := os.ReadFile(filepath.Join(dotfilesDir, "packages", f))
		if err != nil {
			data = []byte("(missing)")
		}
		fmt.Fprintf(h, "%s %d\n", f, len(data))
		h.Write(data)
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

type gitSyncOpts struct {
	DryRun bool
	NoPush bool
	Host   string // goes in the commit message, so the log says which machine
}

// gitSyncResult is what gitSync did, for the caller and for tests.
type gitSyncResult struct {
	Committed []string // files committed (or that would be, in a dry run)
	Pulled    int      // remote commits rebased onto
	Pushed    int      // local commits pushed
	Notes     []string // lines to show the user
}

// errConflict means the rebase stopped on a conflict and was undone.
var errConflict = errors.New("rebase conflict")

// gitSync commits local changes, rebases onto origin's copy of the current
// branch, and pushes. It never resolves a conflict and never creates a
// remote branch. Being offline, having no remote, or a failed push are
// notes, not errors: the caller still links from the local repo.
func gitSync(dir string, o gitSyncOpts) (gitSyncResult, error) {
	var r gitSyncResult
	note := func(format string, a ...any) { r.Notes = append(r.Notes, fmt.Sprintf(format, a...)) }

	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		note("Not a git repository: linking from local files")
		return r, nil
	}

	// 1. Commit local changes.
	status, err := git(dir, "status", "--porcelain")
	if err != nil {
		return r, fmt.Errorf("git status: %w", err)
	}
	if status != "" {
		for _, line := range strings.Split(status, "\n") {
			if len(line) > 3 {
				r.Committed = append(r.Committed, strings.TrimSpace(line[2:]))
			}
		}
		msg := generateCommitMessage([]byte(status))
		if o.Host != "" {
			msg += " (" + o.Host + ")"
		}
		if o.DryRun {
			note("Would commit: %s", msg)
		} else {
			if _, err := git(dir, "add", "-A"); err != nil {
				return r, fmt.Errorf("git add: %w", err)
			}
			if _, err := git(dir, "commit", "-m", msg); err != nil {
				return r, fmt.Errorf("git commit: %w", err)
			}
			note("Committed: %s", msg)
		}
		for _, f := range r.Committed {
			note("  %s", f)
		}
	}

	if _, err := git(dir, "remote", "get-url", "origin"); err != nil {
		note("No remote 'origin': nothing to pull or push")
		return r, nil
	}
	branch, err := git(dir, "symbolic-ref", "--short", "HEAD")
	if err != nil {
		note("Detached HEAD: not pulling or pushing")
		return r, nil
	}

	// 2. Fetch, then rebase if behind.
	if out, err := git(dir, "fetch", "origin"); err != nil {
		note("Fetch failed (offline?): linking from the local repo. %s", firstLine(out))
		return r, nil
	}
	remote := "origin/" + branch
	if _, err := git(dir, "rev-parse", "--verify", "--quiet", remote); err != nil {
		note("Branch %s isn't on origin: not pulling or pushing (push it yourself to share it)", branch)
		return r, nil
	}
	counts, err := git(dir, "rev-list", "--left-right", "--count", remote+"...HEAD")
	if err != nil {
		return r, fmt.Errorf("comparing with %s: %w", remote, err)
	}
	var behind, ahead int
	_, _ = fmt.Sscanf(counts, "%d\t%d", &behind, &ahead)

	if behind > 0 {
		if o.DryRun {
			note("Would pull %d commit(s) from %s", behind, remote)
		} else {
			if out, err := git(dir, "rebase", remote); err != nil {
				conflicted, _ := git(dir, "diff", "--name-only", "--diff-filter=U")
				_, _ = git(dir, "rebase", "--abort")
				files := strings.Fields(conflicted)
				if len(files) == 0 {
					return r, fmt.Errorf("rebase onto %s failed, undone: %s", remote, firstLine(out))
				}
				return r, fmt.Errorf("%w: %s changed both here and on %s. The rebase was undone and your commit kept; nothing was linked. Resolve with: cd %s && git rebase %s",
					errConflict, strings.Join(files, ", "), remote, dir, remote)
			}
			r.Pulled = behind
			note("Pulled %d commit(s) from %s", behind, remote)
		}
	}

	// 3. Push.
	if o.DryRun && r.Committed != nil {
		ahead++ // the commit a real run would make
	}
	switch {
	case ahead == 0:
		if behind == 0 {
			note("Up to date with %s", remote)
		}
	case o.NoPush:
		note("%d local commit(s) not pushed (--no-push)", ahead)
	case o.DryRun:
		note("Would push %d commit(s) to %s", ahead, remote)
	default:
		if out, err := git(dir, "push", "origin", branch); err != nil {
			note("Committed, not pushed (%s). The next sync from a machine with access will push it.", firstLine(out))
		} else {
			r.Pushed = ahead
			note("Pushed %d commit(s) to %s", ahead, remote)
		}
	}
	return r, nil
}

// git runs git in dir and returns its trimmed combined output.
func git(dir string, args ...string) (string, error) {
	c := execCommand("git", args...)
	c.Dir = dir
	out, err := c.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	return s
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
