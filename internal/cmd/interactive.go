package cmd

import (
	"fmt"

	"github.com/charmbracelet/huh"
	"github.com/mitchell-wallace/rover/internal/sizes"
	"github.com/mitchell-wallace/rover/internal/ui"
)

// runInteractive drives the menu shown for a bare `rover` invocation. Every
// branch calls the same do* functions as the non-interactive subcommands.
func runInteractive() error {
	if !ui.Interactive() {
		// No TTY: fall back to a status summary rather than blocking on a prompt.
		a, err := loadContext()
		if err != nil {
			return err
		}
		return doStatus(a)
	}

	a, err := loadContext()
	if err != nil {
		return err
	}

	fmt.Printf("Rover %s — remote VM compute for Dune\n\n", version)

	for {
		var action string
		err := huh.NewSelect[string]().
			Title("What would you like to do?").
			Options(
				huh.NewOption("Status", "status"),
				huh.NewOption("Up (start/resize VM)", "up"),
				huh.NewOption("Provision (Ansible)", "provision"),
				huh.NewOption("SSH into VM", "ssh"),
				huh.NewOption("Connect (Tailscale)", "connect"),
				huh.NewOption("Down (deallocate)", "down"),
				huh.NewOption("Delete all resources", "delete"),
				huh.NewOption("Config", "config"),
				huh.NewOption("Quit", "quit"),
			).
			Value(&action).
			Run()
		if err != nil {
			return err
		}

		switch action {
		case "status":
			err = doStatus(a)
		case "up":
			var size string = a.state.Size
			if size == "" {
				size = "small"
			}
			err = huh.NewSelect[string]().
				Title("Size").
				Options(sizeOptions()...).
				Value(&size).
				Run()
			if err == nil {
				err = doUp(a, size, false)
			}
		case "provision":
			err = doProvision(a)
		case "ssh":
			err = doSSH(a)
		case "connect":
			err = doConnect(a)
		case "down":
			err = doDown(a, false, false)
		case "delete":
			err = doDown(a, true, false)
		case "config":
			err = editConfig(a.state)
		case "quit":
			return nil
		}

		if err != nil {
			ui.Warn("%v", err)
		}
		fmt.Println()
	}
}

func sizeOptions() []huh.Option[string] {
	opts := make([]huh.Option[string], 0, len(sizes.Order))
	for _, name := range sizes.Order {
		p, _ := sizes.Get(name)
		opts = append(opts, huh.NewOption(p.Describe(), name))
	}
	return opts
}
