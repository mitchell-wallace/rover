// Package azure is Rover's boundary to Azure. For the MVP it shells out to the
// scripts in scripts/azure/*, but the surface here (Up/Down/Status/SSH/Info) is
// intentionally script-agnostic so a direct Azure SDK implementation can
// replace the script calls later without touching callers.
package azure

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mitchell-wallace/rover/internal/config"
)

// Info is the connection/status snapshot emitted by the scripts as JSON.
type Info struct {
	Exists        bool   `json:"exists"`
	PowerState    string `json:"powerState"`
	VMName        string `json:"vmName"`
	ResourceGroup string `json:"resourceGroup"`
	Location      string `json:"location"`
	VMSize        string `json:"vmSize"`
	DiskSizeGB    int    `json:"diskSizeGB"`
	AdminUsername string `json:"adminUsername"`
	PublicIP      string `json:"publicIp"`
	FQDN          string `json:"fqdn"`
	PrivateIP     string `json:"privateIp"`
	SSHTarget     string `json:"sshTarget"`
}

// Running reports whether the VM is powered on.
func (i Info) Running() bool {
	return i.Exists && containsFold(i.PowerState, "running")
}

// Host returns the best connection host (FQDN preferred, else public IP).
func (i Info) Host() string {
	if i.FQDN != "" {
		return i.FQDN
	}
	return i.PublicIP
}

// Client runs Azure operations for the given state.
type Client struct {
	state    *config.State
	assetDir string
}

// New builds a Client. assetDir is the materialized asset tree root.
func New(state *config.State, assetDir string) *Client {
	return &Client{state: state, assetDir: assetDir}
}

func (c *Client) scriptPath(name string) string {
	return filepath.Join(c.assetDir, "scripts", "azure", name)
}

func (c *Client) runJSON(script string, args ...string) (Info, error) {
	args = append(args, "--json")
	out, err := c.capture(script, args...)
	if err != nil {
		return Info{}, err
	}
	var info Info
	if err := json.Unmarshal(bytes.TrimSpace(out), &info); err != nil {
		return Info{}, fmt.Errorf("parse %s output: %w", script, err)
	}
	return info, nil
}

func (c *Client) capture(script string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", append([]string{c.scriptPath(script)}, args...)...)
	cmd.Env = c.state.Env()
	cmd.Stderr = os.Stderr
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s: %w", script, err)
	}
	return out.Bytes(), nil
}

func (c *Client) stream(script string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", append([]string{c.scriptPath(script)}, args...)...)
	cmd.Env = c.state.Env()
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", script, err)
	}
	return nil
}

// Up provisions/redeploys the VM at the given family/size and returns its info.
func (c *Client) Up(family, size string) (Info, error) {
	return c.runJSON("up", "--family", family, size)
}

// Down deallocates the VM, or deletes the whole resource group when delete is
// true. Confirmation is the caller's responsibility (pass yes=true).
func (c *Client) Down(del, yes bool) (Info, error) {
	args := []string{}
	if del {
		args = append(args, "--delete")
	}
	if yes {
		args = append(args, "--yes")
	}
	return c.runJSON("down", args...)
}

// Status returns the current VM info.
func (c *Client) Status() (Info, error) {
	return c.runJSON("status")
}

// ResizeDisk grows the OS disk to gb GiB and returns updated info.
func (c *Client) ResizeDisk(gb int) (Info, error) {
	return c.runJSON("disk", strconv.Itoa(gb))
}

// Restart reboots the running VM and returns updated connection info.
func (c *Client) Restart() (Info, error) {
	return c.runJSON("restart")
}

// Info returns the current connection info (alias of status JSON via ip).
func (c *Client) Info() (Info, error) {
	return c.runJSON("ip")
}

// SSH opens an interactive SSH session, passing through any extra args.
func (c *Client) SSH(extra ...string) error {
	return c.stream("ssh", extra...)
}

// SetPublicSSH enables or disables public SSH access on the NSG.
func (c *Client) SetPublicSSH(allowed bool) error {
	action := "allow"
	if !allowed {
		action = "deny"
	}
	return c.stream("ssh-access", action)
}

// RunCommand executes a shell script inside the running VM via Azure Run
// Command. It does not depend on SSH reachability, so it still works after
// Rover has locked public SSH down to Tailscale-only access.
//
// Azure Run Command serializes invocations against a VM: a second invoke while
// a previous one is still executing (including an orphaned one whose local
// `az` process was cancelled — the server-side extension keeps running) is
// rejected with HTTP 409 "(Conflict) Run command extension execution is in
// progress." RunCommand detects that and retries with bounded backoff, so
// transient contention no longer surfaces as a silent repair failure. Guest
// scripts must bound themselves (e.g. via `timeout(1)`) so a wedged process
// inside the VM cannot pin the extension for its ~90 minute script ceiling.
func (c *Client) RunCommand(script string) error {
	return c.runCommand(context.Background(), script, defaultRunCommandPolicy(), defaultRunCommandRunner)
}

// runCommandPolicy governs RunCommand's retry/backoff behaviour.
type runCommandPolicy struct {
	// MaxAttempts is the total number of az invocations (>=1).
	MaxAttempts int
	// AttemptTimeout caps each individual az invocation.
	AttemptTimeout time.Duration
	// Backoff returns the delay before the next attempt, given the number of
	// attempts that have already failed (0-based).
	Backoff func(failedAttempts int) time.Duration
}

func defaultRunCommandPolicy() runCommandPolicy {
	return runCommandPolicy{
		MaxAttempts:    4,
		AttemptTimeout: 5 * time.Minute,
		Backoff: func(failed int) time.Duration {
			// 10s, 20s, 40s ... capped at 60s.
			d := time.Duration(10*(1<<failed)) * time.Second
			if d > 60*time.Second {
				d = 60 * time.Second
			}
			return d
		},
	}
}

// runCommandRunner executes a single az invocation with captured output. The
// default shells out to `az`; tests substitute a fake to exercise
// classification and retry without the real CLI.
type runCommandRunner func(ctx context.Context, env []string, args []string) (stdout, stderr string, err error)

var defaultRunCommandRunner runCommandRunner = func(ctx context.Context, env, args []string) (string, string, error) {
	cmd := exec.CommandContext(ctx, "az", args...)
	cmd.Env = env
	cmd.Stdin = os.Stdin
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	return out.String(), errOut.String(), cmd.Run()
}

func (c *Client) runCommand(ctx context.Context, script string, p runCommandPolicy, runner runCommandRunner) error {
	env := c.state.Env()
	args := c.buildRunCommandArgs(script)

	var (
		lastKind   RunCommandErrorKind
		lastOutput string
		lastErr    error
	)
	for attempt := 1; attempt <= p.MaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return &RunCommandError{Kind: lastKind, Output: lastOutput, Attempts: attempt - 1, Err: err}
		}
		attemptCtx, cancel := context.WithTimeout(ctx, p.AttemptTimeout)
		stdout, stderr, runErr := runner(attemptCtx, env, args)
		cancel()
		output := stdout + stderr

		if runErr == nil {
			return nil
		}

		kind := classifyRunCommandError(stdout, stderr, runErr)
		lastKind, lastOutput, lastErr = kind, output, runErr

		// Only a definitive guest-script failure short-circuits: the script
		// ran (bounded by its own timeout(1)) and returned non-zero, so
		// re-running it will not help. Everything else is retried with bounded
		// backoff. This is deliberate: during Run Command contention — the
		// exact scenario RunCommand exists to ride through — az surfaces a
		// wider menagerie of errors than the clean 409 (throttles, transient
		// deployment errors, mid-LRO failures) that don't all match a neat
		// pattern, and a spurious "give up" is worse for the user than a
		// bounded retry. The caller's context caps the total time, and the
		// classified Kind is still returned so callers can branch on it.
		if kind == KindGuestScriptFailed {
			return &RunCommandError{Kind: kind, Output: output, Attempts: attempt, Err: runErr}
		}
		if attempt == p.MaxAttempts {
			break
		}
		wait := p.Backoff(attempt - 1)
		fmt.Fprintf(os.Stderr, "rover: run-command %s (attempt %d/%d); retrying in %s...\n",
			kind, attempt, p.MaxAttempts, wait)
		if !sleepCtx(ctx, wait) {
			return &RunCommandError{Kind: kind, Output: output, Attempts: attempt, Err: ctx.Err()}
		}
	}
	return &RunCommandError{Kind: lastKind, Output: lastOutput, Attempts: p.MaxAttempts, Err: lastErr}
}

func (c *Client) buildRunCommandArgs(script string) []string {
	args := []string{
		"vm", "run-command", "invoke",
		"-g", c.state.ResourceGroup,
		"-n", c.state.VMName,
		"--command-id", "RunShellScript",
		"--scripts", script,
		"-o", "none",
	}
	if c.state.Subscription != "" {
		args = append(args, "--subscription", c.state.Subscription)
	}
	return args
}

// RunCommandError conveys a classified Run Command failure. Callers use
// errors.As to branch on Kind and surface Output to the user.
type RunCommandError struct {
	Kind     RunCommandErrorKind
	Output   string // combined stdout+stderr captured from az / the guest script
	Attempts int    // number of az invocations performed
	Err      error  // underlying exec error, if any
}

func (e *RunCommandError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("az vm run-command: %s: %v", e.Kind, e.Err)
	}
	return fmt.Sprintf("az vm run-command: %s", e.Kind)
}

func (e *RunCommandError) Unwrap() error { return e.Err }

// RunCommandErrorKind categorizes a Run Command failure so callers can decide
// whether to retry, escalate, or report.
type RunCommandErrorKind string

const (
	// KindConflictBusy marks an Azure 409: another Run Command extension is
	// still executing on the VM.
	KindConflictBusy RunCommandErrorKind = "conflict-busy"
	// KindTransient marks a retriable az-level failure (timeout, network,
	// throttle) and is also the default for unclassified errors observed
	// during Run Command contention.
	KindTransient RunCommandErrorKind = "transient"
	// KindGuestScriptFailed means az completed but the guest script exited
	// non-zero. This is the ONLY kind that short-circuits — RunCommand does
	// not retry it, since the script already ran (bounded by its own
	// timeout(1)) and will just fail the same way again.
	KindGuestScriptFailed RunCommandErrorKind = "guest-script-failed"
	// KindFatal marks an authentication/credential failure. It is still
	// retried like the other non-guest kinds; the distinction exists for
	// caller-side branching and accurate logging.
	KindFatal RunCommandErrorKind = "fatal"
)

// classifyRunCommandError inspects the az invocation output and exec error to
// categorize a Run Command failure. It is pure and table-tested.
//
// RunCommand retries every Kind except KindGuestScriptFailed, so the Kind
// primarily drives logging and caller-side branching rather than retry
// eligibility. The default is KindTransient: during Run Command contention az
// surfaces many transient deployment/serialization errors that don't match a
// clean pattern, and labelling them transient matches how they are treated.
func classifyRunCommandError(stdout, stderr string, err error) RunCommandErrorKind {
	low := strings.ToLower(stderr + "\n" + stdout)
	switch {
	case strings.Contains(low, "run command extension execution is in progress"),
		strings.Contains(low, "code: conflict"),
		strings.Contains(low, "(conflict)"):
		return KindConflictBusy
	case strings.Contains(low, "runcommanderror"),
		strings.Contains(low, "exit code"),
		strings.Contains(low, "script failed"):
		return KindGuestScriptFailed
	case strings.Contains(low, "authorization failed"),
		strings.Contains(low, "authentication failed"),
		strings.Contains(low, "unauthorized"),
		strings.Contains(low, "please run 'az login'"):
		return KindFatal
	case errors.Is(err, context.DeadlineExceeded),
		errors.Is(err, context.Canceled),
		strings.Contains(low, "timeout"),
		strings.Contains(low, "temporarily unavailable"),
		strings.Contains(low, "connection reset"),
		strings.Contains(low, "too many requests"),
		strings.Contains(low, "rate limit"),
		strings.Contains(low, "throttl"),
		strings.Contains(low, "429"):
		return KindTransient
	default:
		return KindTransient
	}
}

// sleepCtx sleeps for d, returning false if ctx is cancelled first.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func containsFold(s, sub string) bool {
	return len(s) >= len(sub) && strings.Contains(
		strings.ToLower(s), strings.ToLower(sub),
	)
}
