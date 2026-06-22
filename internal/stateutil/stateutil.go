// Package stateutil holds the shared helpers that map Azure status info onto
// Rover's persisted config.Connection and sync it to state. Service packages
// (vm, provision) depend on this instead of cmd, keeping the dependency graph
// acyclic: stateutil depends only on azure and config.
package stateutil

import (
	"github.com/mitchell-wallace/rover/internal/azure"
	"github.com/mitchell-wallace/rover/internal/config"
)

// ConnectionFromAzure maps an Azure status snapshot onto a config.Connection.
func ConnectionFromAzure(info azure.Info) config.Connection {
	return config.Connection{
		Exists:     info.Exists,
		PowerState: info.PowerState,
		VMSize:     info.VMSize,
		PublicIP:   info.PublicIP,
		FQDN:       info.FQDN,
		PrivateIP:  info.PrivateIP,
		SSHTarget:  info.SSHTarget,
	}
}

// ZeroConnection returns a connection snapshot representing no VM.
func ZeroConnection() config.Connection {
	return config.Connection{Exists: false}
}

// SyncConnection records the latest Azure info onto the persisted state and
// saves it. The VM size is re-asserted when non-empty (ConnectionFromAzure
// already sets it, so the result is unchanged, but the behaviour is preserved
// verbatim from the original appContext.syncConnection).
func SyncConnection(st *config.State, info azure.Info) error {
	st.Connection = ConnectionFromAzure(info)
	if info.VMSize != "" {
		st.Connection.VMSize = info.VMSize
	}
	return st.Save()
}
