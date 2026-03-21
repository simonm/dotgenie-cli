package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/simonm/dotgenie/internal/config"
	"github.com/spf13/cobra"
)

const currentRepoVersion = 3

var newCmd = &cobra.Command{
	Use:   "new",
	Short: "Create a new dotfiles repository",
	Long: `Create a new dotfiles repository with the recommended structure.

This creates a starter dotfiles repo with:
  - Directory structure (common/, workstation/, hosts/<hostname>/)
  - Example package files for your detected OS
  - Ansible playbook and roles
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
		"ansible/inventory",
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

	// Create Ansible files
	if err := writeAnsibleInfrastructure(paths.DotfilesDir); err != nil {
		return err
	}
	fmt.Println("✓ Created Ansible playbook")

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

func createPackageFiles(dotfilesDir string, cfg *config.Config) error {
	// Common packages (all systems)
	commonPkgs := `# Packages installed on all systems
packages:
  - git
  - curl
  - wget
  - htop
  - tmux
  - neovim
  - ripgrep
  - fd:
      debian: fd-find
      ubuntu: fd-find
  - fzf
  - jq
  - tree
`
	if err := os.WriteFile(filepath.Join(dotfilesDir, "packages/common.yml"), []byte(commonPkgs), 0644); err != nil {
		return err
	}

	// Workstation packages (GUI systems)
	workstationPkgs := `# Packages installed on workstations (desktop/laptop with GUI)
packages:
  # - firefox
  # - alacritty
  # - ghostty  # Uncomment if using Ghostty terminal
`
	if err := os.WriteFile(filepath.Join(dotfilesDir, "packages/workstation.yml"), []byte(workstationPkgs), 0644); err != nil {
		return err
	}

	// Server packages
	serverPkgs := `# Packages installed on servers
packages:
  # - docker
  # - nginx
`
	if err := os.WriteFile(filepath.Join(dotfilesDir, "packages/server.yml"), []byte(serverPkgs), 0644); err != nil {
		return err
	}

	// OS-specific packages
	osPkgs := map[string]string{
		"arch": `# Arch Linux specific packages
packages:
  # - base-devel
  # - yay  # AUR helper

# AUR packages (requires kewlfft.aur collection)
aur_packages:
  # - visual-studio-code-bin
`,
		"ubuntu": `# Ubuntu specific packages
packages:
  # - build-essential
`,
		"debian": `# Debian specific packages
packages:
  # - build-essential
`,
		"macos": `# macOS specific packages (via Homebrew)
packages:
  # - coreutils
`,
	}

	// Write detected OS file, or default to arch
	osContent, ok := osPkgs[cfg.OS]
	if !ok {
		osContent = osPkgs["arch"]
	}
	osFile := fmt.Sprintf("packages/%s.yml", cfg.OS)
	if err := os.WriteFile(filepath.Join(dotfilesDir, osFile), []byte(osContent), 0644); err != nil {
		return err
	}

	return nil
}

func writeAnsibleInfrastructure(dotfilesDir string) error {
	// Ensure directories exist (needed for both new repos and upgrades)
	for _, dir := range []string{"ansible/collections", "ansible/inventory", "ansible/roles/packages/tasks", "ansible/roles/packages/defaults"} {
		if err := os.MkdirAll(filepath.Join(dotfilesDir, dir), 0755); err != nil {
			return err
		}
	}

	// Collection requirements
	requirements := `---
collections:
  - name: community.general
  - name: kewlfft.aur
`
	if err := os.WriteFile(filepath.Join(dotfilesDir, "ansible/collections/requirements.yml"), []byte(requirements), 0644); err != nil {
		return err
	}

	// Inventory
	inventory := `all:
  hosts:
    localhost:
      ansible_connection: local
      ansible_python_interpreter: "{{ ansible_playbook_python }}"
`
	if err := os.WriteFile(filepath.Join(dotfilesDir, "ansible/inventory/localhost.yml"), []byte(inventory), 0644); err != nil {
		return err
	}

	// Main playbook
	playbook := `---
- name: Configure system
  hosts: localhost
  become: false

  vars:
    dotgenie_os: "{{ lookup('env', 'DOTGENIE_OS') | default('arch', true) }}"
    dotgenie_type: "{{ lookup('env', 'DOTGENIE_TYPE') | default('workstation', true) }}"
    dotgenie_hostname: "{{ ansible_facts['hostname'] }}"
    dotgenie_dir: "{{ lookup('env', 'HOME') }}/.dotfiles"

  tasks:
    - name: Include packages role
      ansible.builtin.include_role:
        name: packages
      tags: [packages]
`
	if err := os.WriteFile(filepath.Join(dotfilesDir, "ansible/playbook.yml"), []byte(playbook), 0644); err != nil {
		return err
	}

	// Packages role - defaults
	roleDefaults := `---
packages: []
aur_packages: []
continue_on_error: false
`
	if err := os.WriteFile(filepath.Join(dotfilesDir, "ansible/roles/packages/defaults/main.yml"), []byte(roleDefaults), 0644); err != nil {
		return err
	}

	// Packages role - tasks
	// Each package file is loaded into a separate namespace to avoid overwriting,
	// then the lists are merged together before installation.
	roleTasks := `---
# Load package definitions from each layer into separate namespaces
- name: Load common packages
  ansible.builtin.include_vars:
    file: "{{ dotgenie_dir }}/packages/common.yml"
    name: _common_pkgs
  tags: [packages]

- name: Load OS-specific packages
  ansible.builtin.include_vars:
    file: "{{ dotgenie_dir }}/packages/{{ dotgenie_os }}.yml"
    name: _os_pkgs
  failed_when: false
  tags: [packages]

- name: Load system type packages
  ansible.builtin.include_vars:
    file: "{{ dotgenie_dir }}/packages/{{ dotgenie_type }}.yml"
    name: _type_pkgs
  failed_when: false
  tags: [packages]

# Merge all package lists together
- name: Combine package lists
  ansible.builtin.set_fact:
    packages: "{{ (_common_pkgs.packages | default([], true)) + (_os_pkgs.packages | default([], true)) + (_type_pkgs.packages | default([], true)) }}"
    aur_packages: "{{ (_common_pkgs.aur_packages | default([], true)) + (_os_pkgs.aur_packages | default([], true)) + (_type_pkgs.aur_packages | default([], true)) }}"
  tags: [packages]

# Arch Linux
- name: Install packages (Arch)
  become: true
  community.general.pacman:
    name: "{{ item.arch | default(item) if item is mapping else item }}"
    state: present
  loop: "{{ packages }}"
  when:
    - dotgenie_os == 'arch'
    - packages | length > 0
    - (item is string) or (item.arch is defined) or (item[item | first] is not mapping)
  ignore_errors: "{{ continue_on_error }}"
  tags: [packages]

- name: Install AUR packages (Arch)
  become: true
  become_user: "{{ ansible_user_id }}"
  kewlfft.aur.aur:
    name: "{{ item }}"
    state: present
  loop: "{{ aur_packages }}"
  when:
    - dotgenie_os == 'arch'
    - aur_packages | length > 0
  ignore_errors: "{{ continue_on_error }}"
  tags: [packages]

# Debian/Ubuntu
- name: Install packages (Debian/Ubuntu)
  become: true
  ansible.builtin.apt:
    name: "{{ item.debian | default(item.ubuntu) | default(item) if item is mapping else item }}"
    state: present
  loop: "{{ packages }}"
  when:
    - dotgenie_os in ['debian', 'ubuntu']
    - packages | length > 0
  ignore_errors: "{{ continue_on_error }}"
  tags: [packages]

# macOS
- name: Install packages (macOS)
  community.general.homebrew:
    name: "{{ item.macos | default(item) if item is mapping else item }}"
    state: present
  loop: "{{ packages }}"
  when:
    - dotgenie_os == 'macos'
    - packages | length > 0
  ignore_errors: "{{ continue_on_error }}"
  tags: [packages]

# Custom local tasks (optional, never overwritten by dotgenie).
# Create ansible/roles/packages/tasks/local.yml to add your own tasks.
- name: Check for local customizations
  ansible.builtin.stat:
    path: "{{ role_path }}/tasks/local.yml"
  register: _local_tasks
  tags: [packages]

- name: Include local customizations
  ansible.builtin.include_tasks: local.yml
  when: _local_tasks.stat.exists
  tags: [packages]
`
	if err := os.WriteFile(filepath.Join(dotfilesDir, "ansible/roles/packages/tasks/main.yml"), []byte(roleTasks), 0644); err != nil {
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
