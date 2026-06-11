package cli

import "github.com/spf13/cobra"

func Execute() error {
	return newRootCmd().Execute()
}

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mihctl",
		Short: "Companion CLI for managing Mihomo on macOS and Linux",
	}

	cmd.AddCommand(
		newConfigCmd(),
		newDetectCmd(),
		newInstallCmd(),
		newInstallUICmd(),
		newProvidersCmd(),
		newRulesCmd(),
		newServiceCmd(),
		newUninstallCmd(),
		newUpdateCmd(),
		newUpdateGeodataCmd(),
	)

	return cmd
}
