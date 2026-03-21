package cli

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// checkCommand returns true if the named command is available in PATH.
func checkCommand(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// installCommands returns the shell command parts to install a package on the
// given OS, or nil if the OS is not recognized.
func installCommands(detectedOS, pkg string) []string {
	switch detectedOS {
	case "macos":
		return []string{"brew", "install", pkg}
	case "arch":
		return []string{"sudo", "pacman", "-S", "--noconfirm", pkg}
	case "ubuntu", "debian":
		return []string{"sudo", "apt-get", "install", "-y", pkg}
	default:
		return nil
	}
}

// ensureDep checks whether a command is available. If not, it tells the user
// how to install it and offers to do so automatically when the OS is recognized.
// Returns an error only if the dep is missing AND the user declines / install fails.
func ensureDep(dep, detectedOS string) error {
	if checkCommand(dep) {
		return nil
	}

	cmds := installCommands(detectedOS, dep)
	if cmds == nil {
		return fmt.Errorf("%s is required but not installed. Please install it manually and try again", dep)
	}

	fmt.Printf("%s is required but not installed.\n", dep)
	fmt.Printf("Install with: %s\n", strings.Join(cmds, " "))
	fmt.Print("Install now? [Y/n] ")

	reader := bufio.NewReader(os.Stdin)
	response, _ := reader.ReadString('\n')
	response = strings.TrimSpace(strings.ToLower(response))

	if response != "" && response != "y" && response != "yes" {
		return fmt.Errorf("%s is required but not installed", dep)
	}

	fmt.Printf("Installing %s...\n", dep)
	installCmd := exec.Command(cmds[0], cmds[1:]...)
	installCmd.Stdout = os.Stdout
	installCmd.Stderr = os.Stderr
	installCmd.Stdin = os.Stdin
	if err := installCmd.Run(); err != nil {
		return fmt.Errorf("failed to install %s: %w", dep, err)
	}

	// Verify it worked
	if !checkCommand(dep) {
		return fmt.Errorf("%s was installed but still not found in PATH", dep)
	}

	fmt.Printf("Successfully installed %s\n", dep)
	return nil
}

// ensureDeps checks multiple dependencies. Stops on the first failure.
func ensureDeps(deps []string, detectedOS string) error {
	for _, dep := range deps {
		if err := ensureDep(dep, detectedOS); err != nil {
			return err
		}
	}
	return nil
}
