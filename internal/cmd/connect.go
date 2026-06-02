package cmd

import "github.com/spf13/cobra"

func init() {
	cmd := &cobra.Command{
		Use:   "connect [-- extra ssh args]",
		Short: "Connect to the VM over Tailscale (Tailscale SSH)",
		Long: `Connect to the Rover VM over your tailnet using Tailscale SSH, independent of
the Azure public IP. Requires the VM to have been provisioned with Tailscale
(TS_AUTHKEY=<key> rover provision) and the local 'tailscale' client connected.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadContext()
			if err != nil {
				return err
			}
			return doConnect(a, args...)
		},
	}
	rootCmd.AddCommand(cmd)
}
