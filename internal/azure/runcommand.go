package azure

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// RunCommand executes a shell script inside the running VM via Azure Run
// Command. It does not depend on SSH reachability, so it still works after
// Rover has locked public SSH down to Tailscale-only access.
//
// Azure Run Command serializes invocations against a VM: a second invoke while
// a previous one is still executing (including an orphaned one whose local
// `az` process was cancelled - the server-side extension keeps running) is
// rejected with HTTP 409 "(Conflict) Run command extension execution is in
// progress." RunCommand detects that and retries with bounded backoff, so
// transient contention no longer surfaces as a silent repair failure. Guest
// scripts must bound themselves (e.g. via `timeout(1)`) so a wedged process
// inside the VM cannot pin the extension for its ~90 minute script ceiling.
func (c *Client) RunCommand(ctx context.Context, script string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	return c.runCommand(ctx, script, defaultRunCommandPolicy(), defaultRunCommandRunner)
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
	env, err := c.commandEnv()
	if err != nil {
		return err
	}
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
		// backoff. This is deliberate: during Run Command contention - the
		// exact scenario RunCommand exists to ride through - az surfaces a
		// wider set of errors than the clean 409. A spurious give-up is worse
		// for the user than a bounded retry.
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
	if subscription := c.state.AzureSubscription(); subscription != "" {
		args = append(args, "--subscription", subscription)
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
	// non-zero. This is the only kind that short-circuits.
	KindGuestScriptFailed RunCommandErrorKind = "guest-script-failed"
	// KindFatal marks an authentication/credential failure. It is still
	// retried like the other non-guest kinds; the distinction exists for
	// caller-side branching and accurate logging.
	KindFatal RunCommandErrorKind = "fatal"
)

// classifyRunCommandError inspects the az invocation output and exec error to
// categorize a Run Command failure. It is pure and table-tested.
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
