package cmd

import "github.com/spf13/cobra"

func init() {
	var deleteAll bool
	var assumeYes bool

	cmd := &cobra.Command{
		Use:   "down",
		Short: "Deallocate the VM (or delete everything with --delete)",
		Long: `Stop the Rover VM.

Default: deallocate — stops compute billing. The OS disk and static public IP
remain and continue to incur small charges.

--delete: delete the entire resource group (VM, disks, IP, network). Destructive
and removes persistent data. Requires confirmation or --yes.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadContext()
			if err != nil {
				return err
			}
			return doDown(a, deleteAll, assumeYes)
		},
	}
	cmd.Flags().BoolVar(&deleteAll, "delete", false, "delete all resources (destructive)")
	cmd.Flags().BoolVarP(&assumeYes, "yes", "y", false, "skip confirmation prompts")
	rootCmd.AddCommand(cmd)
}
