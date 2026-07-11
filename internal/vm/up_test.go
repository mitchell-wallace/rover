package vm

import (
	"context"
	"errors"
	"testing"

	"github.com/mitchell-wallace/rover/internal/azure"
)

func TestUp_FreshCreateTailscaleNotReadyDeclinedConfirmAborts(t *testing.T) {
	h := newTestHarness(t)
	h.conn.ready = false
	h.az.statusInfo = azure.Info{Exists: false}

	err := h.svc.Up(context.Background(), "burstable", "small", false, false)

	requireErrContains(t, err, "configure Tailscale")
	requireEqual(t, len(h.az.upCalls), 0)
	requireEqual(t, h.prov.runCalls, 0)
	requireEqual(t, h.conn.readyCalls, 1)
}

func TestUp_FreshCreateAutoProvisionsUnlessNoProvision(t *testing.T) {
	t.Run("auto provisions", func(t *testing.T) {
		h := newTestHarness(t)
		h.az.statusInfo = azure.Info{Exists: false}

		requireNoErr(t, h.svc.Up(context.Background(), "burstable", "small", true, false))

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

		requireNoErr(t, h.svc.Up(context.Background(), "burstable", "small", true, true))

		requireEqual(t, len(h.az.upCalls), 1)
		requireEqual(t, h.prov.runCalls, 0)
		requireEqual(t, h.conn.readyCalls, 0)
	})
}

func TestUp_ExistingVMRestoresConnectivity(t *testing.T) {
	h := newTestHarness(t)
	h.az.statusInfo = azure.Info{Exists: true, PowerState: "VM deallocated", VMSize: "Standard_B2as_v2"}

	requireNoErr(t, h.svc.Up(context.Background(), "burstable", "small", true, false))

	requireEqual(t, len(h.az.upCalls), 1)
	requireEqual(t, h.conn.restoreCalls, 1)
	requireEqual(t, h.prov.runCalls, 0)
}

func TestUp_ComputeResizeUpdatesOnlySwapfile(t *testing.T) {
	h := newTestHarness(t)
	h.az.statusInfo = azure.Info{Exists: true, PowerState: "VM running", VMSize: "Standard_B2as_v2"}
	h.az.upInfo = azure.Info{Exists: true, PowerState: "VM running", VMSize: "Standard_B4als_v2"}

	requireNoErr(t, h.svc.Up(context.Background(), "burstable", "medium", true, false))

	requireEqual(t, h.conn.restoreCalls, 1)
	requireEqual(t, h.prov.resizeSwapfileCalls, 1)
	requireEqual(t, h.prov.runCalls, 0)
}

func TestUp_SameComputeSizeSkipsSwapfileUpdate(t *testing.T) {
	h := newTestHarness(t)
	h.az.statusInfo = runningVMInfo()

	requireNoErr(t, h.svc.Up(context.Background(), "burstable", "small", true, false))

	requireEqual(t, h.prov.resizeSwapfileCalls, 0)
}

func TestUp_ComputeResizeSwapfileFailureOffersTargetedRetry(t *testing.T) {
	h := newTestHarness(t)
	h.az.statusInfo = azure.Info{Exists: true, PowerState: "VM running", VMSize: "Standard_B2as_v2"}
	h.prov.resizeSwapfileErr = errors.New("disk full")

	err := h.svc.Up(context.Background(), "burstable", "medium", true, false)

	requireErrContains(t, err, "VM resized, but swapfile update failed")
	requireErrContains(t, err, "rover provision --swapfile-only")
}
