package cli

import (
	"testing"
)

func TestCheckCommand(t *testing.T) {
	// "ls" should exist on all unix systems
	if !checkCommand("ls") {
		t.Error("expected 'ls' to be found")
	}

	// A nonsense binary should not exist
	if checkCommand("definitely-not-a-real-binary-xyz123") {
		t.Error("expected fake binary to not be found")
	}
}

func TestInstallCommands(t *testing.T) {
	tests := []struct {
		os      string
		pkg     string
		wantNil bool
	}{
		{"macos", "git", false},
		{"arch", "git", false},
		{"ubuntu", "git", false},
		{"debian", "ansible", false},
		{"unknown", "git", true},
	}

	for _, tt := range tests {
		cmds := installCommands(tt.os, tt.pkg)
		if tt.wantNil && cmds != nil {
			t.Errorf("installCommands(%q, %q) = %v, want nil", tt.os, tt.pkg, cmds)
		}
		if !tt.wantNil && cmds == nil {
			t.Errorf("installCommands(%q, %q) = nil, want non-nil", tt.os, tt.pkg)
		}
	}
}
