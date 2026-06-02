package cmd

import (
	"strconv"

	"github.com/spf13/cobra"
)

func init() {
	var assumeYes bool

	cmd := &cobra.Command{
		Use:   "disk <size-gb>",
		Short: "Grow the OS disk (independent of compute size; preserves data)",
		Long: `Resize the persistent OS disk to <size-gb> GiB.

The disk is independent of the compute size, so it (and your data) is preserved
when you change size with 'rover up'. Azure disks can only grow, never shrink.
The VM is briefly deallocated during the resize and restarted if it was running.`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			gb, err := strconv.Atoi(args[0])
			if err != nil {
				return err
			}
			a, err := loadContext()
			if err != nil {
				return err
			}
			return doDisk(a, gb, assumeYes)
		},
	}
	cmd.Flags().BoolVarP(&assumeYes, "yes", "y", false, "skip confirmation prompts")
	rootCmd.AddCommand(cmd)
}
