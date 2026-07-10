package azure

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSSHScriptTmuxAndCommandModes(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantArgs []string
		wantTmux bool
	}{
		{
			name:     "interactive defaults to tmux",
			wantArgs: []string{"-p", "29472", "-o", "StrictHostKeyChecking=accept-new", "-t", "rover@rover-vm.example.test"},
			wantTmux: true,
		},
		{
			name:     "explicit plain shell",
			args:     []string{"--no-tmux"},
			wantArgs: []string{"-p", "29472", "-o", "StrictHostKeyChecking=accept-new", "rover@rover-vm.example.test"},
		},
		{
			name:     "command bypasses requested tmux",
			args:     []string{"--tmux", "uname", "-a"},
			wantArgs: []string{"-p", "29472", "-o", "StrictHostKeyChecking=accept-new", "rover@rover-vm.example.test", "uname", "-a"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runSSHScript(t, tt.args...)
			if tt.wantTmux {
				if len(got) != len(tt.wantArgs)+1 || !reflect.DeepEqual(got[:len(tt.wantArgs)], tt.wantArgs) {
					t.Fatalf("ssh args = %#v, want prefix %#v plus tmux command", got, tt.wantArgs)
				}
				remote := got[len(got)-1]
				for _, want := range []string{
					"command -v tmux",
					"tmux new-session -A -s rover",
					"tmux is not installed; falling back to a plain shell",
					`exec "${SHELL:-/bin/sh}" -l`,
				} {
					if !strings.Contains(remote, want) {
						t.Errorf("remote tmux command %q does not contain %q", remote, want)
					}
				}
				return
			}
			if !reflect.DeepEqual(got, tt.wantArgs) {
				t.Fatalf("ssh args = %#v, want %#v", got, tt.wantArgs)
			}
		})
	}
}

func runSSHScript(t *testing.T, args ...string) []string {
	t.Helper()
	binDir := t.TempDir()
	capture := filepath.Join(t.TempDir(), "ssh-args")
	writeTestExecutable(t, filepath.Join(binDir, "az"), `#!/usr/bin/env bash
case "$*" in
  *"account show"*) exit 0 ;;
  *"vm get-instance-view"*) printf '%s\n' 'VM running' ;;
  *"vm show"*"hardwareProfile.vmSize"*) printf '%s\n' 'Standard_B2as_v2' ;;
  *"vm show"*) exit 0 ;;
  *"disk show"*) printf '%s\n' '30' ;;
  *"vm list-ip-addresses"*"publicIpAddresses"*) printf '%s\n' '203.0.113.10' ;;
  *"vm list-ip-addresses"*"privateIpAddresses"*) printf '%s\n' '10.0.0.4' ;;
  *"network public-ip show"*) printf '%s\n' 'rover-vm.example.test' ;;
esac
`)
	writeTestExecutable(t, filepath.Join(binDir, "ssh"), `#!/usr/bin/env bash
printf '%s\0' "$@" > "${SSH_CAPTURE}"
`)

	script := filepath.Join("..", "..", "scripts", "azure", "ssh")
	cmd := exec.Command("bash", append([]string{script}, args...)...)
	cmd.Env = append(os.Environ(),
		"PATH="+binDir+":"+os.Getenv("PATH"),
		"SSH_CAPTURE="+capture,
		"ROVER_ADMIN_USER=rover",
		"ROVER_SSH_KEY="+filepath.Join(t.TempDir(), "missing-key"),
		"ROVER_SSH_PORT=29472",
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ssh script: %v\n%s", err, output)
	}
	data, err := os.ReadFile(capture)
	if err != nil {
		t.Fatalf("read captured ssh args: %v", err)
	}
	return strings.Split(strings.TrimSuffix(string(data), "\x00"), "\x00")
}

func writeTestExecutable(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
