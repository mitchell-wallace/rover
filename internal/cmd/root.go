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
	"github.com/mitchell-wallace/rover/internal/ui"
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
menu, or use the subcommands (login/logout/up/down/status/ssh/provision/doctor) directly.`,
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
	azure     *azure.Client
	vm        *vm.Service
	conn      *connectivity.Service
	provision *provision.Service
}

// loadStateOnly loads state without materializing assets (for commands that
// don't shell out, like doctor/config).
func loadStateOnly() (*config.State, error) {
	return config.Load()
}

// loadAzureOnly prepares Azure auth/account operations without materializing
// Rover's embedded infrastructure assets.
func loadAzureOnly() (*config.State, *azure.Client, error) {
	st, err := config.Load()
	if err != nil {
		return nil, nil, err
	}
	az := newAzureClient(st, "")
	return st, az, nil
}

func newAzureClient(st *config.State, assetDir string) *azure.Client {
	if config.AzureConfigDirOverridden() {
		effective, effectiveErr := st.AzureConfigDir()
		configured, configuredErr := st.ConfiguredAzureConfigDir()
		if effectiveErr == nil && configuredErr == nil {
			ui.Warn("AZURE_CONFIG_DIR is set; using %s instead of Rover config %s", effective, configured)
		}
	}
	return azure.New(st, assetDir)
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
	az := newAzureClient(st, dir)
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
	conn.Restart = func(ctx context.Context) error {
		return vmSvc.Restart(ctx)
	}
	return &appContext{
		state:     st,
		assetDir:  dir,
		azure:     az,
		vm:        vmSvc,
		conn:      conn,
		provision: prov,
	}, nil
}
