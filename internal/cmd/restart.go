package cmd

import (
	"context"

	"github.com/spf13/cobra"
)

func init() {
	cmd := &cobra.Command{
		Use:   "restart",
		Short: "Restart the running Rover VM",
		Long: `Restart the Rover VM through Azure.

The VM must already be running. Use 'rover up' to start a deallocated VM.`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			a, err := loadContext()
			if err != nil {
				return err
			}
			return a.vm.Restart(context.Background())
		},
	}
	rootCmd.AddCommand(cmd)
}
