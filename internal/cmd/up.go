package cmd

import (
	"context"
	"os"
	"strings"

	"github.com/mitchell-wallace/chassis/remember"
	"github.com/mitchell-wallace/rover/internal/locale"
	"github.com/mitchell-wallace/rover/internal/sizes"
	"github.com/mitchell-wallace/rover/internal/ui"
	"github.com/spf13/cobra"
)

var runRememberPrompt = remember.Run

func init() {
	var sizeFlag string
	var familyFlag string
	var assumeYes bool
	var noProvision bool
	var timezoneFlag string
	var localeFlag string

	cmd := &cobra.Command{
		Use:   "up [small|medium|large]",
		Short: "Provision (or resize) the Rover VM",
		Long: "Create or redeploy the single Rover-managed VM at the chosen family and size.\n\n" +
			"Families: " + strings.Join(sizes.Families, " | ") + " (default burstable).\n" +
			"Sizes:    " + strings.Join(sizes.Order, " | ") + " (xsmall is burstable-only).",
		ValidArgs: sizes.Order,
		Args:      cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			a, err := loadContext()
			if err != nil {
				return err
			}
			if timezoneFlag != "" {
				if err := locale.ValidateTimezone(timezoneFlag); err != nil {
					return err
				}
				a.provision.Timezone = timezoneFlag
				a.state.Timezone = timezoneFlag
			}
			if localeFlag != "" {
				if err := locale.ValidateLocale(localeFlag); err != nil {
					return err
				}
				a.provision.Locale = localeFlag
				a.state.Locale = localeFlag
			}
			if timezoneFlag != "" || localeFlag != "" {
				if err := a.state.Save(); err != nil {
					return err
				}
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

			familyExplicit := cmd.Flags().Changed("family")
			sizeExplicit := cmd.Flags().Changed("size") || len(args) == 1
			family, size, err = selectUpCompute(ctx, family, size, familyExplicit, sizeExplicit, assumeYes, ui.Interactive())
			if err != nil {
				return err
			}
			return a.vm.Up(ctx, family, size, assumeYes, noProvision)
		},
	}
	cmd.Flags().StringVar(&familyFlag, "family", "", "compute family: "+strings.Join(sizes.Families, "|"))
	cmd.Flags().StringVar(&sizeFlag, "size", "", "size profile: "+strings.Join(sizes.Order, "|"))
	cmd.Flags().BoolVarP(&assumeYes, "yes", "y", false, "skip confirmation prompts")
	cmd.Flags().BoolVar(&noProvision, "no-provision", false, "on a fresh create, don't auto-run provisioning")
	cmd.Flags().StringVar(&timezoneFlag, "timezone", "", "set the VM timezone (IANA zone, e.g. America/New_York)")
	cmd.Flags().StringVar(&localeFlag, "locale", "", "set the VM locale (e.g. en_US.UTF-8)")
	rootCmd.AddCommand(cmd)
}

func selectUpCompute(ctx context.Context, family, size string, familyExplicit, sizeExplicit, assumeYes, interactive bool) (string, string, error) {
	if familyExplicit || sizeExplicit || assumeYes || !interactive {
		return family, size, nil
	}

	family, err := runRememberPrompt(ctx, os.Stdin, os.Stdout, remember.Config{
		Label:       "Compute family",
		Remembered:  &family,
		Choices:     familyRememberChoices(),
		Validate:    sizes.ValidateFamily,
		Explanation: "Compute families trade burst pricing, sustained CPU, and memory capacity.",
	})
	if err != nil {
		return "", "", err
	}

	size = normalizeSizeForFamily(family, size)
	size, err = runRememberPrompt(ctx, os.Stdin, os.Stdout, remember.Config{
		Label:      "Compute size",
		Remembered: &size,
		Choices:    sizeRememberChoices(family),
		Validate: func(candidate string) error {
			return sizes.Validate(family, candidate)
		},
		Explanation: "Larger sizes trade higher Azure cost for more CPU and memory.",
	})
	if err != nil {
		return "", "", err
	}

	return family, size, nil
}

func familyRememberChoices() []remember.Choice {
	choices := make([]remember.Choice, 0, len(sizes.Families))
	for _, family := range sizes.Families {
		choices = append(choices, remember.Choice{
			Value: family,
			Label: sizes.DescribeFamily(family),
		})
	}
	return choices
}

func sizeRememberChoices(family string) []remember.Choice {
	available := sizes.Available(family)
	choices := make([]remember.Choice, 0, len(available))
	for _, size := range available {
		profile, _ := sizes.Get(family, size)
		choices = append(choices, remember.Choice{
			Value: size,
			Label: profile.Describe(),
		})
	}
	return choices
}
