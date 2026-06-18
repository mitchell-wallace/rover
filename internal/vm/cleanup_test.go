package vm

import (
	"io"
	"os"
	"testing"

	"github.com/mitchell-wallace/rover/internal/tailscale"
)

func TestCleanupTailscaleDevices_NoOAuthError(t *testing.T) {
	h := newTestHarness(t)
	h.st.TailscaleClientID = ""
	h.st.TailscaleClientSecret = ""

	_, err := h.svc.CleanupTailscaleDevices(false, false)

	requireErrContains(t, err, "OAuth credentials not configured")
	requireEqual(t, len(h.ts.cleanupCalls), 0)
}

func TestCleanupTailscaleDevices_DryRunPrintsResult(t *testing.T) {
	h := newTestHarness(t)
	h.ts.cleanupResult = tailscale.CleanupResult{
		Matched: []tailscale.Device{
			{Name: "rover-vm.tailnet.ts.net", Hostname: "rover-vm"},
			{Name: "rover-vm-old.tailnet.ts.net", Hostname: "rover-vm-old", ConnectedToControl: true},
		},
		WouldDelete: []tailscale.Device{{Name: "rover-vm.tailnet.ts.net", Hostname: "rover-vm"}},
		Skipped:     []tailscale.Device{{Name: "rover-vm-old.tailnet.ts.net", Hostname: "rover-vm-old", ConnectedToControl: true}},
	}

	var err error
	out := captureStderr(t, func() {
		_, err = h.svc.CleanupTailscaleDevices(false, true)
	})

	requireNoErr(t, err)
	requireEqual(t, len(h.ts.cleanupCalls), 1)
	requireEqual(t, h.ts.cleanupCalls[0].dryRun, true)
	requireContains(t, out, "Would delete Tailscale device: rover-vm.tailnet.ts.net")
	requireContains(t, out, "Would skip online Tailscale device: rover-vm-old.tailnet.ts.net")
	requireContains(t, out, "matched=2 deleted=0 would-delete=1 skipped=1")
}

func TestCleanupTailscaleDevices_PropagatesDeleteOnline(t *testing.T) {
	h := newTestHarness(t)

	_, err := h.svc.CleanupTailscaleDevices(true, false)

	requireNoErr(t, err)
	requireEqual(t, len(h.ts.cleanupCalls), 1)
	requireEqual(t, h.ts.cleanupCalls[0].clientID, "fake-client-id")
	requireEqual(t, h.ts.cleanupCalls[0].secret, "fake-client-secret")
	requireEqual(t, h.ts.cleanupCalls[0].hostname, "rover-vm")
	requireEqual(t, h.ts.cleanupCalls[0].deleteOnline, true)
	requireEqual(t, h.ts.cleanupCalls[0].dryRun, false)
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stderr: %v", err)
	}
	os.Stderr = w
	fn()
	if err := w.Close(); err != nil {
		t.Fatalf("close stderr pipe: %v", err)
	}
	os.Stderr = old
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read stderr: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("close stderr reader: %v", err)
	}
	return string(out)
}
