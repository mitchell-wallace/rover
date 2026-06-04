package cmd

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

func runCommand(args []string) (string, string, error) {
	oldOut := os.Stdout
	oldErr := os.Stderr
	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	os.Stdout = wOut
	os.Stderr = wErr

	rootCmd.SetArgs(args)
	err := rootCmd.Execute()

	_ = wOut.Close()
	_ = wErr.Close()
	os.Stdout = oldOut
	os.Stderr = oldErr

	var outBuf, errBuf bytes.Buffer
	_, _ = io.Copy(&outBuf, rOut)
	_, _ = io.Copy(&errBuf, rErr)

	return outBuf.String(), errBuf.String(), err
}

func TestUpdateDev(t *testing.T) {
	origVersion := version
	defer func() { version = origVersion }()

	version = "dev"

	stdout, stderr, err := runCommand([]string{"update"})
	if err != nil {
		t.Fatalf("expected no error, got %v, stderr: %s", err, stderr)
	}

	if !strings.Contains(stdout, "Current version: dev (cannot check for updates)") {
		t.Errorf("unexpected stdout: %q", stdout)
	}
}

func TestUpdateUpToDate(t *testing.T) {
	origVersion := version
	origFetch := fetchLatestVersionFunc
	defer func() {
		version = origVersion
		fetchLatestVersionFunc = origFetch
	}()

	version = "1.0.0"
	fetchLatestVersionFunc = func() (string, error) {
		return "1.0.0", nil
	}

	stdout, stderr, err := runCommand([]string{"update"})
	if err != nil {
		t.Fatalf("expected no error, got %v, stderr: %s", err, stderr)
	}

	if !strings.Contains(stdout, "You are up to date.") {
		t.Errorf("expected up to date message, got: %q", stdout)
	}
}

func TestUpdateYesSuccessful(t *testing.T) {
	origVersion := version
	origFetch := fetchLatestVersionFunc
	origInstall := installLatestVersionFn
	origUpdateYes := updateYes
	defer func() {
		version = origVersion
		fetchLatestVersionFunc = origFetch
		installLatestVersionFn = origInstall
		updateYes = origUpdateYes
	}()

	version = "0.9.0"
	fetchLatestVersionFunc = func() (string, error) {
		return "1.0.0", nil
	}

	installCalled := false
	installLatestVersionFn = func() error {
		installCalled = true
		return nil
	}

	stdout, stderr, err := runCommand([]string{"update", "--yes"})
	if err != nil {
		t.Fatalf("expected no error, got %v, stderr: %s", err, stderr)
	}

	if !installCalled {
		t.Error("expected installLatestVersion to be called")
	}

	if !strings.Contains(stdout, "Current version: 0.9.0") || !strings.Contains(stdout, "Latest version:  1.0.0") {
		t.Errorf("unexpected stdout: %q", stdout)
	}
}

func TestUpdateYesInstallError(t *testing.T) {
	origVersion := version
	origFetch := fetchLatestVersionFunc
	origInstall := installLatestVersionFn
	origUpdateYes := updateYes
	defer func() {
		version = origVersion
		fetchLatestVersionFunc = origFetch
		installLatestVersionFn = origInstall
		updateYes = origUpdateYes
	}()

	version = "0.9.0"
	fetchLatestVersionFunc = func() (string, error) {
		return "1.0.0", nil
	}

	installLatestVersionFn = func() error {
		return errors.New("install failed error")
	}

	_, _, err := runCommand([]string{"update", "--yes"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !strings.Contains(err.Error(), "install failed error") {
		t.Errorf("expected install failed error, got: %v", err)
	}
}
