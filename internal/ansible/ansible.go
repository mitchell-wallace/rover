// Package ansible runs the host-configuration playbook against the live VM.
// It builds an ad-hoc inventory ("<host>,") so no inventory file is needed and
// the playbook stays usable by hand.
package ansible

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// Params describes a provisioning run.
type Params struct {
	Host       string
	User       string
	PrivateKey string
	AssetDir   string
	ExtraVars  map[string]string
}

// Available reports whether ansible-playbook is on PATH.
func Available() bool {
	_, err := exec.LookPath("ansible-playbook")
	return err == nil
}

// Provision runs the playbook, streaming output to the user's terminal.
func Provision(p Params) error {
	if !Available() {
		return fmt.Errorf("ansible-playbook not found on PATH; install Ansible (e.g. 'pipx install ansible' or 'pip install --user ansible')")
	}
	if p.Host == "" {
		return fmt.Errorf("no host to provision; is the VM up?")
	}

	ansibleDir := filepath.Join(p.AssetDir, "ansible")
	args := []string{
		"-i", p.Host + ",",
		"-u", p.User,
		"playbook.yml",
	}
	if p.PrivateKey != "" {
		args = append(args, "--private-key", p.PrivateKey)
	}
	for k, v := range p.ExtraVars {
		args = append(args, "-e", k+"="+v)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ansible-playbook", args...)
	cmd.Dir = ansibleDir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(),
		"ANSIBLE_CONFIG="+filepath.Join(ansibleDir, "ansible.cfg"),
		"ANSIBLE_HOST_KEY_CHECKING=False",
	)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ansible-playbook: %w", err)
	}
	return nil
}
