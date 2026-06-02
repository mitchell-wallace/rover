package cmd

import "github.com/spf13/cobra"

func init() {
	cmd := &cobra.Command{
		Use:   "provision",
		Short: "Configure the VM for Dune with Ansible",
		Long:  "Run the Ansible playbook against the live VM to install Docker, dune, zsh, and tooling.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadContext()
			if err != nil {
				return err
			}
			return doProvision(a)
		},
	}
	rootCmd.AddCommand(cmd)
}
