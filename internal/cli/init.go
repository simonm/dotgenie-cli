package cli

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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
	initCmd.Flags().StringVarP(&initSystemType, "type", "t", "", "System type ("+strings.Join(config.SystemTypes, ", ")+")")
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
		if !config.IsSystemType(initSystemType) {
			return fmt.Errorf("unknown system type %q: use one of %s", initSystemType, strings.Join(config.SystemTypes, ", "))
		}
		cfg.SystemType = initSystemType
	} else {
		fmt.Printf("\nSystem types:\n")
		fmt.Printf("  1) workstation - Desktop/laptop with GUI\n")
		fmt.Printf("  2) server      - Headless server\n")
		reader := bufio.NewReader(os.Stdin)
		for {
			fmt.Printf("\nConfirm type [%s]: ", cfg.SystemType)
			input, err := reader.ReadString('\n')
			input = strings.TrimSpace(input)
			if input == "" {
				break
			}
			if n, convErr := strconv.Atoi(input); convErr == nil && n >= 1 && n <= len(config.SystemTypes) {
				input = config.SystemTypes[n-1]
			}
			if config.IsSystemType(input) {
				cfg.SystemType = input
				break
			}
			fmt.Printf("Unknown system type %q: use one of %s\n", input, strings.Join(config.SystemTypes, ", "))
			if err != nil {
				return fmt.Errorf("no valid system type given")
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

	// dotgenie commits as git_author (config.yml) or git's own identity. If
	// there's neither, ask once, so the first adopt or sync doesn't fail.
	if shared, err := config.Load(configPath); err == nil && shared.GitAuthor == "" && gitIdentity(paths.DotfilesDir) == "" {
		fmt.Println("\nThis machine has no git identity and config.yml has no git_author.")
		if a := askAuthor(); a != "" {
			shared.GitAuthor = a
			if err := shared.SaveShared(configPath); err != nil {
				return fmt.Errorf("saving git_author: %w", err)
			}
			fmt.Printf("Recorded git_author: %s in config.yml\n", a)
		} else {
			fmt.Println(errNoIdentity)
		}
	}

	fmt.Printf("\n✓ Initialization complete!\n\n")
	fmt.Printf("Next steps:\n")
	fmt.Printf("  dotgenie sync            # Pull, link dotfiles, install packages\n")
	fmt.Printf("  dotgenie sync --dry-run  # Preview first\n")
	fmt.Printf("  dotgenie status          # Check current state\n")

	return nil
}
