package cli

import "github.com/spf13/cobra"

func newServiceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "service",
		Short: "Manage the Mihomo service",
	}

	cmd.AddCommand(
		newServiceActionCmd("start"),
		newServiceActionCmd("stop"),
		newServiceActionCmd("restart"),
		&cobra.Command{
			Use:   "status",
			Short: "Show service status",
			RunE: func(cmd *cobra.Command, args []string) error {
				env, err := loadEnv()
				if err != nil {
					return err
				}
				return env.ServiceStatus()
			},
		},
	)

	return cmd
}

func newServiceActionCmd(action string) *cobra.Command {
	return &cobra.Command{
		Use:   action,
		Short: action + " the Mihomo service",
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := loadEnv()
			if err != nil {
				return err
			}
			switch action {
			case "start":
				return env.ServiceStart()
			case "stop":
				return env.ServiceStop()
			case "restart":
				return env.ServiceRestart()
			default:
				return nil
			}
		},
	}
}
