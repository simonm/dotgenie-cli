package dotfiles

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Target represents a destination root (home, etc, var, etc.)
type Target struct {
	Name     string // "home", "etc", "var", etc.
	RootPath string // Actual path: $HOME, /etc, /var, etc.
	NeedSudo bool   // Whether this target requires root privileges
}

// Common targets
var (
	HomeTarget = Target{Name: "home", RootPath: "", NeedSudo: false} // RootPath set dynamically
	EtcTarget  = Target{Name: "etc", RootPath: "/etc", NeedSudo: true}
	VarTarget  = Target{Name: "var", RootPath: "/var", NeedSudo: true}
	UsrTarget  = Target{Name: "usr", RootPath: "/usr", NeedSudo: true}
)

// AllTargets returns all known targets
func AllTargets(homeDir string) []Target {
	return []Target{
		{Name: "home", RootPath: homeDir, NeedSudo: false},
		{Name: "etc", RootPath: "/etc", NeedSudo: true},
		{Name: "var", RootPath: "/var", NeedSudo: true},
		{Name: "usr", RootPath: "/usr", NeedSudo: true},
	}
}

// Manager handles dotfile operations
type Manager struct {
	DotfilesDir  string
	HomeDir      string
	SystemType   string
	Hostname     string
	BackupSuffix string
}

// FileAction represents what to do with a file
type FileAction struct {
	RelPath    string
	SourcePath string
	TargetPath string
	Target     string // "home", "etc", etc.
	Action     string // "link", "skip", "backup_and_link"
	Error      error
}

// NewManager creates a new dotfiles manager
func NewManager(dotfilesDir, homeDir, systemType, hostname string) *Manager {
	return &Manager{
		DotfilesDir:  dotfilesDir,
		HomeDir:      homeDir,
		SystemType:   systemType,
		Hostname:     hostname,
		BackupSuffix: fmt.Sprintf(".dotgenie-bak.%s", time.Now().Format("2006-01-02")),
	}
}

// CollectFiles gathers all dotfiles from the layers for specified targets
// Returns map of "target:relPath" -> sourcePath
func (m *Manager) CollectFiles(targets []Target) (map[string]string, error) {
	files := make(map[string]string)

	// Layer 1: common (always)
	commonDir := filepath.Join(m.DotfilesDir, "dotfiles", "common")
	if err := m.walkLayerTargets(commonDir, targets, files); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("walking common: %w", err)
	}

	// Layer 2: system type (if workstation)
	if m.SystemType == "workstation" {
		workstationDir := filepath.Join(m.DotfilesDir, "dotfiles", "workstation")
		if err := m.walkLayerTargets(workstationDir, targets, files); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("walking workstation: %w", err)
		}
	}

	// Layer 3: host-specific (always, if exists)
	hostDir := filepath.Join(m.DotfilesDir, "dotfiles", "hosts", m.Hostname)
	if err := m.walkLayerTargets(hostDir, targets, files); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("walking host %s: %w", m.Hostname, err)
	}

	return files, nil
}

// walkLayerTargets walks a layer directory looking for target subdirs (home/, etc/, etc.)
func (m *Manager) walkLayerTargets(layerDir string, targets []Target, files map[string]string) error {
	for _, target := range targets {
		targetDir := filepath.Join(layerDir, target.Name)
		if _, err := os.Stat(targetDir); os.IsNotExist(err) {
			continue
		}

		err := filepath.WalkDir(targetDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}

			relPath, err := filepath.Rel(targetDir, path)
			if err != nil {
				return err
			}

			// Key includes target name to handle same relPath in different targets
			key := target.Name + ":" + relPath
			files[key] = path
			return nil
		})

		if err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// Apply creates symlinks for all collected files
func (m *Manager) Apply(targets []Target, dryRun bool) ([]FileAction, error) {
	files, err := m.CollectFiles(targets)
	if err != nil {
		return nil, err
	}

	// Build target lookup
	targetMap := make(map[string]Target)
	for _, t := range targets {
		targetMap[t.Name] = t
	}

	// Process files in parallel
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		actions []FileAction
	)

	sem := make(chan struct{}, 20)

	for key, sourcePath := range files {
		wg.Add(1)
		go func(key, sourcePath string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			// Parse key: "target:relPath"
			parts := strings.SplitN(key, ":", 2)
			targetName := parts[0]
			relPath := parts[1]

			target := targetMap[targetName]
			action := m.processFile(target, relPath, sourcePath, dryRun)

			mu.Lock()
			actions = append(actions, action)
			mu.Unlock()
		}(key, sourcePath)
	}

	wg.Wait()
	return actions, nil
}

func (m *Manager) processFile(target Target, relPath, sourcePath string, dryRun bool) FileAction {
	targetPath := filepath.Join(target.RootPath, relPath)

	action := FileAction{
		RelPath:    relPath,
		SourcePath: sourcePath,
		TargetPath: targetPath,
		Target:     target.Name,
	}

	// SAFETY: Never create symlinks inside the dotfiles directory
	// This can happen if targetPath is already a symlink pointing into dotfiles
	absTarget, _ := filepath.EvalSymlinks(filepath.Dir(targetPath))
	if absTarget != "" && strings.HasPrefix(absTarget, m.DotfilesDir) {
		action.Action = "skip"
		action.Error = fmt.Errorf("target resolves inside dotfiles dir (circular symlink?)")
		return action
	}

	// Also check the target path itself without following symlinks
	absTargetPath, _ := filepath.Abs(targetPath)
	if strings.HasPrefix(absTargetPath, m.DotfilesDir) {
		action.Action = "skip"
		action.Error = fmt.Errorf("target is inside dotfiles dir")
		return action
	}

	// Check current state of target
	targetInfo, err := os.Lstat(targetPath)

	if os.IsNotExist(err) {
		action.Action = "link"
		if !dryRun {
			if err := m.createSymlink(sourcePath, targetPath); err != nil {
				action.Error = err
			}
		}
		return action
	}

	if err != nil {
		action.Action = "error"
		action.Error = err
		return action
	}

	// Target exists
	if targetInfo.Mode()&os.ModeSymlink != 0 {
		linkTarget, err := os.Readlink(targetPath)
		if err != nil {
			action.Action = "error"
			action.Error = err
			return action
		}

		if linkTarget == sourcePath {
			action.Action = "skip"
			return action
		}

		// Wrong symlink - replace it
		action.Action = "link"
		if !dryRun {
			os.Remove(targetPath)
			if err := m.createSymlink(sourcePath, targetPath); err != nil {
				action.Error = err
			}
		}
		return action
	}

	// It's a real file - backup and replace
	action.Action = "backup_and_link"
	if !dryRun {
		backupPath := targetPath + m.BackupSuffix
		if err := os.Rename(targetPath, backupPath); err != nil {
			action.Error = fmt.Errorf("backup failed: %w", err)
			return action
		}
		if err := m.createSymlink(sourcePath, targetPath); err != nil {
			action.Error = err
		}
	}
	return action
}

func (m *Manager) createSymlink(source, target string) error {
	parentDir := filepath.Dir(target)
	if err := os.MkdirAll(parentDir, 0755); err != nil {
		return fmt.Errorf("creating parent dir: %w", err)
	}
	return os.Symlink(source, target)
}

// Status returns the current state of managed dotfiles
func (m *Manager) Status(targets []Target) ([]FileAction, error) {
	files, err := m.CollectFiles(targets)
	if err != nil {
		return nil, err
	}

	// Build target lookup
	targetMap := make(map[string]Target)
	for _, t := range targets {
		targetMap[t.Name] = t
	}

	var actions []FileAction
	for key, sourcePath := range files {
		parts := strings.SplitN(key, ":", 2)
		targetName := parts[0]
		relPath := parts[1]

		target := targetMap[targetName]
		targetPath := filepath.Join(target.RootPath, relPath)

		action := FileAction{
			RelPath:    relPath,
			SourcePath: sourcePath,
			TargetPath: targetPath,
			Target:     targetName,
		}

		targetInfo, err := os.Lstat(targetPath)
		if os.IsNotExist(err) {
			action.Action = "missing"
			actions = append(actions, action)
			continue
		}
		if err != nil {
			action.Action = "error"
			action.Error = err
			actions = append(actions, action)
			continue
		}

		if targetInfo.Mode()&os.ModeSymlink != 0 {
			linkTarget, _ := os.Readlink(targetPath)
			if linkTarget == sourcePath {
				action.Action = "ok"
			} else {
				action.Action = "wrong_link"
			}
		} else {
			action.Action = "not_symlink"
		}
		actions = append(actions, action)
	}

	return actions, nil
}

// PrintActions prints a summary of actions
func PrintActions(actions []FileAction, verbose bool) {
	var linked, skipped, backed, errors int

	for _, a := range actions {
		prefix := ""
		if a.Target != "home" {
			prefix = fmt.Sprintf("[%s] ", a.Target)
		}

		switch a.Action {
		case "link":
			linked++
			if verbose {
				fmt.Printf("  ✓ %s%s\n", prefix, a.RelPath)
			}
		case "skip", "ok":
			skipped++
		case "backup_and_link":
			backed++
			if verbose {
				fmt.Printf("  ✓ %s%s (backed up existing)\n", prefix, a.RelPath)
			}
		case "error":
			errors++
			fmt.Printf("  ✗ %s%s: %v\n", prefix, a.RelPath, a.Error)
		case "missing":
			if verbose {
				fmt.Printf("  ○ %s%s (not linked)\n", prefix, a.RelPath)
			}
		case "wrong_link":
			if verbose {
				fmt.Printf("  ! %s%s (wrong symlink)\n", prefix, a.RelPath)
			}
		case "not_symlink":
			if verbose {
				fmt.Printf("  ! %s%s (not a symlink)\n", prefix, a.RelPath)
			}
		}
	}

	fmt.Printf("\nTotal: %d files\n", len(actions))
	if linked > 0 {
		fmt.Printf("  Linked: %d\n", linked)
	}
	if backed > 0 {
		fmt.Printf("  Backed up & linked: %d\n", backed)
	}
	if skipped > 0 {
		fmt.Printf("  Already correct: %d\n", skipped)
	}
	if errors > 0 {
		fmt.Printf("  Errors: %d\n", errors)
	}
}

// GetLayerForPath determines which layer a path should go to
func GetLayerForPath(scope string, hostname string) string {
	switch strings.ToLower(scope) {
	case "common":
		return "common"
	case "workstation":
		return "workstation"
	case "host":
		return filepath.Join("hosts", hostname)
	default:
		return "common"
	}
}

// GetTargetForPath determines the target (home, etc, var) based on a filesystem path
func GetTargetForPath(path, homeDir string) (targetName string, relPath string) {
	absPath, _ := filepath.Abs(path)

	// Check if it's under home
	if strings.HasPrefix(absPath, homeDir) {
		rel, _ := filepath.Rel(homeDir, absPath)
		return "home", rel
	}

	// Check system paths
	systemPaths := []struct {
		prefix string
		target string
	}{
		{"/etc/", "etc"},
		{"/var/", "var"},
		{"/usr/", "usr"},
	}

	for _, sp := range systemPaths {
		if strings.HasPrefix(absPath, sp.prefix) {
			rel := strings.TrimPrefix(absPath, sp.prefix[:len(sp.prefix)-1])
			return sp.target, strings.TrimPrefix(rel, "/")
		}
	}

	// Default to home if we can't determine
	return "home", filepath.Base(absPath)
}

// TargetNeedsSudo returns true if the target requires root privileges
func TargetNeedsSudo(targetName string) bool {
	switch targetName {
	case "etc", "var", "usr":
		return true
	default:
		return false
	}
}
