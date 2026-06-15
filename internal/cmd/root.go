// Package cmd wires up Rover's Cobra commands. Interactive mode (bare `rover`)
// and the non-interactive subcommands call the same service functions in
// internal/azure and internal/ansible, so the two paths stay at parity.
package cmd

import (
	"fmt"
	"os"

	"github.com/mitchell-wallace/rover/internal/azure"
	"github.com/mitchell-wallace/rover/internal/config"
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

// azureProvider is the subset of Azure operations used by command handlers.
// The concrete *azure.Client satisfies this interface; tests provide mocks.
type azureProvider interface {
	Up(family, size string) (azure.Info, error)
	Down(del, yes bool) (azure.Info, error)
	Status() (azure.Info, error)
	ResizeDisk(gb int) (azure.Info, error)
	Restart() (azure.Info, error)
	Info() (azure.Info, error)
	SSH(extra ...string) error
	SetPublicSSH(allowed bool) error
	RunCommand(script string) error
}

// appContext bundles the loaded state and a ready Azure client.
type appContext struct {
	state    *config.State
	azure    azureProvider
	assetDir string
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
	return &appContext{
		state:    st,
		azure:    azure.New(st, dir),
		assetDir: dir,
	}, nil
}
