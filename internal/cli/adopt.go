package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/simonm/dotgenie/internal/config"
	"github.com/simonm/dotgenie/internal/dotfiles"
	"github.com/spf13/cobra"
)

var (
	adoptScope    string
	adoptCopyOnly bool
	adoptYes      bool
)

var adoptCmd = &cobra.Command{
	Use:   "adopt <path>...",
	Short: "Adopt existing dotfiles into management",
	Long: `Adopt existing configuration files into dotgenie management.

The files are moved into your dotfiles repository and replaced with symlinks.
The target (home/, etc/, var/) is auto-detected based on the file path.

You can specify which layer to adopt into:
  - common:      Shared across all systems
  - workstation: Desktop/laptop systems only (or any custom system type)
  - host:        This specific host only

If the file is already managed in one layer and you adopt it into a different
layer, dotgenie copies (never moves) the content. The original stays put so
other systems that used it are unaffected. The local symlink is redirected
only when the new layer wins the specificity race on this machine.

Examples:
  dotgenie adopt ~/.config/nvim                           # → common/home/
  dotgenie adopt --scope workstation ~/.config/hypr       # → workstation/home/
  dotgenie adopt --scope host /etc/modprobe.d/iwlwifi.conf  # → hosts/<hostname>/etc/
  dotgenie adopt --copy-only ~/.bashrc                    # Copy without symlinking`,
	Args: cobra.MinimumNArgs(1),
	RunE: runAdopt,
}

func init() {
	adoptCmd.Flags().StringVarP(&adoptScope, "scope", "s", "common", "Layer to adopt into (common, workstation, host)")
	adoptCmd.Flags().BoolVar(&adoptCopyOnly, "copy-only", false, "Copy files without creating symlinks")
	adoptCmd.Flags().BoolVarP(&adoptYes, "yes", "y", false, "Skip confirmation prompts")
}

func runAdopt(cmd *cobra.Command, args []string) error {
	paths := config.DefaultPaths()
	if dotfilesDir != "" {
		paths.DotfilesDir = dotfilesDir
	}

	// Load config
	cfg, err := config.Load(filepath.Join(paths.DotfilesDir, "config.yml"))
	if err != nil {
		return fmt.Errorf("loading config: %w\nRun 'dotgenie init' first", err)
	}

	// Determine target layer directory
	layer := dotfiles.GetLayerForPath(adoptScope, cfg.Hostname)

	fmt.Printf("Adopting into layer: %s\n", layer)

	// Expand globs and collect all files to adopt
	var filesToAdopt []string
	for _, pattern := range args {
		// Expand ~ to home directory
		if strings.HasPrefix(pattern, "~/") {
			pattern = filepath.Join(paths.Home, pattern[2:])
		}

		// Expand globs
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return fmt.Errorf("invalid glob pattern %s: %w", pattern, err)
		}

		if len(matches) == 0 {
			// Not a glob, treat as literal path
			filesToAdopt = append(filesToAdopt, pattern)
		} else {
			filesToAdopt = append(filesToAdopt, matches...)
		}
	}

	// Process each file/directory
	for _, sourcePath := range filesToAdopt {
		if err := adoptPath(sourcePath, layer, paths, cfg, adoptCopyOnly, adoptYes); err != nil {
			fmt.Printf("Error adopting %s: %v\n", sourcePath, err)
			// Hint if path looks like a scope name
			basename := filepath.Base(sourcePath)
			if basename == "common" || basename == "workstation" || basename == "host" {
				fmt.Printf("  Hint: Did you mean --scope %s?\n", basename)
			}
		}
	}

	// Auto-commit if enabled
	if cfg.AutoCommitAfterAdopt && !adoptCopyOnly {
		if !checkCommand("git") {
			fmt.Println("Warning: git not found, skipping auto-commit")
		} else if err := gitCommitAdopted(paths.DotfilesDir); err != nil {
			fmt.Printf("Warning: auto-commit failed: %v\n", err)
		}
	}

	return nil
}

func adoptPath(sourcePath, layer string, paths config.Paths, cfg *config.Config, copyOnly, autoYes bool) error {
	// Get absolute path
	absSource, err := filepath.Abs(sourcePath)
	if err != nil {
		return err
	}

	// Check if source is a symlink (from stow, chezmoi, etc., or from us)
	var existingLinkTarget string
	if linkTarget, err := os.Readlink(absSource); err == nil {
		// It's a symlink - resolve to absolute path
		if !filepath.IsAbs(linkTarget) {
			linkTarget = filepath.Join(filepath.Dir(absSource), linkTarget)
		}
		existingLinkTarget = linkTarget
	}

	// Resolve symlinks to get the real file
	realSource, err := filepath.EvalSymlinks(absSource)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("file does not exist: %s", absSource)
		}
		realSource = absSource // Use original if eval fails
	}

	// Auto-detect target (home, etc, var, usr) based on path
	targetName, relPath := dotfiles.GetTargetForPath(absSource, paths.Home)

	// Target path in dotfiles repo: dotfiles/<layer>/<target>/<relPath>
	destPath := filepath.Join(paths.DotfilesDir, "dotfiles", layer, targetName, relPath)

	// Check if this is a cross-layer adopt (source is our own symlink into a different layer)
	if currentLayer, ok := layerFromRepoPath(existingLinkTarget, paths.DotfilesDir); ok {
		if currentLayer == layer {
			return fmt.Errorf("already managed in layer: %s", layer)
		}
		return promoteBetweenLayers(promoteArgs{
			absSource:    absSource,
			currentLink:  existingLinkTarget,
			currentLayer: currentLayer,
			newLayer:     layer,
			newDestPath:  destPath,
			targetName:   targetName,
			cfg:          cfg,
			copyOnly:     copyOnly,
			autoYes:      autoYes,
		})
	}

	// Check if already adopted
	if _, err := os.Stat(destPath); err == nil {
		// Check if source is already a symlink to destPath
		if linkTarget, err := os.Readlink(absSource); err == nil && linkTarget == destPath {
			return fmt.Errorf("already managed (symlinked to dotfiles)")
		}
		return fmt.Errorf("already exists in dotfiles: %s\n  Use 'dotgenie forget' first if you want to re-adopt", destPath)
	}

	// Show what will happen
	info, err := os.Stat(realSource)
	if err != nil {
		return err
	}

	if info.IsDir() {
		fmt.Printf("\nAdopting directory: %s\n", absSource)
	} else {
		fmt.Printf("\nAdopting file: %s\n", absSource)
	}

	// Show migration info if adopting from another tool
	if existingLinkTarget != "" {
		fmt.Printf("  Migrating from: %s (existing symlink)\n", existingLinkTarget)
		fmt.Printf("  Content from:   %s\n", realSource)
	}

	fmt.Printf("  Target: [%s] %s\n", targetName, relPath)
	fmt.Printf("  Destination: %s\n", destPath)

	if copyOnly {
		fmt.Println("  (copy only, no symlink)")
	}

	if dotfiles.TargetNeedsSudo(targetName) && !copyOnly {
		fmt.Println("  Note: System file - symlink creation will require sudo")
	}

	// Confirm unless -y flag
	if !autoYes {
		fmt.Print("Proceed? [y/N] ")
		reader := bufio.NewReader(os.Stdin)
		response, _ := reader.ReadString('\n')
		response = strings.TrimSpace(strings.ToLower(response))
		if response != "y" && response != "yes" {
			fmt.Println("Skipped")
			return nil
		}
	}

	// Create parent directory
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return fmt.Errorf("creating directory: %w", err)
	}

	// Copy or move the file/directory
	if info.IsDir() {
		if err := copyDir(realSource, destPath); err != nil {
			return fmt.Errorf("copying directory: %w", err)
		}
	} else {
		if err := copyFile(realSource, destPath); err != nil {
			return fmt.Errorf("copying file: %w", err)
		}
	}

	if copyOnly {
		fmt.Printf("Copied to %s\n", destPath)
		return nil
	}

	// Remove original and create symlink
	if dotfiles.TargetNeedsSudo(targetName) {
		// Need sudo to modify system files
		if err := removeAndLinkWithSudo(absSource, destPath); err != nil {
			return err
		}
	} else {
		if err := os.RemoveAll(absSource); err != nil {
			return fmt.Errorf("removing original: %w", err)
		}
		if err := os.Symlink(destPath, absSource); err != nil {
			return fmt.Errorf("creating symlink: %w", err)
		}
	}

	fmt.Printf("Adopted and linked: %s → %s\n", absSource, destPath)
	return nil
}

// layerFromRepoPath returns the layer name if the given absolute path is inside
// the dotfiles repo's dotfiles/ tree, along with true. Otherwise returns "", false.
// Handles paths like:
//   <repo>/dotfiles/common/home/.config/foo    -> "common"
//   <repo>/dotfiles/workstation/home/.config/x -> "workstation"
//   <repo>/dotfiles/hosts/xenon/home/.config/y -> "hosts/xenon"
func layerFromRepoPath(path, dotfilesDir string) (string, bool) {
	if path == "" {
		return "", false
	}
	prefix := filepath.Join(dotfilesDir, "dotfiles") + string(filepath.Separator)
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}
	rel := strings.TrimPrefix(path, prefix)
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) == 0 {
		return "", false
	}
	if parts[0] == "hosts" && len(parts) >= 2 {
		return filepath.Join("hosts", parts[1]), true
	}
	return parts[0], true
}

// layerSpecificity returns how specific a layer is on this machine. Higher wins.
// Returns -1 if the layer does not apply on this machine.
//
//	common       -> 0 (always applies)
//	<systemtype> -> 1 (applies if matches cfg.SystemType)
//	hosts/<host> -> 2 (applies if matches cfg.Hostname)
func layerSpecificity(layer string, cfg *config.Config) int {
	if layer == "common" {
		return 0
	}
	if strings.HasPrefix(layer, "hosts/") {
		if layer == filepath.Join("hosts", cfg.Hostname) {
			return 2
		}
		return -1
	}
	if layer == cfg.SystemType {
		return 1
	}
	return -1
}

type promoteArgs struct {
	absSource    string
	currentLink  string
	currentLayer string
	newLayer     string
	newDestPath  string
	targetName   string
	cfg          *config.Config
	copyOnly     bool
	autoYes      bool
}

// promoteBetweenLayers copies a managed file from one layer to another. The
// original stays in place. The symlink is updated only if the new layer has
// higher specificity on this machine (i.e. it wins).
func promoteBetweenLayers(a promoteArgs) error {
	// Refuse if the new destination already exists
	if _, err := os.Stat(a.newDestPath); err == nil {
		return fmt.Errorf("already exists in dotfiles: %s\n  Use 'dotgenie forget --scope %s' first if you want to re-adopt", a.newDestPath, scopeFromLayer(a.newLayer))
	}

	currentSpec := layerSpecificity(a.currentLayer, a.cfg)
	newSpec := layerSpecificity(a.newLayer, a.cfg)
	updateSymlink := newSpec > currentSpec

	info, err := os.Stat(a.currentLink)
	if err != nil {
		return fmt.Errorf("reading source: %w", err)
	}

	if info.IsDir() {
		fmt.Printf("\nAdopting directory: %s\n", a.absSource)
	} else {
		fmt.Printf("\nAdopting file: %s\n", a.absSource)
	}
	fmt.Printf("  Currently managed in: %s\n", a.currentLayer)
	fmt.Printf("  Copying to: %s\n", a.newLayer)
	fmt.Printf("  Source: %s\n", a.currentLink)
	fmt.Printf("  Destination: %s\n", a.newDestPath)
	fmt.Printf("  Original stays -- still applies where %s wins.\n", a.currentLayer)
	if updateSymlink {
		fmt.Printf("  Symlink will update to point at the %s version (wins on this machine).\n", a.newLayer)
	} else {
		fmt.Printf("  Symlink stays pointing at %s (still wins on this machine).\n", a.currentLayer)
		if newSpec >= 0 && newSpec < currentSpec {
			fmt.Printf("  To use the %s version here, run 'dotgenie forget --scope %s' on this path.\n", a.newLayer, scopeFromLayer(a.currentLayer))
		}
	}

	if !a.autoYes {
		fmt.Print("Proceed? [y/N] ")
		reader := bufio.NewReader(os.Stdin)
		response, _ := reader.ReadString('\n')
		response = strings.TrimSpace(strings.ToLower(response))
		if response != "y" && response != "yes" {
			fmt.Println("Skipped")
			return nil
		}
	}

	// Create parent directory and copy content
	if err := os.MkdirAll(filepath.Dir(a.newDestPath), 0755); err != nil {
		return fmt.Errorf("creating directory: %w", err)
	}
	if info.IsDir() {
		if err := copyDir(a.currentLink, a.newDestPath); err != nil {
			return fmt.Errorf("copying directory: %w", err)
		}
	} else {
		if err := copyFile(a.currentLink, a.newDestPath); err != nil {
			return fmt.Errorf("copying file: %w", err)
		}
	}

	// Update the symlink if the new layer wins
	if updateSymlink && !a.copyOnly {
		if dotfiles.TargetNeedsSudo(a.targetName) {
			if err := removeAndLinkWithSudo(a.absSource, a.newDestPath); err != nil {
				return err
			}
		} else {
			if err := os.RemoveAll(a.absSource); err != nil {
				return fmt.Errorf("removing old symlink: %w", err)
			}
			if err := os.Symlink(a.newDestPath, a.absSource); err != nil {
				return fmt.Errorf("creating new symlink: %w", err)
			}
		}
		fmt.Printf("Copied and re-linked: %s -> %s\n", a.absSource, a.newDestPath)
	} else {
		fmt.Printf("Copied to %s\n", a.newDestPath)
	}
	return nil
}

// scopeFromLayer converts an internal layer name back to a --scope flag value.
// "hosts/xenon" -> "host"; "workstation" -> "workstation"; "common" -> "common".
func scopeFromLayer(layer string) string {
	if strings.HasPrefix(layer, "hosts/") {
		return "host"
	}
	return layer
}

func removeAndLinkWithSudo(originalPath, destPath string) error {
	// Remove original with sudo
	rmCmd := execCommand("sudo", "rm", "-rf", originalPath)
	rmCmd.Stdout = os.Stdout
	rmCmd.Stderr = os.Stderr
	if err := rmCmd.Run(); err != nil {
		return fmt.Errorf("sudo rm failed: %w", err)
	}

	// Create symlink with sudo
	lnCmd := execCommand("sudo", "ln", "-s", destPath, originalPath)
	lnCmd.Stdout = os.Stdout
	lnCmd.Stderr = os.Stderr
	if err := lnCmd.Run(); err != nil {
		return fmt.Errorf("sudo ln failed: %w", err)
	}

	return nil
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}

	info, err := os.Stat(src)
	if err != nil {
		return err
	}

	return os.WriteFile(dst, data, info.Mode())
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		targetPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(targetPath, info.Mode())
		}

		return copyFile(path, targetPath)
	})
}

func gitCommitAdopted(dotfilesDir string) error {
	// Check if it's a git repo
	gitDir := filepath.Join(dotfilesDir, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		return nil
	}

	// Check for changes
	cmd := execCommand("git", "status", "--porcelain")
	cmd.Dir = dotfilesDir
	output, err := cmd.Output()
	if err != nil || len(output) == 0 {
		return nil
	}

	// Build commit message with filepaths
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	files := make([]string, 0, len(lines))
	for _, line := range lines {
		if len(line) > 3 {
			files = append(files, strings.TrimSpace(line[2:]))
		}
	}
	var msg strings.Builder
	msg.WriteString("Adopt dotfiles via dotgenie\n")
	for _, f := range files {
		msg.WriteString("\n- ")
		msg.WriteString(f)
	}

	// Add and commit
	addCmd := execCommand("git", "add", "-A")
	addCmd.Dir = dotfilesDir
	if err := addCmd.Run(); err != nil {
		return err
	}

	commitCmd := execCommand("git", "commit", "-m", msg.String())
	commitCmd.Dir = dotfilesDir
	return commitCmd.Run()
}
