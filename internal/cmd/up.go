package cmd

import (
	"strings"

	"github.com/mitchell-wallace/rover/internal/sizes"
	"github.com/spf13/cobra"
)

func init() {
	var sizeFlag string
	var familyFlag string
	var assumeYes bool

	cmd := &cobra.Command{
		Use:   "up [small|medium|large]",
		Short: "Provision (or resize) the Rover VM",
		Long: "Create or redeploy the single Rover-managed VM at the chosen family and size.\n\n" +
			"Families: " + strings.Join(sizes.Families, " | ") + " (default burstable).\n" +
			"Sizes:    " + strings.Join(sizes.Order, " | ") + " (xsmall is burstable-only).",
		ValidArgs: sizes.Order,
		Args:      cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			a, err := loadContext()
			if err != nil {
				return err
			}
			family := a.state.Fam()
			if familyFlag != "" {
				family = familyFlag
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
			return doUp(a, family, size, assumeYes)
		},
	}
	cmd.Flags().StringVar(&familyFlag, "family", "", "compute family: "+strings.Join(sizes.Families, "|"))
	cmd.Flags().StringVar(&sizeFlag, "size", "", "size profile: "+strings.Join(sizes.Order, "|"))
	cmd.Flags().BoolVarP(&assumeYes, "yes", "y", false, "skip confirmation prompts")
	rootCmd.AddCommand(cmd)
}
