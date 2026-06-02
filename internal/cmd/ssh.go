package cmd

import "github.com/spf13/cobra"

func init() {
	cmd := &cobra.Command{
		Use:   "ssh [-- extra ssh args]",
		Short: "Open an SSH session to the Rover VM",
		RunE: func(_ *cobra.Command, args []string) error {
			a, err := loadContext()
			if err != nil {
				return err
			}
			return doSSH(a, args...)
		},
	}
	rootCmd.AddCommand(cmd)
}
