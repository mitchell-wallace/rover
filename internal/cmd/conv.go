package cmd

import (
	"github.com/mitchell-wallace/rover/internal/azure"
	"github.com/mitchell-wallace/rover/internal/config"
)

func configConnFrom(info azure.Info) config.Connection {
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

func stateZeroConn() config.Connection {
	return config.Connection{Exists: false}
}
