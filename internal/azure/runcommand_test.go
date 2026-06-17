package azure

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mitchell-wallace/rover/internal/config"
)

type runnerResp struct {
	stdout string
	stderr string
	err    error
}

// sequenceRunner returns canned az responses in order, repeating the last one
// for any extra calls. The returned func reports how many invocations occurred.
func sequenceRunner(responses ...runnerResp) (runCommandRunner, func() int) {
	calls := 0
	runner := func(_ context.Context, _, _ []string) (string, string, error) {
		idx := calls
		calls++
		if idx >= len(responses) {
			idx = len(responses) - 1
		}
		r := responses[idx]
		return r.stdout, r.stderr, r.err
	}
	return runner, func() int { return calls }
}

func newTestClient(t *testing.T) *Client {
	t.Helper()
	return &Client{state: &config.State{ResourceGroup: "test-rg", VMName: "test-vm"}, assetDir: t.TempDir()}
}

func fastPolicy(maxAttempts int) runCommandPolicy {
	return runCommandPolicy{
		MaxAttempts:    maxAttempts,
		AttemptTimeout: time.Second,
		Backoff:        func(int) time.Duration { return time.Millisecond },
	}
}

func TestClassifyRunCommandError(t *testing.T) {
	azErr := errors.New("az exited 1")
	tests := []struct {
		name   string
		stdout string
		stderr string
		err    error
		want   RunCommandErrorKind
	}{
		{
			name:   "conflict busy (exact Azure message observed in production)",
			stderr: "(Conflict) Run command extension execution is in progress. Please wait for completion before invoking a run command.\nCode: Conflict\nMessage: Run command extension execution is in progress. Please wait for completion before invoking a run command.",
			err:    azErr,
			want:   KindConflictBusy,
		},
		{name: "conflict via code marker", stderr: "prefix Code: Conflict trailing", err: azErr, want: KindConflictBusy},
		{name: "conflict via (Conflict) prefix", stderr: "(Conflict) something else", err: azErr, want: KindConflictBusy},
		{name: "guest script failed via exit code", stdout: "Usage: tailscale up ...\nexit code 1", err: azErr, want: KindGuestScriptFailed},
		{name: "guest script failed via RunCommandError code", stderr: `{"status":"Failed","error":{"code":"VMRunCommandError"}}`, err: azErr, want: KindGuestScriptFailed},
		{name: "transient via deadline exceeded", stderr: "x", err: context.DeadlineExceeded, want: KindTransient},
		{name: "transient via timeout text", stderr: "operation timeout reached", err: azErr, want: KindTransient},
		{name: "transient via 429 throttle", stderr: "Too many requests. Please try again later. (429)", err: azErr, want: KindTransient},
		{name: "fatal via auth marker", stderr: "Authorization failed: invalid subscription", err: azErr, want: KindFatal},
		{name: "fatal via az-login prompt", stderr: "Please run 'az login' to setup account.", err: azErr, want: KindFatal},
		{name: "transient default (unclassified)", stderr: "resource not found / ambiguous deployment error", err: azErr, want: KindTransient},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyRunCommandError(tt.stdout, tt.stderr, tt.err); got != tt.want {
				t.Errorf("classifyRunCommandError() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRunCommandSuccessFirstTry(t *testing.T) {
	c := newTestClient(t)
	runner, calls := sequenceRunner(runnerResp{}) // no err = success
	if err := c.runCommand(context.Background(), "echo hi", fastPolicy(3), runner); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if got := calls(); got != 1 {
		t.Errorf("expected 1 invocation, got %d", got)
	}
}

func TestRunCommandRetriesConflictThenSucceeds(t *testing.T) {
	c := newTestClient(t)
	runner, calls := sequenceRunner(
		runnerResp{stderr: "Run command extension execution is in progress", err: errors.New("409")},
		runnerResp{}, // success on retry
	)
	if err := c.runCommand(context.Background(), "script", fastPolicy(3), runner); err != nil {
		t.Fatalf("expected recovery after retry, got %v", err)
	}
	if got := calls(); got != 2 {
		t.Errorf("expected 2 invocations (1 conflict + 1 success), got %d", got)
	}
}

func TestRunCommandExhaustsConflictRetries(t *testing.T) {
	c := newTestClient(t)
	runner, calls := sequenceRunner(
		runnerResp{stderr: "Run command extension execution is in progress", err: errors.New("409")},
	)
	err := c.runCommand(context.Background(), "script", fastPolicy(3), runner)
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	var rcErr *RunCommandError
	if !errors.As(err, &rcErr) {
		t.Fatalf("expected *RunCommandError, got %T: %v", err, err)
	}
	if rcErr.Kind != KindConflictBusy {
		t.Errorf("Kind = %q, want %q", rcErr.Kind, KindConflictBusy)
	}
	if rcErr.Attempts != 3 {
		t.Errorf("Attempts = %d, want 3", rcErr.Attempts)
	}
	if got := calls(); got != 3 {
		t.Errorf("expected 3 invocations, got %d", got)
	}
}

func TestRunCommandGuestFailureDoesNotRetry(t *testing.T) {
	c := newTestClient(t)
	runner, calls := sequenceRunner(
		runnerResp{stdout: "tailscale up: invalid auth key\nexit code 1", err: errors.New("script failed")},
	)
	err := c.runCommand(context.Background(), "script", fastPolicy(3), runner)
	if err == nil {
		t.Fatal("expected guest-failure error")
	}
	var rcErr *RunCommandError
	if !errors.As(err, &rcErr) {
		t.Fatalf("expected *RunCommandError, got %T: %v", err, err)
	}
	if rcErr.Kind != KindGuestScriptFailed {
		t.Errorf("Kind = %q, want %q", rcErr.Kind, KindGuestScriptFailed)
	}
	if rcErr.Attempts != 1 {
		t.Errorf("Attempts = %d, want 1 (guest failures must not retry)", rcErr.Attempts)
	}
	if !strings.Contains(rcErr.Output, "invalid auth key") {
		t.Errorf("expected Output to surface guest message, got %q", rcErr.Output)
	}
	if got := calls(); got != 1 {
		t.Errorf("expected 1 invocation (no retry), got %d", got)
	}
}

func TestRunCommandFatalStillRetried(t *testing.T) {
	// Fatal errors (auth, not-found, ambiguous) are still retried with bounded
	// backoff: during Run Command contention az surfaces many errors that don't
	// match a clean pattern, and a spurious give-up is worse than a bounded
	// retry. Only KindGuestScriptFailed short-circuits. The caller's context
	// caps the total cost.
	c := newTestClient(t)
	runner, calls := sequenceRunner(
		runnerResp{stderr: "ERROR: Authorization failed.", err: errors.New("az exit 1")},
	)
	err := c.runCommand(context.Background(), "script", fastPolicy(3), runner)
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	var rcErr *RunCommandError
	if !errors.As(err, &rcErr) {
		t.Fatalf("expected *RunCommandError, got %T: %v", err, err)
	}
	if rcErr.Kind != KindFatal {
		t.Errorf("Kind = %q, want %q", rcErr.Kind, KindFatal)
	}
	if rcErr.Attempts != 3 {
		t.Errorf("Attempts = %d, want 3 (fatal is retried; only guest-fail short-circuits)", rcErr.Attempts)
	}
	if got := calls(); got != 3 {
		t.Errorf("expected 3 invocations, got %d", got)
	}
}

func TestRunCommandContextCancelDuringBackoff(t *testing.T) {
	c := newTestClient(t)
	runner, calls := sequenceRunner(
		runnerResp{stderr: "Run command extension execution is in progress", err: errors.New("409")},
	)
	// Long backoff so the cancel fires while the sleep is in flight.
	policy := runCommandPolicy{
		MaxAttempts:    3,
		AttemptTimeout: time.Second,
		Backoff:        func(int) time.Duration { return 10 * time.Second },
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	err := c.runCommand(ctx, "script", policy, runner)
	if err == nil {
		t.Fatal("expected error on cancel")
	}
	var rcErr *RunCommandError
	if !errors.As(err, &rcErr) {
		t.Fatalf("expected *RunCommandError, got %T: %v", err, err)
	}
	if !errors.Is(rcErr.Err, context.Canceled) {
		t.Errorf("expected wrapped context.Canceled, got %v", rcErr.Err)
	}
	if got := calls(); got != 1 {
		t.Errorf("expected 1 invocation before cancel, got %d", got)
	}
}

func TestRunCommandContextCancelledBeforeFirstAttempt(t *testing.T) {
	c := newTestClient(t)
	runner, calls := sequenceRunner(runnerResp{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := c.runCommand(ctx, "script", fastPolicy(3), runner)
	if err == nil {
		t.Fatal("expected error when ctx pre-cancelled")
	}
	if got := calls(); got != 0 {
		t.Errorf("expected 0 invocations, got %d", got)
	}
}

func TestSleepCtx(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if sleepCtx(ctx, 5*time.Second) {
		t.Error("cancelled ctx should return false immediately")
	}
	if !sleepCtx(context.Background(), 5*time.Millisecond) {
		t.Error("live ctx with short sleep should return true")
	}
	if !sleepCtx(context.Background(), 0) {
		t.Error("zero duration on live ctx should return true")
	}
}

func TestDefaultRunCommandPolicyBackoff(t *testing.T) {
	p := defaultRunCommandPolicy()
	cases := map[int]time.Duration{
		0:  10 * time.Second,
		1:  20 * time.Second,
		2:  40 * time.Second,
		3:  60 * time.Second, // capped
		10: 60 * time.Second,
	}
	for failed, want := range cases {
		if got := p.Backoff(failed); got != want {
			t.Errorf("Backoff(%d) = %s, want %s", failed, got, want)
		}
	}
	if p.MaxAttempts < 2 {
		t.Errorf("MaxAttempts = %d, want >= 2", p.MaxAttempts)
	}
	if p.AttemptTimeout < time.Minute {
		t.Errorf("AttemptTimeout = %s, want >= 1m", p.AttemptTimeout)
	}
}
