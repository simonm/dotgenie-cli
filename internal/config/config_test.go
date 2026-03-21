package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMergesConfigs(t *testing.T) {
	dir := t.TempDir()

	shared := []byte("repo_version: 1\nauto_pull_before_apply: true\nauto_commit_after_adopt: false\nauto_push_after_adopt: false\n")
	if err := os.WriteFile(filepath.Join(dir, "config.yml"), shared, 0644); err != nil {
		t.Fatal(err)
	}

	local := []byte("os: arch\nsystem_type: workstation\nhostname: testhost\n")
	if err := os.WriteFile(filepath.Join(dir, "config.local.yml"), local, 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(filepath.Join(dir, "config.yml"))
	if err != nil {
		t.Fatal(err)
	}

	if cfg.RepoVersion != 1 {
		t.Errorf("RepoVersion = %d, want 1", cfg.RepoVersion)
	}
	if cfg.OS != "arch" {
		t.Errorf("OS = %q, want %q", cfg.OS, "arch")
	}
	if cfg.Hostname != "testhost" {
		t.Errorf("Hostname = %q, want %q", cfg.Hostname, "testhost")
	}
	if !cfg.AutoPullBeforeApply {
		t.Error("AutoPullBeforeApply should be true")
	}
	if cfg.AutoCommitAfterAdopt {
		t.Error("AutoCommitAfterAdopt should be false")
	}
}

func TestLoadAutoGeneratesLocal(t *testing.T) {
	dir := t.TempDir()

	shared := []byte("repo_version: 1\nauto_pull_before_apply: true\nauto_commit_after_adopt: true\nauto_push_after_adopt: false\n")
	if err := os.WriteFile(filepath.Join(dir, "config.yml"), shared, 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(filepath.Join(dir, "config.yml"))
	if err != nil {
		t.Fatal(err)
	}

	if cfg.OS == "" {
		t.Error("OS should be auto-detected, got empty")
	}
	if cfg.Hostname == "" {
		t.Error("Hostname should be auto-detected, got empty")
	}

	localPath := filepath.Join(dir, "config.local.yml")
	if _, err := os.Stat(localPath); os.IsNotExist(err) {
		t.Error("config.local.yml should have been auto-generated")
	}
}

func TestLoadLegacyConfig(t *testing.T) {
	dir := t.TempDir()

	legacy := []byte("os: ubuntu\nsystem_type: server\nhostname: oldhost\nauto_pull_before_apply: true\nauto_commit_after_adopt: true\nauto_push_after_adopt: false\n")
	if err := os.WriteFile(filepath.Join(dir, "config.yml"), legacy, 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(filepath.Join(dir, "config.yml"))
	if err != nil {
		t.Fatal(err)
	}

	if cfg.OS != "ubuntu" {
		t.Errorf("OS = %q, want %q", cfg.OS, "ubuntu")
	}
	if cfg.RepoVersion != 0 {
		t.Errorf("RepoVersion = %d, want 0 (legacy)", cfg.RepoVersion)
	}
}

func TestSaveSharedConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")

	cfg := &Config{
		RepoVersion:          1,
		AutoPullBeforeApply:  true,
		AutoCommitAfterAdopt: true,
		AutoPushAfterAdopt:   false,
		OS:                   "arch",
		SystemType:           "workstation",
		Hostname:             "myhost",
	}

	if err := cfg.SaveShared(path); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(path)
	content := string(data)

	if !containsStr(content, "repo_version") {
		t.Error("shared config should contain repo_version")
	}
	if containsStr(content, "hostname") {
		t.Error("shared config should NOT contain hostname")
	}
	if containsStr(content, "system_type") {
		t.Error("shared config should NOT contain system_type")
	}
	if containsStr(content, "os:") {
		t.Error("shared config should NOT contain os")
	}
}

func TestSaveLocalConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.local.yml")

	cfg := &Config{
		RepoVersion:          1,
		AutoPullBeforeApply:  true,
		OS:                   "arch",
		SystemType:           "workstation",
		Hostname:             "myhost",
	}

	if err := cfg.SaveLocal(path); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(path)
	content := string(data)

	if !containsStr(content, "hostname") {
		t.Error("local config should contain hostname")
	}
	if containsStr(content, "repo_version") {
		t.Error("local config should NOT contain repo_version")
	}
	if containsStr(content, "auto_pull") {
		t.Error("local config should NOT contain auto_pull")
	}
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
