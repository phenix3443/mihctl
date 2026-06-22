package cli

import "github.com/spf13/cobra"

var runRulesSync = func(env rulesSyncEnv) error {
	return env.SyncRules()
}

type rulesSyncEnv interface {
	SyncRules() error
}

func newRulesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rules",
		Short: "Manage rule providers",
	}

	cmd.AddCommand(newRulesSyncCmd("sync", "Fetch remote rule providers into the live ruleset directory"))
	cmd.AddCommand(newRulesSyncCmd("update", "Force the running Mihomo instance to refresh all active rule providers"))

	return cmd
}

func newRulesSyncCmd(use string, short string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := loadEnv()
			if err != nil {
				return err
			}
			return runRulesSync(env)
		},
	}
}
