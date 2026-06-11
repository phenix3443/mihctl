package cli

import (
	"github.com/phenix3443/mihomo-companion/internal/configgen"
	"github.com/phenix3443/mihomo-companion/internal/mihomo"
	"github.com/spf13/cobra"
)

var runProvidersUpdate = func(env *mihomo.Env) error {
	return env.UpdateProvidersRemote()
}

var runProvidersRefreshOfficialSupport = func(env *mihomo.Env) error {
	return env.RefreshOfficialSupport()
}

var runProvidersProbe = func(env *mihomo.Env, scope configgen.ProbeScope) error {
	return env.ProbeProviders(scope)
}

func newProvidersCmd() *cobra.Command {
	var probeProviders []string
	var probeGroups []string

	cmd := &cobra.Command{
		Use:   "providers",
		Short: "Manage proxy providers",
	}

	syncCmd := &cobra.Command{
		Use:   "sync",
		Short: "Copy repository providers into the detected live config directory",
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := loadEnv()
			if err != nil {
				return err
			}
			return env.SyncProvidersToLive()
		},
	}
	updateCmd := &cobra.Command{
		Use:   "update",
		Short: "Fetch provider URLs into repository providers/",
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := loadEnv()
			if err != nil {
				return err
			}
			return runProvidersUpdate(env)
		},
	}
	refreshOfficialSupportCmd := &cobra.Command{
		Use:   "refresh-official-support",
		Short: "Write the config-driven official service support catalog to the local state file",
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := loadEnv()
			if err != nil {
				return err
			}
			return runProvidersRefreshOfficialSupport(env)
		},
	}
	probeCmd := &cobra.Command{
		Use:   "probe",
		Short: "Run local service probes for repository providers",
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := loadEnv()
			if err != nil {
				return err
			}
			return runProvidersProbe(env, configgen.ProbeScope{
				Providers: probeProviders,
				Services:  probeGroups,
				Mode:      configgen.ProbeModeService,
			})
		},
	}
	probeCmd.Flags().StringSliceVar(&probeProviders, "provider", nil, "limit probe to one or more providers")
	probeCmd.Flags().StringSliceVar(&probeGroups, "group", nil, "limit probe to one or more service groups")

	cmd.AddCommand(
		syncCmd,
		updateCmd,
		refreshOfficialSupportCmd,
		probeCmd,
	)

	return cmd
}
