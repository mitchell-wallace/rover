package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mitchell-wallace/rover/internal/telemetry"
)

type telemetryCommandResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

func TestCommandBehaviorMatchesWithAndWithoutTelemetryKey(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "help", args: []string{"--help"}},
		{name: "version", args: []string{"--version"}},
		{name: "unknown command", args: []string{"not-a-command"}},
		{name: "invalid up arguments", args: []string{"up", "small", "large"}},
	}
	for _, command := range []string{
		"command", "completion", "config", "connect", "disk", "doctor", "down", "init", "login", "logout",
		"provision", "restart", "ssh", "status", "tailscale", "up", "update", "version",
	} {
		tests = append(tests, struct {
			name string
			args []string
		}{name: command + " help", args: []string{command, "--help"}})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			without := captureTelemetryCommand(t, tt.args, "")
			with := captureTelemetryCommand(t, tt.args, strings.Repeat("1", 40))
			if !reflect.DeepEqual(without, with) {
				t.Fatalf("command behavior differs without vs with telemetry:\nwithout: %#v\nwith:    %#v", without, with)
			}
		})
	}
}

func captureTelemetryCommand(t *testing.T, args []string, license string) telemetryCommandResult {
	t.Helper()
	encodedArgs, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(os.Args[0], "-test.run=^TestTelemetryParityHelperProcess$")
	command.Env = append(os.Environ(),
		"ROVER_TELEMETRY_PARITY_HELPER=1",
		"ROVER_TELEMETRY_PARITY_ARGS="+string(encodedArgs),
		"ROVER_TELEMETRY_PARITY_LICENSE="+license,
	)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err = command.Run()
	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("run helper: %v", err)
		}
		exitCode = exitErr.ExitCode()
	}
	return telemetryCommandResult{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: exitCode}
}

func TestTelemetryParityHelperProcess(t *testing.T) {
	if os.Getenv("ROVER_TELEMETRY_PARITY_HELPER") != "1" {
		return
	}
	var args []string
	if err := json.Unmarshal([]byte(os.Getenv("ROVER_TELEMETRY_PARITY_ARGS")), &args); err != nil {
		os.Exit(2)
	}
	if err := os.Setenv("ROVER_NEW_RELIC_LICENSE_KEY", os.Getenv("ROVER_TELEMETRY_PARITY_LICENSE")); err != nil {
		os.Exit(2)
	}
	if err := os.Setenv("ROVER_NEW_RELIC_APP_NAME", "Rover Parity Test"); err != nil {
		os.Exit(2)
	}
	rootCmd.SetArgs(args)
	if err := executeWithTelemetry("test", telemetry.Config{ShutdownTimeout: time.Millisecond}); err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}
