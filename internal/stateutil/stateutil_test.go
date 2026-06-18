package stateutil

import (
	"testing"

	"github.com/mitchell-wallace/rover/internal/azure"
	"github.com/mitchell-wallace/rover/internal/config"
)

func newTestState(t *testing.T) *config.State {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	st := config.Default()
	st.AdminUsername = "testuser"
	if err := st.Save(); err != nil {
		t.Fatalf("save state: %v", err)
	}
	return st
}

func TestSyncConnection_SavesState(t *testing.T) {
	st := newTestState(t)

	info := azure.Info{
		Exists:     true,
		PublicIP:   "1.2.3.4",
		FQDN:       "rover-vm.australiaeast.cloudapp.azure.com",
		VMSize:     "Standard_B2als_v2",
		PowerState: "VM running",
	}
	if err := SyncConnection(st, info); err != nil {
		t.Fatalf("syncConnection: %v", err)
	}
	if st.Connection.PublicIP != "1.2.3.4" {
		t.Errorf("Connection.PublicIP = %q, want 1.2.3.4", st.Connection.PublicIP)
	}
	if st.Connection.VMSize != "Standard_B2als_v2" {
		t.Errorf("Connection.VMSize = %q, want Standard_B2als_v2", st.Connection.VMSize)
	}
}

func TestSyncConnection_PreservesVMSize(t *testing.T) {
	st := newTestState(t)

	// Re-asserting a non-empty VM size overwrites the previously recorded size.
	st.Connection.VMSize = "Standard_B2als_v2"
	info := azure.Info{Exists: true, VMSize: "Standard_D4s_v5", PowerState: "VM running"}
	if err := SyncConnection(st, info); err != nil {
		t.Fatalf("syncConnection: %v", err)
	}
	if st.Connection.VMSize != "Standard_D4s_v5" {
		t.Errorf("Connection.VMSize = %q, want Standard_D4s_v5", st.Connection.VMSize)
	}

	// An empty VM size must not clear the mapped value (info drives the snapshot,
	// so the connection carries whatever Azure reported, including empty).
	info = azure.Info{Exists: true, VMSize: "", PowerState: "VM deallocated"}
	if err := SyncConnection(st, info); err != nil {
		t.Fatalf("syncConnection: %v", err)
	}
	if st.Connection.VMSize != "" {
		t.Errorf("Connection.VMSize = %q, want empty", st.Connection.VMSize)
	}
}

func TestZeroConnection(t *testing.T) {
	conn := ZeroConnection()
	if conn.Exists {
		t.Errorf("ZeroConnection().Exists = true, want false")
	}
	if conn != (config.Connection{}) {
		t.Errorf("ZeroConnection() = %+v, want zero-value config.Connection with Exists=false", conn)
	}

	// Resetting a populated connection back to zero mirrors vm.Down(delete=true).
	st := newTestState(t)
	st.Connection = ConnectionFromAzure(azure.Info{
		Exists:     true,
		PublicIP:   "1.2.3.4",
		VMSize:     "Standard_B2als_v2",
		PowerState: "VM running",
	})
	st.Connection = ZeroConnection()
	if st.Connection != (config.Connection{}) {
		t.Errorf("after reset, Connection = %+v, want zero-value", st.Connection)
	}
}
