package cmd

import (
	"fmt"

	"github.com/mitchell-wallace/rover/internal/ui"
	"github.com/spf13/cobra"
)

func init() {
	var deleteOnline bool
	var dryRun bool
	var assumeYes bool

	tailscaleCmd := &cobra.Command{
		Use:   "tailscale",
		Short: "Manage Rover's Tailscale integration",
	}

	cleanupCmd := &cobra.Command{
		Use:   "cleanup",
		Short: "Remove stale Rover devices from Tailscale",
		Long: `Remove Rover-managed devices from the Tailscale control plane.

By default this deletes only matching stale/offline devices and skips an online
Rover VM. Pass --all to delete every matching Rover device, which is useful after
the VM has been deleted or manually logged out of Tailscale.`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			a, err := loadContext()
			if err != nil {
				return err
			}
			if dryRun {
				_, err := doTailscaleCleanup(a, deleteOnline, true)
				return err
			}
			if !assumeYes {
				prompt := "Delete stale/offline Rover Tailscale devices?"
				if deleteOnline {
					prompt = "Delete ALL matching Rover Tailscale devices, including online devices?"
				}
				ok, err := ui.Confirm(prompt, fmt.Sprintf("Matches hostname %q with tags %q.", a.state.TSHostname(), a.state.TSTags()), !deleteOnline)
				if err != nil {
					return err
				}
				if !ok {
					return fmt.Errorf("aborted")
				}
			}
			_, err = doTailscaleCleanup(a, deleteOnline, false)
			return err
		},
	}
	cleanupCmd.Flags().BoolVar(&deleteOnline, "all", false, "delete all matching Rover devices, including online devices")
	cleanupCmd.Flags().BoolVar(&dryRun, "dry-run", false, "show matching devices without deleting")
	cleanupCmd.Flags().BoolVarP(&assumeYes, "yes", "y", false, "skip confirmation prompts")

	tailscaleCmd.AddCommand(cleanupCmd)
	rootCmd.AddCommand(tailscaleCmd)
}
