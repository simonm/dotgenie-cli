package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	version   = "dev"
	commit    = "none"
	buildDate = "unknown"

	cfgFile     string
	dotfilesDir string
)

func SetVersion(v, c, d string) {
	version = v
	commit = c
	buildDate = d
}

var rootCmd = &cobra.Command{
	Use:   "dotgenie",
	Short: "A fast, simple dotfiles manager",
	Long: `dotgenie is a fast, simple dotfiles manager that helps you:
  - Sync dotfiles across machines
  - Install packages via system package managers and mise
  - Manage host-specific configurations

Get started:
  dotgenie init https://github.com/you/dotfiles
  dotgenie apply`,
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: ~/.dotfiles/config.yml)")
	rootCmd.PersistentFlags().StringVar(&dotfilesDir, "dotfiles", "", "dotfiles directory (default: ~/.dotfiles)")

	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(newCmd)
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(applyCmd)
	rootCmd.AddCommand(adoptCmd)
	rootCmd.AddCommand(forgetCmd)
	rootCmd.AddCommand(syncCmd)
	rootCmd.AddCommand(statusCmd)
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("dotgenie %s\n", version)
		fmt.Printf("  commit: %s\n", commit)
		fmt.Printf("  built:  %s\n", buildDate)
	},
}
