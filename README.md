# dotgenie

A fast, simple dotfiles manager written in Go.

## Features

- **Fast parallel symlinking** - Links hundreds of dotfiles in milliseconds
- **Layered configuration** - `common/` -> `workstation/` -> `hosts/<hostname>/`
- **Multi-target support** - Manage `home/`, `etc/`, `var/`, `usr/` files
- **Direct symlinks** - Edit `~/.config/foo` and it edits your managed file
- **Package management** - System packages via yay/apt/brew, dev tools via mise
- **Cross-platform** - Linux (Arch, Ubuntu, Debian) and macOS

## Installation

```bash
curl -fsSL https://raw.githubusercontent.com/simonm/dotgenie-cli/main/install.sh | bash
```

Or download from [releases](https://github.com/simonm/dotgenie-cli/releases).

### Dependencies

dotgenie requires `git` for repository operations. For package installation, it calls your system package manager directly (yay on Arch, apt-get on Debian/Ubuntu, brew on macOS) and uses [mise](https://mise.jdx.dev) for developer tools. Missing tools are detected and offered for automatic installation.

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
dotgenie apply --packages

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
dotgenie apply --packages
```

## Commands

| Command | Description |
|---------|-------------|
| `new` | Create a new dotfiles repository with starter structure |
| `init [repo]` | Clone existing dotfiles repo and configure |
| `apply` | Link dotfiles and optionally install packages |
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
- mise tool list for dev tools
- README and .gitignore

### apply

```bash
dotgenie apply                    # Link home dotfiles only
dotgenie apply --packages         # Also install system packages and mise tools
dotgenie apply --dry-run          # Preview changes without making them
dotgenie apply --verbose          # Show all files being processed
```

### adopt

Import existing configuration files into management:

```bash
dotgenie adopt ~/.config/nvim                              # -> common/home/
dotgenie adopt --scope workstation ~/.config/hypr          # -> workstation/home/
dotgenie adopt --scope host ~/.config/monitors.xml         # -> hosts/<hostname>/home/
dotgenie adopt --scope host /etc/modprobe.d/iwlwifi.conf   # -> hosts/<hostname>/etc/
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
dotgenie sync                     # Commit, fetch, pull, push
dotgenie sync -y                  # Auto-accept all prompts
dotgenie sync -ya                 # Sync and apply dotfiles
dotgenie sync --no-push           # Skip pushing
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
+-- dotfiles/
|   +-- common/
|   |   +-- home/             # -> $HOME (all systems)
|   |   |   +-- .config/
|   |   |       +-- nvim/
|   |   +-- etc/              # -> /etc (all systems, requires sudo)
|   |       +-- environment.d/
|   +-- workstation/
|   |   +-- home/             # -> $HOME (desktops/laptops)
|   |   |   +-- .config/
|   |   |       +-- hypr/
|   |   +-- etc/              # -> /etc (desktops/laptops)
|   |       +-- udev/rules.d/
|   +-- hosts/
|       +-- xenon/
|           +-- home/         # -> $HOME (this host only)
|           |   +-- .config/
|           +-- etc/          # -> /etc (this host only)
|               +-- modprobe.d/
|                   +-- iwlwifi.conf
+-- packages/
|   +-- common.yml            # System packages for all machines
|   +-- mise.yml              # Dev tools installed via mise
|   +-- workstation.yml       # GUI system packages
|   +-- server.yml            # Server packages
|   +-- arch.yml              # OS-specific (arch, ubuntu, debian, macos)
+-- config.yml                # Shared config (committed)
+-- config.local.yml          # Machine-specific config (gitignored)
+-- README.md
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

### System Packages

System packages are installed via your OS package manager (yay on Arch, apt-get on Debian/Ubuntu, brew on macOS). Define them in YAML files under `packages/`:

```yaml
# packages/common.yml
packages:
  - git
  - curl
  - htop
  - fish
  - fd:
      debian: fd-find    # Different name on Debian/Ubuntu
      ubuntu: fd-find
```

On Arch, yay is auto-installed if missing and handles both official and AUR packages seamlessly:

```yaml
# packages/arch.yml
packages:
  - base-devel
  - ghostty           # AUR package, yay handles it
```

### mise Tools

Developer tools with frequent releases are managed by [mise](https://mise.jdx.dev), which installs the latest versions regardless of OS:

```yaml
# packages/mise.yml
mise_packages:
  - node@lts
  - python@latest
  - go@latest
  - neovim@latest
  - eza@latest
  - starship@latest
  - bat@latest
  - ripgrep@latest
```

mise is auto-installed if missing.

## Configuration

`config.yml` (committed) holds shared preferences:

```yaml
repo_version: 6
auto_pull_before_apply: true
auto_commit_after_adopt: true
auto_push_after_adopt: false
```

`config.local.yml` (gitignored) holds machine-specific settings:

```yaml
os: arch
system_type: workstation
hostname: xenon
```

## Building from Source

```bash
git clone https://github.com/simonm/dotgenie-cli
cd dotgenie-cli
go build -o dotgenie ./cmd/dotgenie
```

## License

MIT
