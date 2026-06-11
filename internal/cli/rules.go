package cli

import "github.com/spf13/cobra"

func newRulesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rules",
		Short: "Manage rule providers",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "sync",
		Short: "Fetch remote rule providers into the live ruleset directory",
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := loadEnv()
			if err != nil {
				return err
			}
			return env.SyncRules()
		},
	})

	return cmd
}
