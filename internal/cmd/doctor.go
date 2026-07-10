package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/mitchell-wallace/rover/internal/ansible"
	"github.com/mitchell-wallace/rover/internal/config"
	"github.com/mitchell-wallace/rover/internal/tailscale"
	"github.com/mitchell-wallace/rover/internal/ui"
	"github.com/spf13/cobra"
)

func init() {
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check that prerequisites for Rover are installed/configured",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return doDoctor()
		},
	}
	rootCmd.AddCommand(cmd)
}

func doDoctor() error {
	st, err := loadStateOnly()
	if err != nil {
		return err
	}
	az := newAzureClient(st, "")

	ok := true
	// pass marks a check result; on failure, when interactive and a fixer is
	// supplied, it offers to run the fix and re-checks once.
	pass := func(name string, test func() bool, hint string, fix func() error) {
		if test() {
			fmt.Printf("  \033[1;32m✓\033[0m %s\n", name)
			return
		}
		fmt.Printf("  \033[1;31m✗\033[0m %s — %s\n", name, hint)
		if fix != nil && ui.Interactive() {
			run, cerr := ui.Confirm(fmt.Sprintf("Fix now: %s?", name), hint, true)
			if cerr == nil && run {
				if ferr := fix(); ferr != nil {
					ui.Warn("fix failed: %v", ferr)
				} else if test() {
					fmt.Printf("  \033[1;32m✓\033[0m %s (fixed)\n", name)
					return
				}
			}
		}
		ok = false
	}

	fmt.Println("Rover prerequisites:")

	azInstalled := func() bool { _, e := exec.LookPath("az"); return e == nil }
	pass("Azure CLI (az) installed", azInstalled,
		"install from https://learn.microsoft.com/cli/azure/install-azure-cli", nil)

	if azInstalled() {
		pass("Azure CLI logged in",
			func() bool {
				account, accountErr := az.Account()
				return accountErr == nil && account.LoggedIn
			},
			"run 'rover login'",
			func() error { return az.Login(true) })
		pass("Bicep available",
			az.BicepAvailable,
			"run 'rover doctor' interactively to install Bicep in Rover's Azure context",
			az.InstallBicep)
	}

	pass("ssh client installed",
		func() bool { _, e := exec.LookPath("ssh"); return e == nil },
		"install OpenSSH client", nil)

	pass("Ansible installed", ansible.Available,
		"install Ansible (e.g. 'pipx install ansible')", nil)

	// Tailscale is optional; report status without failing the overall check.
	if tailscale.Available() {
		fmt.Printf("  \033[1;32m✓\033[0m Tailscale CLI installed (optional, for 'rover connect')\n")
	} else {
		fmt.Printf("  \033[1;34m·\033[0m Tailscale CLI not installed (optional) — needed only for 'rover connect'\n")
	}

	pass(fmt.Sprintf("SSH public key present (%s)", st.SSHPublicKey),
		func() bool { _, e := os.Stat(st.SSHPublicKey); return e == nil },
		"generate with 'ssh-keygen -t ed25519' or set the path via 'rover config --edit'",
		func() error { return generateSSHKey(st) })

	fmt.Println()
	if ok {
		fmt.Println("All checks passed. You're ready: 'rover up small'")
		return nil
	}
	return fmt.Errorf("some checks failed; address the hints above")
}

// runInherit runs a non-Azure command with the user's terminal attached. Azure
// processes go through azure.Client so they always receive the isolated env.
func runInherit(name string, args ...string) error {
	c := exec.Command(name, args...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	return c.Run()
}

// generateSSHKey creates an ed25519 key pair at the derived private key path so
// the configured public key exists. It refuses to clobber an existing private
// key.
func generateSSHKey(st *config.State) error {
	priv := st.PrivateKeyPath()
	if priv == "" {
		return fmt.Errorf("no SSH key path configured")
	}
	if _, err := os.Stat(priv); err == nil {
		return fmt.Errorf("private key %s already exists; not overwriting (set the public key path with 'rover config --edit')", priv)
	}
	if err := os.MkdirAll(filepath.Dir(priv), 0o700); err != nil {
		return err
	}
	ui.Info("Generating ed25519 key pair at %s", priv)
	return runInherit("ssh-keygen", "-t", "ed25519", "-f", priv)
}
