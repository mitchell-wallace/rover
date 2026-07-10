package cmd

import "github.com/spf13/cobra"

func init() {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show Rover's Azure login and VM connection status",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			a, err := loadContext()
			if err != nil {
				return err
			}
			return a.status()
		},
	}
	rootCmd.AddCommand(cmd)
}
