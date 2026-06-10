package cmd

import (
	"fmt"
	"os"

	"github.com/charmbracelet/huh"
	"github.com/mitchell-wallace/rover/internal/config"
	"github.com/mitchell-wallace/rover/internal/sizes"
	"github.com/mitchell-wallace/rover/internal/ui"
	"github.com/spf13/cobra"
)

func init() {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Guided first-time setup (region, size, SSH key)",
		Long: "Walk through the handful of choices Rover needs — region, default size, " +
			"admin username, and SSH key — then write the config file.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			st, err := loadStateOnly()
			if err != nil {
				return err
			}
			return doInit(st)
		},
	}
	rootCmd.AddCommand(cmd)
}

// doInit runs the guided first-run setup against st, saves it, and offers to
// generate a missing SSH key. It is safe to re-run; it just edits config.
func doInit(st *config.State) error {
	if !ui.Interactive() {
		return fmt.Errorf("'rover init' needs an interactive terminal; use 'rover config --edit' or edit %s directly", mustConfigPath())
	}

	fmt.Println("Welcome to Rover — let's set up your config.")
	fmt.Println("Press enter to accept the defaults; you can change anything later with 'rover config --edit'.")
	fmt.Println()

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Azure region").
				Description("Where the VM lives. Must have VM core quota for your subscription.").
				Value(&st.Location),
			huh.NewSelect[string]().
				Title("Default family").
				Description("burstable (cheap, CPU-credit) · balanced (sustained CPU) · ramheavy (memory-optimized).").
				Options(familyOptions()...).
				Value(&st.Family),
			huh.NewSelect[string]().
				Title("Default size").
				Description("Compute envelope. Disk is independent and persists across resizes.").
				Options(sizeOptions(st.Fam())...).
				Value(&st.Size),
			huh.NewInput().
				Title("Admin username").
				Description("The Linux login Rover creates on the VM.").
				Value(&st.AdminUsername).
				Validate(config.ValidateAdminUsername),
			huh.NewInput().
				Title("SSH public key path").
				Description("Used to log in. If it doesn't exist yet, Rover can generate a key pair next.").
				Value(&st.SSHPublicKey),
			huh.NewInput().
				Title("Azure subscription (blank = az default)").
				Description("Pin a subscription, or leave blank to use your active 'az' login.").
				Value(&st.Subscription),
		),
	)
	if err := form.Run(); err != nil {
		return err
	}
	st.Family = sizes.NormalizeFamily(st.Family)
	st.Size = normalizeSizeForFamily(st.Family, st.Size)
	if err := st.Save(); err != nil {
		return err
	}
	ui.Info("Config saved to %s", mustConfigPath())

	// Offer to generate the key pair if the chosen public key is missing.
	if _, err := os.Stat(st.SSHPublicKey); err != nil {
		gen, cerr := ui.Confirm(
			"SSH public key not found — generate an ed25519 key pair now?",
			fmt.Sprintf("Creates %s (and its .pub).", st.PrivateKeyPath()),
			true,
		)
		if cerr == nil && gen {
			if ferr := generateSSHKey(st); ferr != nil {
				ui.Warn("key generation failed: %v", ferr)
			}
		}
	}

	fmt.Println("\nNext steps:")
	fmt.Println("  rover doctor      # verify prerequisites (az login, Bicep, Ansible, ...)")
	fmt.Println("  rover up small    # provision the VM")
	return nil
}

func mustConfigPath() string {
	p, err := config.Path()
	if err != nil {
		return "(error: " + err.Error() + ")"
	}
	return p
}
