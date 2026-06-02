package cmd

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/mitchell-wallace/rover/internal/ansible"
	"github.com/spf13/cobra"
)

func init() {
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check that prerequisites for Rover are installed/configured",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
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

	ok := true
	check := func(name string, pass bool, hint string) {
		if pass {
			fmt.Printf("  \033[1;32m✓\033[0m %s\n", name)
			return
		}
		ok = false
		fmt.Printf("  \033[1;31m✗\033[0m %s — %s\n", name, hint)
	}

	fmt.Println("Rover prerequisites:")

	_, azErr := exec.LookPath("az")
	check("Azure CLI (az) installed", azErr == nil, "install from https://learn.microsoft.com/cli/azure/install-azure-cli")

	if azErr == nil {
		loggedIn := exec.Command("az", "account", "show", "-o", "none").Run() == nil
		check("Azure CLI logged in", loggedIn, "run 'az login'")
		bicepOK := exec.Command("az", "bicep", "version").Run() == nil
		check("Bicep available", bicepOK, "run 'az bicep install'")
	}

	_, sshErr := exec.LookPath("ssh")
	check("ssh client installed", sshErr == nil, "install OpenSSH client")

	check("Ansible installed", ansible.Available(), "install Ansible (e.g. 'pipx install ansible')")

	_, keyErr := os.Stat(st.SSHPublicKey)
	check(fmt.Sprintf("SSH public key present (%s)", st.SSHPublicKey), keyErr == nil,
		"generate with 'ssh-keygen -t ed25519' or set the path via 'rover config --edit'")

	fmt.Println()
	if ok {
		fmt.Println("All checks passed. You're ready: 'rover up small'")
		return nil
	}
	return fmt.Errorf("some checks failed; address the hints above")
}
