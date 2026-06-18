package cmd

import "github.com/spf13/cobra"

func init() {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show the VM power state and connection info",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			a, err := loadContext()
			if err != nil {
				return err
			}
			return a.vm.Status()
		},
	}
	rootCmd.AddCommand(cmd)
}
