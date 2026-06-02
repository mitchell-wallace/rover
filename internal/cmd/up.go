package cmd

import (
	"github.com/mitchell-wallace/rover/internal/sizes"
	"github.com/spf13/cobra"
)

func init() {
	var sizeFlag string
	var assumeYes bool

	cmd := &cobra.Command{
		Use:       "up [small|medium|large]",
		Short:     "Provision (or resize) the Rover VM",
		Long:      "Create or redeploy the single Rover-managed VM at the chosen size.",
		ValidArgs: sizes.Order,
		Args:      cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			a, err := loadContext()
			if err != nil {
				return err
			}
			size := a.state.Size
			if sizeFlag != "" {
				size = sizeFlag
			}
			if len(args) == 1 {
				size = args[0]
			}
			if size == "" {
				size = "small"
			}
			return doUp(a, size, assumeYes)
		},
	}
	cmd.Flags().StringVar(&sizeFlag, "size", "", "size profile: small|medium|large")
	cmd.Flags().BoolVarP(&assumeYes, "yes", "y", false, "skip confirmation prompts")
	rootCmd.AddCommand(cmd)
}
