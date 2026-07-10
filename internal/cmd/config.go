package cmd

import (
	"fmt"
	"strconv"

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
	p, err := config.Path()
	if err != nil {
		p = "(error: " + err.Error() + ")"
	}
	fmt.Printf("Rover config (%s)\n", p)
	azure := st.AzureSettings()
	azureDir, azureDirErr := st.AzureConfigDir()
	if azureDirErr != nil {
		azureDir = "(error: " + azureDirErr.Error() + ")"
	}
	if config.AzureConfigDirOverridden() {
		azureDir += " (AZURE_CONFIG_DIR override)"
	}
	fmt.Printf("  azure config dir: %s\n", azureDir)
	fmt.Printf("  subscription:    %s\n", orDefault(st.AzureSubscription(), "(az default)"))
	fmt.Printf("  tenant:          %s\n", orDefault(azure.Tenant, "(az default)"))
	fmt.Printf("  resource group:  %s\n", st.ResourceGroup)
	fmt.Printf("  region:          %s\n", st.Location)
	fmt.Printf("  vm name:         %s\n", st.VMName)
	fmt.Printf("  family:          %s\n", st.Fam())
	fmt.Printf("  size:            %s\n", st.Size)
	fmt.Printf("  disk:            %d GiB\n", st.DiskGB())
	fmt.Printf("  admin username:  %s\n", st.AdminUsername)
	fmt.Printf("  ssh port:        %d\n", st.SSHPort())
	fmt.Printf("  ssh tmux:        %v\n", st.SSHTmux())
	fmt.Printf("  ssh public key:  %s\n", st.SSHPublicKey)
	fmt.Printf("  ssh private key: %s\n", st.PrivateKeyPath())
	fmt.Printf("  ansible applied: %v\n", st.AnsibleApplied)
	fmt.Printf("  tailscale name:  %s\n", st.TSHostname())
	fmt.Printf("  tailscale tags:  %s\n", st.TSTags())
	fmt.Printf("  tailscale client id: %s\n", orDefault(st.TailscaleClientID, "(not set)"))
	secretStatus := "(not set)"
	if st.TailscaleClientSecret != "" {
		secretStatus = "(set, masked)"
	}
	fmt.Printf("  tailscale secret:    %s\n", secretStatus)
	fmt.Printf("  public ssh closed:   %v\n", st.PublicSSHClosed)
}

func editConfig(st *config.State) error {
	// huh inputs bind to strings; round-trip the SSH port through one.
	portStr := strconv.Itoa(st.SSHPort())
	azure := st.AzureSettings()
	ssh := st.SSHSettings()
	defaultAzureDir, err := config.DefaultAzureConfigDir()
	if err != nil {
		return err
	}
	form := huh.NewForm(huh.NewGroup(
		huh.NewInput().Title("Azure config directory (blank = Rover default)").
			Description("Default: "+defaultAzureDir).Value(&azure.ConfigDir),
		huh.NewInput().Title("Azure subscription (blank = az default)").Value(&azure.Subscription),
		huh.NewInput().Title("Azure tenant (blank = az default)").Value(&azure.Tenant),
		huh.NewInput().Title("Resource group").Value(&st.ResourceGroup),
		huh.NewInput().Title("Region").Value(&st.Location),
		huh.NewInput().Title("VM name").Value(&st.VMName),
		huh.NewSelect[string]().Title("Default family").
			Options(familyOptions()...).Value(&st.Family),
		huh.NewSelect[string]().Title("Default size").
			Options(huh.NewOptions(sizes.Order...)...).Value(&st.Size),
		huh.NewInput().Title("Admin username").Value(&st.AdminUsername).
			Validate(config.ValidateAdminUsername),
		huh.NewInput().Title("Public SSH port").Value(&portStr).Validate(validatePortStr),
		huh.NewConfirm().Title("Attach interactive SSH sessions to tmux").Value(&ssh.Tmux),
		huh.NewInput().Title("SSH public key path").Value(&st.SSHPublicKey),
		huh.NewInput().Title("Tailscale hostname (blank = VM name)").Value(&st.TailscaleHostname),
		huh.NewInput().Title("Tailscale tags").Value(&st.TailscaleTags),
		huh.NewInput().Title("Tailscale OAuth Client ID").Value(&st.TailscaleClientID),
		huh.NewInput().Title("Tailscale OAuth Client Secret").EchoMode(huh.EchoModePassword).Value(&st.TailscaleClientSecret),
		huh.NewConfirm().Title("Close public SSH port (only allow Tailscale SSH)").Value(&st.PublicSSHClosed),
	))
	if err := form.Run(); err != nil {
		return err
	}
	if p, err := strconv.Atoi(portStr); err == nil {
		st.SSHListenPort = p
	}
	st.Family = sizes.NormalizeFamily(st.Family)
	st.Size = normalizeSizeForFamily(st.Family, st.Size)
	if err := st.Save(); err != nil {
		return err
	}
	fmt.Println("Saved.")
	printConfig(st)
	return nil
}

// validatePortStr validates a port entered as text in the config form.
func validatePortStr(s string) error {
	p, err := strconv.Atoi(s)
	if err != nil {
		return fmt.Errorf("ssh port must be a number")
	}
	return config.ValidateSSHPort(p)
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
