package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/wonskur/upm/internal/utils"
)

type GlobalFlags struct {
	Force      bool
	DryRun     bool
	Verbose    bool
	Quiet      bool
	Yes        bool
	NoDeps     bool
	AsDeps     bool
	AsExplicit bool
}

var (
	Flags GlobalFlags

	sourceFlag      bool
	installFileFlag string

	rootCmd = &cobra.Command{
		Use:   "upm",
		Short: "upm is the universal package manager for humanix",
		Long: `upm is a fast, versatile package manager for humanix GNU/Linux.
It installs native humanix packages directly, builds local packages from source,
and runs imported Fedora, Debian, and Arch binaries inside bwrap sandboxes. AUR entries can be queried but are not installable until a verified isolated source-build flow is available.`,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			utils.InitLogger(Flags.Verbose, Flags.Quiet)
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if sourceFlag {
				return runSource(cmd, args)
			}
			if installFileFlag != "" {
				return runForeign(cmd, []string{installFileFlag})
			}
			return cmd.Help()
		},
	}
)

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	// Global persistent flags
	rootCmd.PersistentFlags().BoolVar(&Flags.Force, "force", false, "Force operation without safety checks")
	rootCmd.PersistentFlags().BoolVar(&Flags.DryRun, "dry-run", false, "Simulate actions without modifying system")
	rootCmd.PersistentFlags().BoolVarP(&Flags.Verbose, "verbose", "v", false, "Enable verbose debug output")
	rootCmd.PersistentFlags().BoolVarP(&Flags.Quiet, "quiet", "q", false, "Suppress output except errors")
	rootCmd.PersistentFlags().BoolVarP(&Flags.Yes, "yes", "y", false, "Assume 'yes' to all interactive prompts")
	rootCmd.PersistentFlags().BoolVar(&Flags.NoDeps, "no-deps", false, "Skip dependency resolution and installation")
	rootCmd.PersistentFlags().BoolVar(&Flags.AsDeps, "asdeps", false, "Install packages as non-explicit dependencies")
	rootCmd.PersistentFlags().BoolVar(&Flags.AsExplicit, "asexplicit", false, "Install packages as explicitly requested")

	// Root flags for shorthand syntax: upm -s and upm -i <path>
	rootCmd.Flags().BoolVarP(&sourceFlag, "source", "s", false, "Build and install package from source in current directory")
	rootCmd.Flags().StringVarP(&installFileFlag, "install-file", "i", "", "Install local package file directly")

	// Register subcommands
	rootCmd.AddCommand(syncCmd)
	rootCmd.AddCommand(installCmd)
	rootCmd.AddCommand(removeCmd)
	rootCmd.AddCommand(updateCmd)
	rootCmd.AddCommand(searchCmd)
	rootCmd.AddCommand(infoCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(filesCmd)
	rootCmd.AddCommand(ownerCmd)
	rootCmd.AddCommand(cleanCmd)
	rootCmd.AddCommand(rollbackCmd)
	rootCmd.AddCommand(sourceCmd)
	rootCmd.AddCommand(foreignCmd)
}

// FormatDryRun returns a dry-run prefix if dry-run flag is active.
func DryRunPrefix() string {
	if Flags.DryRun {
		return "[DRY-RUN] "
	}
	return ""
}

func checkErr(err error, format string, args ...any) error {
	if err != nil {
		return fmt.Errorf("%s: %w", fmt.Sprintf(format, args...), err)
	}
	return nil
}
