// Package cmd wires up Rover's Cobra commands. Interactive mode (bare `rover`)
// and the non-interactive subcommands call the same service methods, so the
// two paths stay at parity.
package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/mitchell-wallace/rover/internal/azure"
	"github.com/mitchell-wallace/rover/internal/config"
	"github.com/mitchell-wallace/rover/internal/connectivity"
	"github.com/mitchell-wallace/rover/internal/provision"
	"github.com/mitchell-wallace/rover/internal/tailscale"
	"github.com/mitchell-wallace/rover/internal/vm"
	"github.com/spf13/cobra"

	assets "github.com/mitchell-wallace/rover"
)

var version = "dev"

var rootCmd = &cobra.Command{
	Use:   "rover",
	Short: "Rover — provision remote Linux VM compute for running Dune",
	Long: `Rover provisions and manages a single remote Linux VM on Azure so you can
SSH in and run Dune there. Run 'rover' with no arguments for an interactive
menu, or use the subcommands (up/down/status/ssh/provision/doctor) directly.`,
	SilenceUsage:  true,
	SilenceErrors: true,
	// Bare `rover` launches the interactive menu.
	RunE: func(_ *cobra.Command, _ []string) error {
		return runInteractive()
	},
}

// Execute is the entrypoint called by main.
func Execute(v string) error {
	version = v
	rootCmd.Version = v
	rootCmd.SetVersionTemplate("{{.Version}}\n")
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "rover:", err)
		return err
	}
	return nil
}

// appContext is the command-layer composition root.
type appContext struct {
	state     *config.State
	assetDir  string
	vm        *vm.Service
	conn      *connectivity.Service
	provision *provision.Service
}

// loadStateOnly loads state without materializing assets (for commands that
// don't shell out, like doctor/config).
func loadStateOnly() (*config.State, error) {
	return config.Load()
}

// loadContext loads state and prepares the Azure client + asset tree.
func loadContext() (*appContext, error) {
	st, err := config.Load()
	if err != nil {
		return nil, err
	}
	dir, err := assets.Dir(version)
	if err != nil {
		return nil, fmt.Errorf("materialize assets: %w", err)
	}
	az := azure.New(st, dir)
	tsClient := tailscale.NewClient()
	conn := connectivity.New(st, az, tsClient)
	prov := provision.New(st, az, tsClient, dir)
	vmSvc := &vm.Service{
		State:     st,
		Azure:     az,
		TS:        tsClient,
		Conn:      conn,
		Provision: prov,
	}
	conn.Restart = func() error {
		return vmSvc.Restart(context.Background())
	}
	return &appContext{
		state:     st,
		assetDir:  dir,
		vm:        vmSvc,
		conn:      conn,
		provision: prov,
	}, nil
}
