# dotgenie

A fast, simple dotfiles manager written in Go.

## Features

- **Fast parallel symlinking** - Links hundreds of dotfiles in milliseconds
- **Layered configuration** - `common/` → `workstation/` → `hosts/<hostname>/`
- **Multi-target support** - Manage `home/`, `etc/`, `var/`, `usr/` files
- **Direct symlinks** - Edit `~/.config/foo` and it edits your managed file
- **Package management** - Ansible handles package installation
- **Cross-platform** - Linux (Arch, Ubuntu, Debian) and macOS

## Installation

```bash
curl -fsSL https://raw.githubusercontent.com/simonm/dotgenie-cli/main/install.sh | bash
```

Or download from [releases](https://github.com/simonm/dotgenie-cli/releases).

### Dependencies

dotgenie requires `git` for repository operations and `ansible` for package management. If these are missing, dotgenie will detect your OS and offer to install them automatically using your system package manager (brew, pacman, or apt-get).

## Quick Start

### New Setup (starting from scratch)

```bash
# Create a new dotfiles repository
dotgenie new

# Add your existing configs
dotgenie adopt ~/.config/nvim
dotgenie adopt ~/.bashrc

# Edit packages to install
$EDITOR ~/.dotfiles/packages/common.yml

# Apply (links dotfiles and installs packages)
dotgenie apply

# Push to GitHub
cd ~/.dotfiles
git remote add origin https://github.com/YOU/dotfiles
git add -A && git commit -m "Initial dotfiles"
git push -u origin main
```

### Existing Setup (cloning your repo on a new machine)

```bash
# Clone and configure
dotgenie init https://github.com/YOU/dotfiles

# Apply dotfiles and install packages
dotgenie apply
```

## Commands

| Command | Description |
|---------|-------------|
| `new` | Create a new dotfiles repository with starter structure |
| `init [repo]` | Clone existing dotfiles repo and configure |
| `apply` | Link dotfiles and install packages |
| `status` | Show status of managed dotfiles |
| `adopt <path>` | Import existing configs into management |
| `forget <path>` | Remove configs from management |
| `sync` | Sync with remote git repository |
| `upgrade` | Upgrade dotgenie to the latest release |

### new

Create a new dotfiles repository with the recommended structure:

```bash
dotgenie new                      # Create at ~/.dotfiles
dotgenie new --dotfiles ~/dots    # Create at custom location
```

This creates:
- Directory structure for all layers
- Example package files for your detected OS
- Ansible playbook and roles
- README and .gitignore

### apply

```bash
dotgenie apply                    # Link home files and install packages
dotgenie apply --system           # Also link system files (etc/, var/) with sudo
dotgenie apply --dotfiles-only    # Only link dotfiles, skip packages
dotgenie apply --packages-only    # Only install packages, skip dotfiles
dotgenie apply --dry-run          # Preview changes without making them
dotgenie apply --verbose          # Show all files being processed
```

### adopt

Import existing configuration files into management:

```bash
dotgenie adopt ~/.config/nvim                              # → common/home/
dotgenie adopt --scope workstation ~/.config/hypr          # → workstation/home/
dotgenie adopt --scope host ~/.config/monitors.xml         # → hosts/<hostname>/home/
dotgenie adopt --scope host /etc/modprobe.d/iwlwifi.conf   # → hosts/<hostname>/etc/
dotgenie adopt --copy-only ~/.bashrc                       # Copy without symlinking
dotgenie adopt -y ~/.config/*                              # Skip confirmation
```

### forget

Remove configuration files from management:

```bash
dotgenie forget --scope host ~/.config/monitors.xml    # Remove host override
dotgenie forget --scope common ~/.bashrc               # Remove from common
dotgenie forget --scope host /etc/modprobe.d/iwlwifi.conf  # Remove system file
dotgenie forget --keep-repo ~/.config/nvim             # Unlink but keep in repo
```

When you forget a file from one layer, any version in a lower layer (e.g., common) will take effect on the next `apply`.

### status

```bash
dotgenie status                   # Check home files
dotgenie status --system          # Also check system files (etc/, var/)
```

### sync

```bash
dotgenie sync                     # Fetch and pull if behind
dotgenie sync --push              # Also push local commits
```

### upgrade

Upgrade dotgenie to the latest GitHub release:

```bash
dotgenie upgrade                  # Download and install latest version
dotgenie upgrade --check          # Check for updates without installing
```

The upgrade replaces the running binary in-place. If the install directory is not writable, it will prompt you to run with `sudo`.

## Repository Structure

```
~/.dotfiles/
├── dotfiles/
│   ├── common/
│   │   ├── home/             # → $HOME (all systems)
│   │   │   └── .config/
│   │   │       └── nvim/
│   │   └── etc/              # → /etc (all systems, requires sudo)
│   │       └── environment.d/
│   ├── workstation/
│   │   ├── home/             # → $HOME (desktops/laptops)
│   │   │   └── .config/
│   │   │       └── hypr/
│   │   └── etc/              # → /etc (desktops/laptops)
│   │       └── udev/rules.d/
│   └── hosts/
│       └── xenon/
│           ├── home/         # → $HOME (this host only)
│           │   └── .config/
│           └── etc/          # → /etc (this host only)
│               └── modprobe.d/
│                   └── iwlwifi.conf
├── packages/
│   ├── common.yml            # All systems
│   ├── workstation.yml       # GUI systems
│   ├── server.yml            # Servers
│   └── arch.yml              # OS-specific (arch, ubuntu, debian, macos)
├── ansible/
│   ├── playbook.yml
│   ├── inventory/
│   │   └── localhost.yml
│   └── roles/
│       └── packages/
├── config.yml                # Generated by init/new
└── README.md
```

### Layering

Files are applied in order, with later layers overriding earlier ones:

1. `common/` - Applied to all systems
2. `workstation/` - Applied only if system type is "workstation"
3. `hosts/<hostname>/` - Applied only on the matching host

This lets you have a base config in `common/` and override specific files per-host.

### Targets

Each layer can contain multiple targets:

| Target | Destination | Requires sudo |
|--------|-------------|---------------|
| `home/` | `$HOME` | No |
| `etc/` | `/etc` | Yes |
| `var/` | `/var` | Yes |
| `usr/` | `/usr` | Yes |

Use `--system` flag with `apply` and `status` to include system targets.

## Package Management

Packages are defined in YAML files under `packages/`:

```yaml
# packages/common.yml
packages:
  - git
  - neovim
  - ripgrep
  - fd:
      debian: fd-find    # Different name on Debian/Ubuntu
      ubuntu: fd-find
```

For Arch Linux, you can also install AUR packages:

```yaml
# packages/arch.yml
packages:
  - base-devel

aur_packages:
  - visual-studio-code-bin
  - spotify
```

## Configuration

`config.yml` is created during `dotgenie new` or `dotgenie init`:

```yaml
os: arch
system_type: workstation
hostname: xenon
auto_pull_before_apply: true
auto_commit_after_adopt: true
auto_push_after_adopt: false
```

## Building from Source

```bash
git clone https://github.com/simonm/dotgenie-cli
cd dotgenie-cli
go build -o dotgenie ./cmd/dotgenie
```

## License

MIT
