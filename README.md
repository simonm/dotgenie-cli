# dotgenie

A fast, simple dotfiles manager written in Go.

## Features

- **Fast parallel symlinking** - Links hundreds of dotfiles in milliseconds
- **Layered configuration** - `common/` -> `workstation/` or `server/` -> `hosts/<hostname>/`
- **Multi-target support** - Manage `home/`, `etc/`, `var/`, `usr/` files
- **Direct symlinks** - Edit `~/.config/foo` and it edits your managed file
- **Package management** - System packages via yay/apt/dnf/brew, dev tools via mise
- **Cross-platform** - Linux (Arch, Ubuntu, Debian) and macOS

## Installation

```bash
curl -fsSL https://raw.githubusercontent.com/simonm/dotgenie-cli/main/install.sh | bash
```

Or download from [releases](https://github.com/simonm/dotgenie-cli/releases).

### Dependencies

dotgenie requires `git` for repository operations. For package installation, it calls your system package manager directly (yay on Arch, apt-get on Debian/Ubuntu, dnf on Fedora/Asahi, brew on macOS) and uses [mise](https://mise.jdx.dev) for developer tools. Missing tools are detected and offered for automatic installation.

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

# Link dotfiles and install packages
dotgenie sync

# Push to GitHub (sync commits and pushes from then on)
cd ~/.dotfiles
git remote add origin https://github.com/YOU/dotfiles
git add -A && git commit -m "Initial dotfiles"
git push -u origin main
```

### Existing Setup (cloning your repo on a new machine)

```bash
# Clone and configure
dotgenie init https://github.com/YOU/dotfiles

# Link dotfiles and install packages
dotgenie sync
```

## Commands

| Command | Description |
|---------|-------------|
| `new` | Create a new dotfiles repository with starter structure |
| `init [repo]` | Clone existing dotfiles repo and configure |
| `status` | Show status of managed dotfiles |
| `adopt <path>` | Import existing configs into management |
| `forget <path>` | Remove configs from management |
| `sync` (or `apply`) | Commit, pull, push, link dotfiles, install changed packages |
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

### sync

The one command for keeping a machine up to date, both ways (`apply` is the same command):

1. Commit local changes. Your dotfiles are symlinks into the repo, so editing `~/.config/foo` edits the repo; the commit message names the machine.
2. Fetch, and rebase onto the remote if it has new commits.
3. Push local commits.
4. Link dotfiles from each layer. System files (`/etc`, `/var`, `/usr`) are offered with sudo.
5. Install packages the first time on a machine, and again whenever the package files change. The last install is recorded as `packages_hash` in `config.local.yml`.

```bash
dotgenie sync                     # Everything above
dotgenie sync --dry-run           # Show what would happen; change nothing
dotgenie sync --no-push           # Commit and pull, but don't push
dotgenie sync --offline           # No git: link and install from the local repo
dotgenie sync --packages          # Install packages even if unchanged
dotgenie sync --verbose           # Show all files being processed
```

If the rebase hits a conflict (the same file changed here and on another machine), sync undoes the rebase, keeps your commit, names the files, and stops before linking. It never resolves a conflict by guessing. If the push fails (say, a server with no push access), the commit is kept, sync says so and carries on; the next sync from a machine with access pushes it. Offline, it links from the local repo. It never creates a branch on the remote.

`auto_pull_before_apply: false` in `config.yml` makes sync behave as `--offline`.

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

A directory is linked file by file: `~/.config/nvim` stays a real directory and each file in it is a symlink into the repo. (Older versions linked the whole directory as one symlink, which let `apply` rename files inside the repo. `sync` now converts any such link it finds.)

To move a managed file to another scope, adopt it again with the new `--scope`:

```bash
dotgenie adopt --scope host ~/.config/fish/config.fish     # Promote: copy common's version to this host
dotgenie forget --scope common ~/.config/fish/config.fish  # Optional: drop the common copy
```

The original copy stays in its layer, so other machines keep using it. The symlink moves to the new copy if the new layer wins on this machine (host beats system type, which beats common). To demote, adopt into the lower layer, then `forget` the higher one.

### forget

Remove configuration files from management:

```bash
dotgenie forget --scope host ~/.config/monitors.xml    # Remove host override
dotgenie forget --scope common ~/.bashrc               # Remove from common
dotgenie forget --scope host /etc/modprobe.d/iwlwifi.conf  # Remove system file
dotgenie forget --keep-repo ~/.config/nvim             # Unlink but keep in repo
```

When you forget a file from one layer, the version in the most specific remaining layer (host, then system type, then common) will take effect on the next `apply`.

### status

```bash
dotgenie status                   # Check home files
dotgenie status --system          # Also check system files (etc/, var/)
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
|   +-- server/
|   |   +-- home/             # -> $HOME (headless servers)
|   +-- hosts/
|       +-- xenon/
|           +-- home/         # -> $HOME (this host only)
|           |   +-- .config/
|           +-- etc/          # -> /etc (this host only)
|               +-- modprobe.d/
|                   +-- iwlwifi.conf
+-- packages/
|   +-- common.yml            # System packages + mise tools for all machines
|   +-- workstation.yml       # Additional packages for desktops/laptops
|   +-- server.yml            # Additional packages for servers
+-- config.yml                # Shared config (committed)
+-- config.local.yml          # Machine-specific config (gitignored)
+-- README.md
```

### Layering

Files are applied in order, with later layers overriding earlier ones:

1. `common/` - Applied to all systems
2. `workstation/` or `server/` - Applied only on systems of that type (`system_type` in `config.local.yml`)
3. `hosts/<hostname>/` - Applied only on the matching host

This lets you have a base config in `common/` and override specific files per-host.

The system types are `workstation` and `server`. `status` and `sync` print the layers that apply on this machine and what each holds, for example:

```
Layers:      common (42 files, common.yml) -> server (no files, server.yml) -> hosts/boron (3 files)
```

They warn if `system_type` is unset, isn't a known type, or names a layer with no files and no package file. `--scope` for `adopt` and `forget` takes `common`, a system type, or `host`; anything else is an error.

### Targets

Each layer can contain multiple targets:

| Target | Destination | Requires sudo |
|--------|-------------|---------------|
| `home/` | `$HOME` | No |
| `etc/` | `/etc` | Yes |
| `var/` | `/var` | Yes |
| `usr/` | `/usr` | Yes |

`sync` always checks system targets and asks before running sudo; `status --system` includes them in the report.

## Package Management

Package files follow the same layering as dotfiles: `common.yml` is always loaded, then the system type file (`workstation.yml` or `server.yml`) adds on top. Each file can contain both system packages and mise tools:

```yaml
# packages/common.yml - installed on every machine
packages:
  - git
  - curl
  - htop
  - fish
  - fd:
      debian: fd-find    # OS-specific name mappings
      ubuntu: fd-find

mise_packages:
  - node@lts
  - python@latest
  - bat@latest
  - ripgrep@latest
```

```yaml
# packages/workstation.yml - added on desktops/laptops
packages:
  - base-devel:
      debian: build-essential
  - ghostty             # AUR packages work on Arch (yay handles them)

mise_packages:
  - go@latest
  - neovim@latest
  - lazygit@latest
```

System packages are installed via yay (Arch), apt-get (Debian/Ubuntu), dnf (Fedora/Asahi), or brew (macOS). yay is auto-installed on Arch if missing. [mise](https://mise.jdx.dev) is auto-installed if missing.

## Configuration

`config.yml` (committed) holds shared preferences:

```yaml
repo_version: 6
auto_pull_before_apply: true
auto_commit_after_adopt: true
auto_push_after_adopt: false
git_author: Your Name <you@example.com>
```

`git_author` is who dotgenie commits as (`sync`, `adopt`, `forget`), on every machine. It's passed to each commit with `git -c`, so git's own config isn't changed. A server with no git identity can still commit. `new` and `init` fill it from the machine's git identity, or ask once. `sync` fills an empty one from the identity of any machine that has one, and commits it. Without `git_author`, dotgenie uses the machine's git identity; with neither, it says how to fix it.

`config.local.yml` (gitignored) holds machine-specific settings:

```yaml
os: arch
system_type: workstation
hostname: xenon
packages_hash: 3f1c0a9e2b7d4c51   # what sync last installed here
```

## Building from Source

```bash
git clone https://github.com/simonm/dotgenie-cli
cd dotgenie-cli
go build -o dotgenie ./cmd/dotgenie
```

## License

MIT
