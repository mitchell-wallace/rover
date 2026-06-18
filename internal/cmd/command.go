package cmd

import (
	"context"

	"github.com/spf13/cobra"
)

func init() {
	cmd := &cobra.Command{
		Use:   "command <command>",
		Short: "Run a command on the VM via Tailscale or SSH",
		Long: `Run a single command on the Rover VM non-interactively.

Uses Tailscale SSH when available, falling back to public SSH otherwise.
The command's stdout/stderr stream to your terminal and its exit code is
propagated.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			a, err := loadContext()
			if err != nil {
				return err
			}
			return a.conn.RunCommand(context.Background(), args)
		},
	}
	rootCmd.AddCommand(cmd)
}
