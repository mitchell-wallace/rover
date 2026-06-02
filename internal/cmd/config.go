package cmd

import (
	"fmt"

	"github.com/charmbracelet/huh"
	"github.com/mitchell-wallace/rover/internal/config"
	"github.com/mitchell-wallace/rover/internal/sizes"
	"github.com/spf13/cobra"
)

func init() {
	var edit bool
	var showPath bool

	cmd := &cobra.Command{
		Use:   "config",
		Short: "Show or edit Rover configuration",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if showPath {
				p, err := config.Path()
				if err != nil {
					return err
				}
				fmt.Println(p)
				return nil
			}
			st, err := loadStateOnly()
			if err != nil {
				return err
			}
			if edit {
				return editConfig(st)
			}
			printConfig(st)
			return nil
		},
	}
	cmd.Flags().BoolVar(&edit, "edit", false, "edit configuration interactively")
	cmd.Flags().BoolVar(&showPath, "path", false, "print the config file path")
	rootCmd.AddCommand(cmd)
}

func printConfig(st *config.State) {
	p, _ := config.Path()
	fmt.Printf("Rover config (%s)\n", p)
	fmt.Printf("  subscription:    %s\n", orDefault(st.Subscription, "(az default)"))
	fmt.Printf("  resource group:  %s\n", st.ResourceGroup)
	fmt.Printf("  region:          %s\n", st.Location)
	fmt.Printf("  vm name:         %s\n", st.VMName)
	fmt.Printf("  size:            %s\n", st.Size)
	fmt.Printf("  disk:            %d GiB\n", st.DiskGB())
	fmt.Printf("  admin username:  %s\n", st.AdminUsername)
	fmt.Printf("  ssh public key:  %s\n", st.SSHPublicKey)
	fmt.Printf("  ssh private key: %s\n", st.PrivateKeyPath())
	fmt.Printf("  ansible applied: %v\n", st.AnsibleApplied)
	fmt.Printf("  tailscale name:  %s\n", st.TSHostname())
	fmt.Printf("  tailscale tags:  %s\n", st.TSTags())
}

func editConfig(st *config.State) error {
	form := huh.NewForm(huh.NewGroup(
		huh.NewInput().Title("Azure subscription (blank = az default)").Value(&st.Subscription),
		huh.NewInput().Title("Resource group").Value(&st.ResourceGroup),
		huh.NewInput().Title("Region").Value(&st.Location),
		huh.NewInput().Title("VM name").Value(&st.VMName),
		huh.NewSelect[string]().Title("Default size").
			Options(huh.NewOptions(sizes.Order...)...).Value(&st.Size),
		huh.NewInput().Title("Admin username").Value(&st.AdminUsername).
			Validate(config.ValidateAdminUsername),
		huh.NewInput().Title("SSH public key path").Value(&st.SSHPublicKey),
		huh.NewInput().Title("Tailscale hostname (blank = VM name)").Value(&st.TailscaleHostname),
		huh.NewInput().Title("Tailscale tags").Value(&st.TailscaleTags),
	))
	if err := form.Run(); err != nil {
		return err
	}
	if err := st.Save(); err != nil {
		return err
	}
	fmt.Println("Saved.")
	printConfig(st)
	return nil
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
