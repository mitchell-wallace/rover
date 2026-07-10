package cmd

import (
	"github.com/mitchell-wallace/rover/internal/vm"
	"github.com/spf13/cobra"
)

func init() {
	var noTmux bool
	cmd := &cobra.Command{
		Use:   "ssh [-- remote command]",
		Short: "Open an SSH session to the Rover VM",
		RunE: func(_ *cobra.Command, args []string) error {
			a, err := loadContext()
			if err != nil {
				return err
			}
			return a.vm.SSH(vm.SSHOptions{NoTmux: noTmux}, args...)
		},
	}
	cmd.Flags().BoolVar(&noTmux, "no-tmux", false, "open a plain shell instead of attaching to tmux")
	rootCmd.AddCommand(cmd)
}
