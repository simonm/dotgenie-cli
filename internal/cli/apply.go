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

// Flags shared by sync (and its alias apply) and the linking helpers below.
var (
	applyDryRun          bool
	applyContinueOnError bool
	applyVerbose         bool
)

func applyAllDotfiles(paths config.Paths, cfg *config.Config) error {
	fmt.Println("\n─── Dotfiles ───")
	start := time.Now()

	mgr := dotfiles.NewManager(
		paths.DotfilesDir,
		paths.Home,
		cfg.SystemType,
		cfg.Hostname,
	)
	printLayers(mgr)

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
			_, _ = fmt.Scanln(&response)
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
