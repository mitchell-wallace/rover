package cmd

import (
	"context"
	"fmt"

	"github.com/charmbracelet/huh"
	"github.com/mitchell-wallace/rover/internal/config"
	"github.com/mitchell-wallace/rover/internal/sizes"
	"github.com/mitchell-wallace/rover/internal/ui"
)

// runInteractive drives the menu shown for a bare `rover` invocation. Every
// branch calls the same service methods as the non-interactive subcommands.
func runInteractive() error {
	if !ui.Interactive() {
		// No TTY: fall back to a status summary rather than blocking on a prompt.
		a, err := loadContext()
		if err != nil {
			return err
		}
		return a.vm.Status()
	}

	// First run (no state file yet): walk through guided setup before the menu.
	configured, err := config.Exists()
	if err != nil {
		return err
	}
	if !configured {
		st, err := loadStateOnly()
		if err != nil {
			return err
		}
		if err := doInit(st); err != nil {
			return err
		}
		fmt.Println()
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
				huh.NewOption("Command (run on VM)", "command"),
				huh.NewOption("Down (deallocate)", "down"),
				huh.NewOption("Delete all resources", "delete"),
				huh.NewOption("Config", "config"),
				huh.NewOption("Setup (re-run init)", "init"),
				huh.NewOption("Quit", "quit"),
			).
			Value(&action).
			Run()
		if err != nil {
			return err
		}

		switch action {
		case "status":
			err = a.vm.Status()
		case "up":
			family := a.state.Fam()
			size := a.state.Size
			if size == "" {
				size = "small"
			}
			err = huh.NewSelect[string]().
				Title("Family").
				Options(familyOptions()...).
				Value(&family).
				Run()
			if err == nil {
				// Keep size valid for the chosen family (e.g. xsmall is
				// burstable-only); fall back to the family's smallest.
				size = normalizeSizeForFamily(family, size)
				err = huh.NewSelect[string]().
					Title("Size").
					Options(sizeOptions(family)...).
					Value(&size).
					Run()
			}
			if err == nil {
				err = a.vm.Up(context.Background(), family, size, false, false)
			}
		case "provision":
			err = a.provision.Run(context.Background())
		case "ssh":
			err = a.vm.SSH()
		case "connect":
			err = a.conn.Connect(context.Background())
		case "command":
			var cmdStr string
			if err = huh.NewInput().
				Title("Command to run on the VM").
				Value(&cmdStr).
				Run(); err == nil && cmdStr != "" {
				fmt.Println()
				err = a.conn.RunCommand(context.Background(), []string{cmdStr})
			}
		case "down":
			err = a.vm.Down(context.Background(), false, false)
		case "delete":
			err = a.vm.Down(context.Background(), true, false)
		case "config":
			err = editConfig(a.state)
		case "init":
			err = doInit(a.state)
		case "quit":
			return nil
		}

		if err != nil {
			ui.Warn("%v", err)
		}
		fmt.Println()
	}
}

func familyOptions() []huh.Option[string] {
	opts := make([]huh.Option[string], 0, len(sizes.Families))
	for _, name := range sizes.Families {
		opts = append(opts, huh.NewOption(sizes.DescribeFamily(name), name))
	}
	return opts
}

// normalizeSizeForFamily returns size if the family offers it, otherwise the
// family's smallest available size. Guards cross-family edges like the
// burstable-only xsmall tier.
func normalizeSizeForFamily(family, size string) string {
	if _, ok := sizes.Get(family, size); ok {
		return size
	}
	if avail := sizes.Available(family); len(avail) > 0 {
		return avail[0]
	}
	return size
}

func sizeOptions(family string) []huh.Option[string] {
	avail := sizes.Available(family)
	opts := make([]huh.Option[string], 0, len(avail))
	for _, name := range avail {
		p, _ := sizes.Get(family, name)
		opts = append(opts, huh.NewOption(p.Describe(), name))
	}
	return opts
}
