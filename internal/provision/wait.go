package provision

import (
	"context"
	"net"
	"strconv"
	"time"

	"github.com/mitchell-wallace/rover/internal/ui"
)

func defaultSSHWait(ctx context.Context, host string, port int) {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	deadline := time.Now().Add(5 * time.Minute)
	announced := false
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return
		default:
		}
		dialer := net.Dialer{Timeout: 5 * time.Second}
		conn, err := dialer.DialContext(ctx, "tcp", addr)
		if err == nil {
			_ = conn.Close()
			if announced {
				ui.Info("SSH is up.")
			}
			return
		}
		if !announced {
			ui.Info("Waiting for SSH on port %d (the VM may still be booting)...", port)
			announced = true
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}
