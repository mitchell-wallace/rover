package cmd

import "github.com/spf13/cobra"

func init() {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show the VM power state and connection info",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadContext()
			if err != nil {
				return err
			}
			return doStatus(a)
		},
	}
	rootCmd.AddCommand(cmd)
}
