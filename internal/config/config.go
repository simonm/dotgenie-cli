package config

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config represents the dotgenie configuration
type Config struct {
	RepoVersion int    `yaml:"repo_version,omitempty"`
	OS          string `yaml:"os"`
	SystemType  string `yaml:"system_type"`
	Hostname    string `yaml:"hostname"`

	// Sync settings
	AutoPullBeforeApply  bool `yaml:"auto_pull_before_apply"`
	AutoCommitAfterAdopt bool `yaml:"auto_commit_after_adopt"`
	AutoPushAfterAdopt   bool `yaml:"auto_push_after_adopt"`
	UpdateCheckDays      int  `yaml:"update_check_days,omitempty"`

	// Local-only (not serialized to shared config)
	LastUpdateCheck string `yaml:"last_update_check,omitempty"`
}

// SharedConfig holds fields that belong in the shared config.yml (committed to git)
type SharedConfig struct {
	RepoVersion          int  `yaml:"repo_version,omitempty"`
	AutoPullBeforeApply  bool `yaml:"auto_pull_before_apply"`
	AutoCommitAfterAdopt bool `yaml:"auto_commit_after_adopt"`
	AutoPushAfterAdopt   bool `yaml:"auto_push_after_adopt"`
	UpdateCheckDays      int  `yaml:"update_check_days,omitempty"`
}

// LocalConfig holds fields that belong in config.local.yml (machine-specific, gitignored)
type LocalConfig struct {
	OS               string `yaml:"os"`
	SystemType       string `yaml:"system_type"`
	Hostname         string `yaml:"hostname"`
	LastUpdateCheck  string `yaml:"last_update_check,omitempty"`
}

// Paths holds the important directories
type Paths struct {
	Home        string
	DotfilesDir string
	ConfigFile  string
}

// DefaultPaths returns the default paths
func DefaultPaths() Paths {
	home, _ := os.UserHomeDir()
	dotfilesDir := filepath.Join(home, ".dotfiles")

	return Paths{
		Home:        home,
		DotfilesDir: dotfilesDir,
		ConfigFile:  filepath.Join(dotfilesDir, "config.yml"),
	}
}

// Load reads the config file, then overlays config.local.yml from the same directory.
// If config.local.yml is missing and OS is empty (new-style repo), auto-detect and create it.
// Legacy configs (all fields in one file) still work.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	// Try to load config.local.yml from the same directory
	dir := filepath.Dir(path)
	localPath := filepath.Join(dir, "config.local.yml")

	localData, localErr := os.ReadFile(localPath)
	if localErr == nil {
		// Overlay local fields onto cfg
		var local LocalConfig
		if err := yaml.Unmarshal(localData, &local); err != nil {
			return nil, fmt.Errorf("parsing config.local.yml: %w", err)
		}
		if local.OS != "" {
			cfg.OS = local.OS
		}
		if local.SystemType != "" {
			cfg.SystemType = local.SystemType
		}
		if local.Hostname != "" {
			cfg.Hostname = local.Hostname
		}
		cfg.LastUpdateCheck = local.LastUpdateCheck
	} else if os.IsNotExist(localErr) && cfg.OS == "" {
		// New-style repo without a local config -- auto-detect and create it
		cfg.OS = DetectOS()
		cfg.SystemType = DetectSystemType()
		cfg.Hostname = DetectHostname()

		fmt.Printf("Detected: %s / %s / %s\n", cfg.OS, cfg.SystemType, cfg.Hostname)

		if err := cfg.SaveLocal(localPath); err != nil {
			return nil, fmt.Errorf("auto-generating config.local.yml: %w", err)
		}
	}

	// Apply defaults for legacy configs (RepoVersion 0 with all bools false)
	if cfg.RepoVersion == 0 && !cfg.AutoPullBeforeApply && !cfg.AutoCommitAfterAdopt {
		cfg.AutoPullBeforeApply = true
		cfg.AutoCommitAfterAdopt = true
	}

	return &cfg, nil
}

// Save writes the full config file (backward compat)
func (c *Config) Save(path string) error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// SaveShared writes only the shared config fields to the given path
func (c *Config) SaveShared(path string) error {
	shared := SharedConfig{
		RepoVersion:          c.RepoVersion,
		AutoPullBeforeApply:  c.AutoPullBeforeApply,
		AutoCommitAfterAdopt: c.AutoCommitAfterAdopt,
		AutoPushAfterAdopt:   c.AutoPushAfterAdopt,
		UpdateCheckDays:      c.UpdateCheckDays,
	}
	data, err := yaml.Marshal(&shared)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// SaveLocal writes only the local config fields to the given path
func (c *Config) SaveLocal(path string) error {
	local := LocalConfig{
		OS:              c.OS,
		SystemType:      c.SystemType,
		Hostname:        c.Hostname,
		LastUpdateCheck: c.LastUpdateCheck,
	}
	data, err := yaml.Marshal(&local)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// DetectOS detects the operating system
func DetectOS() string {
	if runtime.GOOS == "darwin" {
		return "macos"
	}

	// Read /etc/os-release for Linux
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return "unknown"
	}

	content := string(data)
	lines := strings.Split(content, "\n")

	var id, idLike string
	for _, line := range lines {
		if strings.HasPrefix(line, "ID=") {
			id = strings.Trim(strings.TrimPrefix(line, "ID="), "\"")
		}
		if strings.HasPrefix(line, "ID_LIKE=") {
			idLike = strings.Trim(strings.TrimPrefix(line, "ID_LIKE="), "\"")
		}
	}

	// Match on ID first
	switch id {
	case "arch", "archlinux", "endeavouros", "manjaro":
		return "arch"
	case "ubuntu":
		return "ubuntu"
	case "debian":
		return "debian"
	}

	// Fall back to ID_LIKE for derivatives (e.g. Proxmox has ID=pve, ID_LIKE=debian)
	for _, like := range strings.Fields(idLike) {
		switch like {
		case "arch":
			return "arch"
		case "ubuntu":
			return "ubuntu"
		case "debian":
			return "debian"
		}
	}

	if id != "" {
		return id
	}
	return "unknown"
}

// DetectSystemType tries to auto-detect the system type
func DetectSystemType() string {
	// Check for display (GUI)
	if os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != "" {
		return "workstation"
	}

	// Check if in container
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return "container"
	}

	// Check for graphical target on systemd
	cmd := exec.Command("systemctl", "is-active", "--quiet", "graphical.target")
	if err := cmd.Run(); err == nil {
		return "workstation"
	}

	return "server"
}

// DetectHostname returns the hostname
func DetectHostname() string {
	hostname, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return hostname
}

// NewFromDetection creates a new config from auto-detection
func NewFromDetection() *Config {
	return &Config{
		OS:                   DetectOS(),
		SystemType:           DetectSystemType(),
		Hostname:             DetectHostname(),
		AutoPullBeforeApply:  true,
		AutoCommitAfterAdopt: true,
		AutoPushAfterAdopt:   false,
	}
}

// Print prints the config
func (c *Config) Print() {
	fmt.Printf("OS:          %s\n", c.OS)
	fmt.Printf("System type: %s\n", c.SystemType)
	fmt.Printf("Hostname:    %s\n", c.Hostname)
}
