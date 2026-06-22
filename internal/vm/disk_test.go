package vm

import (
	"testing"

	"github.com/mitchell-wallace/rover/internal/azure"
)

func TestDisk_AlreadyCorrectSize_SavesState(t *testing.T) {
	h := newTestHarness(t)
	h.az.statusInfo = azure.Info{Exists: true, DiskSizeGB: 50}

	requireNoErr(t, h.svc.Disk(50, true))

	requireEqual(t, h.st.DiskSizeGB, 50)
	requireEqual(t, len(h.az.resizeDiskCalls), 0)
}

func TestDisk_NoVM_RecordsSize(t *testing.T) {
	h := newTestHarness(t)
	h.az.statusInfo = azure.Info{Exists: false}

	requireNoErr(t, h.svc.Disk(100, true))

	requireEqual(t, h.st.DiskSizeGB, 100)
}

func TestDisk_CannotShrink(t *testing.T) {
	h := newTestHarness(t)
	h.az.statusInfo = azure.Info{Exists: true, DiskSizeGB: 100}

	err := h.svc.Disk(50, true)

	requireErrContains(t, err, "cannot shrink")
	requireEqual(t, len(h.az.resizeDiskCalls), 0)
}

func TestDisk_MinimumSize(t *testing.T) {
	h := newTestHarness(t)

	err := h.svc.Disk(10, true)

	requireErrContains(t, err, "at least 30 GiB")
	requireEqual(t, h.az.statusCalls, 0)
}
