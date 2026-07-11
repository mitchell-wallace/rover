// Package vm owns Rover's single-VM lifecycle workflows.
package vm

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/mitchell-wallace/rover/internal/azure"
	"github.com/mitchell-wallace/rover/internal/config"
	"github.com/mitchell-wallace/rover/internal/tailscale"
)

// AzureLifecycle is the Azure subset needed by VM lifecycle workflows.
type AzureLifecycle interface {
	Up(family, size string) (azure.Info, error)
	Down(del, yes bool) (azure.Info, error)
	Status() (azure.Info, error)
	Restart() (azure.Info, error)
	ResizeDisk(gb int) (azure.Info, error)
	SSH(tmux bool, extra ...string) error
	RunCommand(ctx context.Context, script string) error
}

type connRestorer interface {
	Ready() bool
	Restore(ctx context.Context) error
}

type provisioner interface {
	Run(ctx context.Context) error
	ResizeSwapfile(ctx context.Context) error
}

// SSHOptions controls one public SSH invocation.
type SSHOptions struct {
	NoTmux bool
}

// Service orchestrates VM lifecycle operations and composes connectivity and
// provisioning through narrow consumer-side interfaces.
type Service struct {
	State     *config.State
	Azure     AzureLifecycle
	TS        tailscale.Client
	Conn      connRestorer
	Provision provisioner
}

func scrubKnownHosts(host string, port int) {
	if host == "" {
		return
	}
	for _, target := range []string{host, fmt.Sprintf("[%s]:%d", host, port)} {
		_ = exec.Command("ssh-keygen", "-R", target).Run()
	}
}

func printInfo(info azure.Info) {
	fmt.Printf("  size:        %s\n", info.VMSize)
	if info.DiskSizeGB > 0 {
		fmt.Printf("  disk:        %d GiB\n", info.DiskSizeGB)
	}
	fmt.Printf("  region:      %s\n", info.Location)
	fmt.Printf("  public IP:   %s\n", info.PublicIP)
	fmt.Printf("  fqdn:        %s\n", info.FQDN)
	fmt.Printf("  private IP:  %s\n", info.PrivateIP)
	fmt.Printf("  ssh target:  %s\n", info.SSHTarget)
}
