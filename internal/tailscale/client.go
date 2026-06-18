package tailscale

// Client is the provider-side seam that consumers (connectivity, provision, vm)
// depend on instead of reaching for the package-level funcs directly. The
// default implementation is CLI, returned by NewClient; tests inject their own
// implementation.
//
// The interface lives in package tailscale (rather than in a consumer package)
// so that the Peer and CleanupResult result types do not force an import cycle:
// connectivity imports tailscale for those types, so tailscale must not import
// connectivity.
type Client interface {
	FindPeer(host string) (*Peer, error)
	PingPeer(p *Peer) bool
	GetAuthKey(clientID, secret string, tags []string) (string, error)
	Connect(user, host string, extra ...string) error
	CleanupDevices(clientID, secret string, tags []string, hostname string, deleteOnline, dryRun bool) (CleanupResult, error)
}

// CLI is the default Client implementation. Each method is a thin delegation to
// the existing package-level funcs in tailscale.go; no behavior is added or
// changed.
type CLI struct{}

// NewClient returns the default Client backed by the local `tailscale` CLI and
// the Tailscale control-plane API.
func NewClient() Client { return CLI{} }

// FindPeer delegates to FindPeer.
func (CLI) FindPeer(host string) (*Peer, error) { return FindPeer(host) }

// PingPeer delegates to PingPeer.
func (CLI) PingPeer(p *Peer) bool { return PingPeer(p) }

// GetAuthKey delegates to GetAuthKey.
func (CLI) GetAuthKey(clientID, secret string, tags []string) (string, error) {
	return GetAuthKey(clientID, secret, tags)
}

// Connect delegates to Connect.
func (CLI) Connect(user, host string, extra ...string) error { return Connect(user, host, extra...) }

// CleanupDevices delegates to CleanupDevices.
func (CLI) CleanupDevices(clientID, secret string, tags []string, hostname string, deleteOnline, dryRun bool) (CleanupResult, error) {
	return CleanupDevices(clientID, secret, tags, hostname, deleteOnline, dryRun)
}
