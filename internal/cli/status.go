package cli

import (
	"fmt"
	"path/filepath"

	"github.com/simonm/dotgenie/internal/config"
	"github.com/simonm/dotgenie/internal/dotfiles"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show status of managed dotfiles",
	RunE:  runStatus,
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

	actions, err := mgr.Status()
	if err != nil {
		return fmt.Errorf("checking status: %w", err)
	}

	var ok, missing, wrong, notLink, errors int
	for _, a := range actions {
		switch a.Action {
		case "ok":
			ok++
		case "missing":
			missing++
			fmt.Printf("  ○ %s (not linked)\n", a.RelPath)
		case "wrong_link":
			wrong++
			fmt.Printf("  ! %s (wrong symlink)\n", a.RelPath)
		case "not_symlink":
			notLink++
			fmt.Printf("  ! %s (exists but not a symlink)\n", a.RelPath)
		case "error":
			errors++
			fmt.Printf("  ✗ %s: %v\n", a.RelPath, a.Error)
		}
	}

	fmt.Printf("\nSummary: %d total, %d ok", len(actions), ok)
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
	}

	return nil
}
