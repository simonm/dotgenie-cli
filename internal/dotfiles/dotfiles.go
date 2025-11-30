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

// Manager handles dotfile operations
type Manager struct {
	DotfilesDir string
	HomeDir     string
	SystemType  string
	Hostname    string
	BackupSuffix string
}

// FileAction represents what to do with a file
type FileAction struct {
	RelPath    string
	SourcePath string
	TargetPath string
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

// CollectFiles gathers all dotfiles from the layers and determines the source for each
func (m *Manager) CollectFiles() (map[string]string, error) {
	// Map of relative path -> source path (later layers override earlier)
	files := make(map[string]string)

	// Layer 1: common (always)
	commonDir := filepath.Join(m.DotfilesDir, "dotfiles", "common")
	if err := m.walkLayer(commonDir, files); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("walking common: %w", err)
	}

	// Layer 2: system type (if workstation)
	if m.SystemType == "workstation" {
		workstationDir := filepath.Join(m.DotfilesDir, "dotfiles", "workstation")
		if err := m.walkLayer(workstationDir, files); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("walking workstation: %w", err)
		}
	}

	// Layer 3: host-specific (always, if exists)
	hostDir := filepath.Join(m.DotfilesDir, "dotfiles", "hosts", m.Hostname)
	if err := m.walkLayer(hostDir, files); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("walking host %s: %w", m.Hostname, err)
	}

	return files, nil
}

func (m *Manager) walkLayer(layerDir string, files map[string]string) error {
	return filepath.WalkDir(layerDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(layerDir, path)
		if err != nil {
			return err
		}

		files[relPath] = path
		return nil
	})
}

// Apply creates symlinks for all collected files
func (m *Manager) Apply(dryRun bool) ([]FileAction, error) {
	files, err := m.CollectFiles()
	if err != nil {
		return nil, err
	}

	// Process files in parallel
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		actions []FileAction
	)

	// Use a semaphore to limit concurrency
	sem := make(chan struct{}, 20)

	for relPath, sourcePath := range files {
		wg.Add(1)
		go func(relPath, sourcePath string) {
			defer wg.Done()
			sem <- struct{}{}        // acquire
			defer func() { <-sem }() // release

			action := m.processFile(relPath, sourcePath, dryRun)

			mu.Lock()
			actions = append(actions, action)
			mu.Unlock()
		}(relPath, sourcePath)
	}

	wg.Wait()
	return actions, nil
}

func (m *Manager) processFile(relPath, sourcePath string, dryRun bool) FileAction {
	targetPath := filepath.Join(m.HomeDir, relPath)

	action := FileAction{
		RelPath:    relPath,
		SourcePath: sourcePath,
		TargetPath: targetPath,
	}

	// Check current state of target
	targetInfo, err := os.Lstat(targetPath)

	if os.IsNotExist(err) {
		// Target doesn't exist - create symlink
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
		// It's a symlink - check if it points to the right place
		linkTarget, err := os.Readlink(targetPath)
		if err != nil {
			action.Action = "error"
			action.Error = err
			return action
		}

		if linkTarget == sourcePath {
			// Already correct
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
	// Ensure parent directory exists
	parentDir := filepath.Dir(target)
	if err := os.MkdirAll(parentDir, 0755); err != nil {
		return fmt.Errorf("creating parent dir: %w", err)
	}

	return os.Symlink(source, target)
}

// Status returns the current state of managed dotfiles
func (m *Manager) Status() ([]FileAction, error) {
	files, err := m.CollectFiles()
	if err != nil {
		return nil, err
	}

	var actions []FileAction
	for relPath, sourcePath := range files {
		targetPath := filepath.Join(m.HomeDir, relPath)
		action := FileAction{
			RelPath:    relPath,
			SourcePath: sourcePath,
			TargetPath: targetPath,
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
		switch a.Action {
		case "link":
			linked++
			if verbose {
				fmt.Printf("  ✓ %s\n", a.RelPath)
			}
		case "skip", "ok":
			skipped++
		case "backup_and_link":
			backed++
			if verbose {
				fmt.Printf("  ✓ %s (backed up existing)\n", a.RelPath)
			}
		case "error":
			errors++
			fmt.Printf("  ✗ %s: %v\n", a.RelPath, a.Error)
		case "missing":
			if verbose {
				fmt.Printf("  ○ %s (not linked)\n", a.RelPath)
			}
		case "wrong_link":
			if verbose {
				fmt.Printf("  ! %s (wrong symlink)\n", a.RelPath)
			}
		case "not_symlink":
			if verbose {
				fmt.Printf("  ! %s (not a symlink)\n", a.RelPath)
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
