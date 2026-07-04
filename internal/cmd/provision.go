package cmd

import (
	"context"

	"github.com/mitchell-wallace/rover/internal/locale"
	"github.com/spf13/cobra"
)

func init() {
	var timezoneFlag string
	var localeFlag string

	cmd := &cobra.Command{
		Use:   "provision",
		Short: "Configure the VM for Dune with Ansible",
		Long:  "Run the Ansible playbook against the live VM to install Docker, dune, zsh, and tooling.",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
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
			return a.provision.Run(context.Background())
		},
	}
	cmd.Flags().StringVar(&timezoneFlag, "timezone", "", "set the VM timezone (IANA zone, e.g. America/New_York)")
	cmd.Flags().StringVar(&localeFlag, "locale", "", "set the VM locale (e.g. en_US.UTF-8)")
	rootCmd.AddCommand(cmd)
}
