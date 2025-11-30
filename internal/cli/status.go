package cli

import (
	"fmt"
	"path/filepath"

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

	return nil
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
