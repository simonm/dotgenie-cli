package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/simonm/dotgenie/internal/config"
	"github.com/spf13/cobra"
)

const currentRepoVersion = 6

var newCmd = &cobra.Command{
	Use:   "new",
	Short: "Create a new dotfiles repository",
	Long: `Create a new dotfiles repository with the recommended structure.

This creates a starter dotfiles repo with:
  - Directory structure (common/, workstation/, hosts/<hostname>/)
  - Example package files for your detected OS
  - Package lists for system and mise tools
  - Initial config.yml

After running this, you can:
  1. Add your dotfiles with 'dotgenie adopt'
  2. Edit packages/*.yml to specify your packages
  3. Run 'dotgenie apply' to link and install

Example:
  dotgenie new                      # Create at ~/.dotfiles
  dotgenie new --dotfiles ~/dots    # Create at custom location`,
	RunE: runNew,
}

func runNew(cmd *cobra.Command, args []string) error {
	paths := config.DefaultPaths()
	if dotfilesDir != "" {
		paths.DotfilesDir = dotfilesDir
	}

	// Check if already exists
	if _, err := os.Stat(paths.DotfilesDir); err == nil {
		return fmt.Errorf("directory already exists: %s\nRemove it first or use --dotfiles to specify a different location", paths.DotfilesDir)
	}

	fmt.Printf("Creating new dotfiles repository at %s\n\n", paths.DotfilesDir)

	// Detect system
	cfg := config.NewFromDetection()

	// Create directory structure
	dirs := []string{
		"dotfiles/common/home/.config",
		"dotfiles/workstation/home/.config",
		fmt.Sprintf("dotfiles/hosts/%s/home/.config", cfg.Hostname),
		fmt.Sprintf("dotfiles/hosts/%s/etc", cfg.Hostname),
		"packages",
	}

	for _, dir := range dirs {
		fullPath := filepath.Join(paths.DotfilesDir, dir)
		if err := os.MkdirAll(fullPath, 0755); err != nil {
			return fmt.Errorf("creating directory %s: %w", dir, err)
		}
	}
	fmt.Println("✓ Created directory structure")

	// Create package files
	if err := createPackageFiles(paths.DotfilesDir, cfg); err != nil {
		return err
	}
	fmt.Println("✓ Created package files")

	// Create shared config (committed to repo)
	cfg.RepoVersion = currentRepoVersion
	configPath := filepath.Join(paths.DotfilesDir, "config.yml")
	if err := cfg.SaveShared(configPath); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}
	fmt.Println("✓ Created config.yml")

	// Create local config (gitignored, machine-specific)
	localConfigPath := filepath.Join(paths.DotfilesDir, "config.local.yml")
	if err := cfg.SaveLocal(localConfigPath); err != nil {
		return fmt.Errorf("saving local config: %w", err)
	}
	fmt.Println("✓ Created config.local.yml")

	// Create .gitignore
	if err := createGitignore(paths.DotfilesDir); err != nil {
		return err
	}
	fmt.Println("✓ Created .gitignore")

	// Create README
	if err := createReadme(paths.DotfilesDir, cfg); err != nil {
		return err
	}
	fmt.Println("✓ Created README.md")

	// Initialize git repo
	gitCmd := execCommand("git", "init")
	gitCmd.Dir = paths.DotfilesDir
	if err := gitCmd.Run(); err != nil {
		fmt.Printf("Warning: git init failed: %v\n", err)
	} else {
		// Set pull strategy to merge (avoids warning on first pull)
		pullCfg := execCommand("git", "config", "pull.rebase", "false")
		pullCfg.Dir = paths.DotfilesDir
		_ = pullCfg.Run()
		fmt.Println("✓ Initialized git repository")
	}

	fmt.Printf("\n✓ Created new dotfiles repository!\n\n")
	fmt.Printf("Detected configuration:\n")
	fmt.Printf("  OS:          %s\n", cfg.OS)
	fmt.Printf("  System type: %s\n", cfg.SystemType)
	fmt.Printf("  Hostname:    %s\n", cfg.Hostname)

	fmt.Printf("\nNext steps:\n")
	fmt.Printf("  1. Add dotfiles:     dotgenie adopt ~/.config/nvim\n")
	fmt.Printf("  2. Edit packages:    $EDITOR %s/packages/common.yml\n", paths.DotfilesDir)
	fmt.Printf("  3. Apply:            dotgenie apply\n")
	fmt.Printf("  4. Push to GitHub:   cd %s && git remote add origin <url> && git push -u origin main\n", paths.DotfilesDir)

	return nil
}

func createPackageFiles(dotfilesDir string, _ *config.Config) error {
	// Common: system packages + mise tools for all machines
	commonPkgs := `# Packages for all systems
# System packages are installed via yay (Arch), apt-get (Debian/Ubuntu), or brew (macOS).
# Mise packages get the latest versions regardless of OS (mise is auto-installed).
packages:
  - git
  - curl
  - wget
  - rsync
  - less
  - htop
  - tree
  - fish

mise_packages:
  - node@lts
  - python@latest
  - bat@latest
  - ripgrep@latest
  - fd@latest
  - fzf@latest
  - jq@latest
  - zoxide@latest
  - starship@latest
  - eza@latest
`
	if err := os.WriteFile(filepath.Join(dotfilesDir, "packages/common.yml"), []byte(commonPkgs), 0644); err != nil {
		return err
	}

	// Workstation: GUI apps + heavier dev tools
	workstationPkgs := `# Additional packages for workstations (desktop/laptop with GUI)
# These are additive on top of common.yml.
packages:
  - base-devel:
      debian: build-essential
      ubuntu: build-essential
  # - firefox
  # - ghostty

mise_packages:
  - go@latest
  - rust@latest
  - lazygit@latest
  - neovim@latest
  - yazi@latest
  - tmux@latest
`
	if err := os.WriteFile(filepath.Join(dotfilesDir, "packages/workstation.yml"), []byte(workstationPkgs), 0644); err != nil {
		return err
	}

	// Server: minimal additions
	serverPkgs := `# Additional packages for servers
# These are additive on top of common.yml.
packages:
  - build-essential:
      arch: base-devel
  # - docker
  # - nginx

mise_packages:
  - neovim@latest
  - tmux@latest
`
	if err := os.WriteFile(filepath.Join(dotfilesDir, "packages/server.yml"), []byte(serverPkgs), 0644); err != nil {
		return err
	}

	return nil
}

func createGitignore(dotfilesDir string) error {
	gitignore := `# Machine-specific config (regenerated by dotgenie init/apply)
config.local.yml

# OS files
.DS_Store
Thumbs.db

# Editor
*.swp
*.swo
*~
.idea/
.vscode/
`
	return os.WriteFile(filepath.Join(dotfilesDir, ".gitignore"), []byte(gitignore), 0644)
}

func createReadme(dotfilesDir string, cfg *config.Config) error {
	readme := fmt.Sprintf(`# Dotfiles

Managed with [dotgenie](https://github.com/simonm/dotgenie-cli).

## Quick Start

%s%s%s

## Structure

%s%s%s

## Adding Dotfiles

%s%s%s

## Package Management

Edit files in %s to manage packages:

- %s - All systems
- %s - GUI systems only
- %s - OS-specific packages

`, "```bash\n",
		`# Install dotgenie
curl -fsSL https://raw.githubusercontent.com/simonm/dotgenie-cli/main/install.sh | bash

# Clone and apply
dotgenie init https://github.com/YOUR_USERNAME/dotfiles
dotgenie apply
`, "```\n",
		"```\n",
		`dotfiles/
├── common/home/        # All systems
├── workstation/home/   # Desktop/laptop
└── hosts/<hostname>/   # Host-specific
    ├── home/
    └── etc/
`, "```\n",
		"```bash\n",
		`dotgenie adopt ~/.config/nvim                    # Add to common
dotgenie adopt --scope workstation ~/.config/hypr  # Add to workstation
dotgenie adopt --scope host ~/.config/monitors.xml # Add to this host only
`, "```\n",
		"`packages/`",
		"`common.yml`",
		"`workstation.yml`",
		fmt.Sprintf("`%s.yml`", cfg.OS),
	)

	return os.WriteFile(filepath.Join(dotfilesDir, "README.md"), []byte(readme), 0644)
}
