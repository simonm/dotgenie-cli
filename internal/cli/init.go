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
	initSystemType string
)

var initCmd = &cobra.Command{
	Use:   "init [repo-url]",
	Short: "Initialize dotgenie with a dotfiles repository",
	Long: `Initialize dotgenie by cloning a dotfiles repository and setting up the configuration.

Examples:
  dotgenie init https://github.com/you/dotfiles
  dotgenie init git@github.com:you/dotfiles.git
  dotgenie init  # Uses existing ~/.dotfiles if present`,
	Args: cobra.MaximumNArgs(1),
	RunE: runInit,
}

func init() {
	initCmd.Flags().StringVarP(&initSystemType, "type", "t", "", "System type (workstation, server, container)")
}

func runInit(cmd *cobra.Command, args []string) error {
	paths := config.DefaultPaths()
	if dotfilesDir != "" {
		paths.DotfilesDir = dotfilesDir
	}

	// Ensure git is available (needed for clone)
	if len(args) > 0 {
		if err := ensureDep("git", "git", config.DetectOS()); err != nil {
			return err
		}
	}

	// Check if already initialized
	if _, err := os.Stat(paths.DotfilesDir); err == nil {
		if len(args) == 0 {
			fmt.Printf("Using existing dotfiles at %s\n", paths.DotfilesDir)
		} else {
			return fmt.Errorf("dotfiles directory already exists: %s\nRemove it first or use --dotfiles to specify a different location", paths.DotfilesDir)
		}
	} else if len(args) > 0 {
		// Clone the repo
		repoURL := args[0]
		fmt.Printf("Cloning %s to %s...\n", repoURL, paths.DotfilesDir)

		gitCmd := exec.Command("git", "clone", repoURL, paths.DotfilesDir)
		gitCmd.Stdout = os.Stdout
		gitCmd.Stderr = os.Stderr
		if err := gitCmd.Run(); err != nil {
			return fmt.Errorf("git clone failed: %w", err)
		}
		fmt.Println("✓ Repository cloned")
	} else {
		return fmt.Errorf("no dotfiles found at %s\nProvide a repository URL: dotgenie init https://github.com/you/dotfiles", paths.DotfilesDir)
	}

	// Detect or prompt for configuration
	cfg := config.NewFromDetection()

	fmt.Printf("\nDetected configuration:\n")
	fmt.Printf("  OS:          %s\n", cfg.OS)
	fmt.Printf("  System type: %s\n", cfg.SystemType)
	fmt.Printf("  Hostname:    %s\n", cfg.Hostname)

	// Allow override of system type
	if initSystemType != "" {
		cfg.SystemType = initSystemType
	} else {
		fmt.Printf("\nSystem types:\n")
		fmt.Printf("  1) workstation - Desktop/laptop with GUI\n")
		fmt.Printf("  2) server      - Headless server\n")
		fmt.Printf("  3) container   - Docker/LXC container\n")
		fmt.Printf("\nConfirm type [%s]: ", cfg.SystemType)

		reader := bufio.NewReader(os.Stdin)
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)

		if input != "" {
			switch input {
			case "1", "workstation":
				cfg.SystemType = "workstation"
			case "2", "server":
				cfg.SystemType = "server"
			case "3", "container":
				cfg.SystemType = "container"
			default:
				cfg.SystemType = input
			}
		}
	}

	// Save local config (machine-specific, gitignored)
	localConfigPath := filepath.Join(paths.DotfilesDir, "config.local.yml")
	if err := cfg.SaveLocal(localConfigPath); err != nil {
		return fmt.Errorf("saving local config: %w", err)
	}
	fmt.Printf("✓ Local configuration saved to %s\n", localConfigPath)

	// Save shared config only if it doesn't exist (cloned repos already have it).
	// Set repo_version to 0 so that the next 'apply' triggers an infrastructure
	// upgrade -- the cloned repo's ansible files may be outdated.
	configPath := filepath.Join(paths.DotfilesDir, "config.yml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		cfg.RepoVersion = 0
		if err := cfg.SaveShared(configPath); err != nil {
			return fmt.Errorf("saving config: %w", err)
		}
		fmt.Printf("✓ Shared configuration saved to %s\n", configPath)
	}

	fmt.Printf("\n✓ Initialization complete!\n\n")
	fmt.Printf("Next steps:\n")
	fmt.Printf("  dotgenie apply           # Link dotfiles and install packages\n")
	fmt.Printf("  dotgenie apply --dotfiles-only  # Link dotfiles only\n")
	fmt.Printf("  dotgenie status          # Check current state\n")

	return nil
}
