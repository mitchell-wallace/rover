package vm

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mitchell-wallace/rover/internal/azure"
	"github.com/mitchell-wallace/rover/internal/telemetry"
)

func TestUp_FreshCreateTailscaleNotReadyDeclinedConfirmAborts(t *testing.T) {
	h := newTestHarness(t)
	h.conn.ready = false
	h.az.statusInfo = azure.Info{Exists: false}

	err := h.svc.Up(context.Background(), "burstable", "small", "remembered", false, false)

	requireErrContains(t, err, "configure Tailscale")
	requireEqual(t, len(h.az.upCalls), 0)
	requireEqual(t, h.prov.runCalls, 0)
	requireEqual(t, h.conn.readyCalls, 1)
}

func TestUp_FreshCreateAutoProvisionsUnlessNoProvision(t *testing.T) {
	t.Run("auto provisions", func(t *testing.T) {
		h := newTestHarness(t)
		h.az.statusInfo = azure.Info{Exists: false}

		requireNoErr(t, h.svc.Up(context.Background(), "burstable", "small", "remembered", true, false))

		requireEqual(t, len(h.az.upCalls), 1)
		requireEqual(t, h.az.upCalls[0].family, "burstable")
		requireEqual(t, h.az.upCalls[0].size, "small")
		requireEqual(t, h.prov.runCalls, 1)
		requireEqual(t, h.st.Connection.Exists, true)
	})

	t.Run("skips with no provision", func(t *testing.T) {
		h := newTestHarness(t)
		h.conn.ready = false
		h.az.statusInfo = azure.Info{Exists: false}

		requireNoErr(t, h.svc.Up(context.Background(), "burstable", "small", "remembered", true, true))

		requireEqual(t, len(h.az.upCalls), 1)
		requireEqual(t, h.prov.runCalls, 0)
		requireEqual(t, h.conn.readyCalls, 0)
	})
}

func TestUp_ExistingVMRestoresConnectivity(t *testing.T) {
	h := newTestHarness(t)
	h.az.statusInfo = azure.Info{Exists: true, PowerState: "VM deallocated", VMSize: "Standard_B2as_v2"}

	requireNoErr(t, h.svc.Up(context.Background(), "burstable", "small", "remembered", true, false))

	requireEqual(t, len(h.az.upCalls), 1)
	requireEqual(t, h.conn.restoreCalls, 1)
	requireEqual(t, h.prov.runCalls, 0)
}

func TestUp_ComputeResizeUpdatesOnlySwapfile(t *testing.T) {
	h := newTestHarness(t)
	h.az.statusInfo = azure.Info{Exists: true, PowerState: "VM running", VMSize: "Standard_B2as_v2"}
	h.az.upInfo = azure.Info{Exists: true, PowerState: "VM running", VMSize: "Standard_B4als_v2"}

	requireNoErr(t, h.svc.Up(context.Background(), "burstable", "medium", "remembered", true, false))

	requireEqual(t, h.conn.restoreCalls, 1)
	requireEqual(t, h.prov.resizeSwapfileCalls, 1)
	requireEqual(t, h.prov.runCalls, 0)
}

func TestUp_SameComputeSizeSkipsSwapfileUpdate(t *testing.T) {
	h := newTestHarness(t)
	h.az.statusInfo = runningVMInfo()

	requireNoErr(t, h.svc.Up(context.Background(), "burstable", "small", "remembered", true, false))

	requireEqual(t, h.prov.resizeSwapfileCalls, 0)
}

func TestUp_ComputeResizeSwapfileFailureOffersTargetedRetry(t *testing.T) {
	h := newTestHarness(t)
	h.az.statusInfo = azure.Info{Exists: true, PowerState: "VM running", VMSize: "Standard_B2as_v2"}
	h.prov.resizeSwapfileErr = errors.New("disk full")

	err := h.svc.Up(context.Background(), "burstable", "medium", "remembered", true, false)

	requireErrContains(t, err, "VM resized, but swapfile update failed")
	requireErrContains(t, err, "rover provision --swapfile-only")
}

func TestUpRecordsSelectionProvisioningOutcomeAndClassifiedFailure(t *testing.T) {
	t.Run("successful auto provision", func(t *testing.T) {
		h := newTestHarness(t)
		h.az.statusInfo = azure.Info{Exists: false}

		requireNoErr(t, h.svc.Up(context.Background(), "balanced", "medium", "customized", true, false))

		if len(h.tel.up) != 1 {
			t.Fatalf("up events = %d, want 1", len(h.tel.up))
		}
		got := h.tel.up[0]
		if got.ComputeFamily != "balanced" || got.ComputeSize != "medium" || got.SelectionSource != "customized" || got.ProvisioningOutcome != "succeeded" || !got.Success {
			t.Fatalf("up event = %#v", got)
		}
		if len(h.tel.diagnostics) != 0 {
			t.Fatalf("diagnostics = %#v, want none", h.tel.diagnostics)
		}
	})

	t.Run("swapfile failure is classified", func(t *testing.T) {
		h := newTestHarness(t)
		h.az.statusInfo = azure.Info{Exists: true, PowerState: "VM running", VMSize: "Standard_B2as_v2"}
		h.prov.resizeSwapfileErr = errors.New("provider account /home/private/resource failed")

		err := h.svc.Up(context.Background(), "burstable", "medium", "explicit-flag", true, false)
		requireErrContains(t, err, "swapfile update failed")

		want := []telemetry.DiagnosticEvent{{Command: "up", Category: "swapfile_update_failure"}}
		if len(h.tel.diagnostics) != 1 || h.tel.diagnostics[0] != want[0] {
			t.Fatalf("diagnostics = %#v, want %#v", h.tel.diagnostics, want)
		}
		if strings.Contains(h.tel.diagnostics[0].Category, "private") {
			t.Fatalf("diagnostic leaked raw error: %#v", h.tel.diagnostics[0])
		}
	})
}
