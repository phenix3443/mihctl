package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func newInstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install Mihomo binary, config, geodata, and service integration",
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := loadEnv()
			if err != nil {
				return err
			}
			return env.Install()
		},
	}
	return cmd
}

func newInstallUICmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install-ui",
		Short: "Install or update the metacubexd web UI",
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := loadEnv()
			if err != nil {
				return err
			}
			if env.OS != "darwin" {
				if err := env.RequireRoot("install-ui"); err != nil {
					return err
				}
			}
			if err := env.SetupUI(); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "[OK]   UI installed at %s/ui\n", env.ConfigDir)
			env.PrintAccessURLs()
			return nil
		},
	}
	return cmd
}

func newDetectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "detect",
		Short: "Show which Mihomo client/config target is active",
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := loadEnv()
			if err != nil {
				return err
			}
			return env.Detect()
		},
	}
	return cmd
}

func newUninstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove Mihomo service integration and binary while keeping config",
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := loadEnv()
			if err != nil {
				return err
			}
			return env.Uninstall()
		},
	}
	return cmd
}

func newUpdateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update the Mihomo binary and restart when running",
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := loadEnv()
			if err != nil {
				return err
			}
			return env.Update()
		},
	}
	return cmd
}

func newUpdateGeodataCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update-geodata",
		Short: "Re-download geosite.dat, geoip.dat, and Country.mmdb",
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := loadEnv()
			if err != nil {
				return err
			}
			return env.UpdateGeodata()
		},
	}
	return cmd
}
