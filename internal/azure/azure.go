// Package azure is Rover's boundary to Azure. For the MVP it shells out to the
// scripts in scripts/azure/*, but the surface here (Up/Down/Status/SSH/Info) is
// intentionally script-agnostic so a direct Azure SDK implementation can
// replace the script calls later without touching callers.
package azure

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"github.com/mitchell-wallace/rover/internal/config"
)

// Info is the connection/status snapshot emitted by the scripts as JSON.
type Info struct {
	Exists        bool   `json:"exists"`
	PowerState    string `json:"powerState"`
	VMName        string `json:"vmName"`
	ResourceGroup string `json:"resourceGroup"`
	Location      string `json:"location"`
	VMSize        string `json:"vmSize"`
	DiskSizeGB    int    `json:"diskSizeGB"`
	AdminUsername string `json:"adminUsername"`
	PublicIP      string `json:"publicIp"`
	FQDN          string `json:"fqdn"`
	PrivateIP     string `json:"privateIp"`
	SSHTarget     string `json:"sshTarget"`
}

// Running reports whether the VM is powered on.
func (i Info) Running() bool {
	return i.Exists && containsFold(i.PowerState, "running")
}

// Host returns the best connection host (FQDN preferred, else public IP).
func (i Info) Host() string {
	if i.FQDN != "" {
		return i.FQDN
	}
	return i.PublicIP
}

// Client runs Azure operations for the given state.
type Client struct {
	state    *config.State
	assetDir string
}

// New builds a Client. assetDir is the materialized asset tree root.
func New(state *config.State, assetDir string) *Client {
	return &Client{state: state, assetDir: assetDir}
}

func (c *Client) scriptPath(name string) string {
	return filepath.Join(c.assetDir, "scripts", "azure", name)
}

// runJSON executes a script with --json and decodes the Info result.
func (c *Client) runJSON(script string, args ...string) (Info, error) {
	args = append(args, "--json")
	out, err := c.capture(script, args...)
	if err != nil {
		return Info{}, err
	}
	var info Info
	if err := json.Unmarshal(bytes.TrimSpace(out), &info); err != nil {
		return Info{}, fmt.Errorf("parse %s output: %w", script, err)
	}
	return info, nil
}

// capture runs a script, streaming stderr to the user, returning stdout.
func (c *Client) capture(script string, args ...string) ([]byte, error) {
	cmd := exec.Command("bash", append([]string{c.scriptPath(script)}, args...)...)
	cmd.Env = c.state.Env()
	cmd.Stderr = os.Stderr
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s: %w", script, err)
	}
	return out.Bytes(), nil
}

// stream runs a script with full stdio inheritance (interactive).
func (c *Client) stream(script string, args ...string) error {
	cmd := exec.Command("bash", append([]string{c.scriptPath(script)}, args...)...)
	cmd.Env = c.state.Env()
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", script, err)
	}
	return nil
}

// Up provisions/redeploys the VM at the given size and returns its info.
func (c *Client) Up(size string) (Info, error) {
	return c.runJSON("up", size)
}

// Down deallocates the VM, or deletes the whole resource group when delete is
// true. Confirmation is the caller's responsibility (pass yes=true).
func (c *Client) Down(delete, yes bool) (Info, error) {
	args := []string{}
	if delete {
		args = append(args, "--delete")
	}
	if yes {
		args = append(args, "--yes")
	}
	return c.runJSON("down", args...)
}

// Status returns the current VM info.
func (c *Client) Status() (Info, error) {
	return c.runJSON("status")
}

// ResizeDisk grows the OS disk to gb GiB and returns updated info.
func (c *Client) ResizeDisk(gb int) (Info, error) {
	return c.runJSON("disk", strconv.Itoa(gb))
}

// Info returns the current connection info (alias of status JSON via ip).
func (c *Client) Info() (Info, error) {
	return c.runJSON("ip")
}

// SSH opens an interactive SSH session, passing through any extra args.
func (c *Client) SSH(extra ...string) error {
	return c.stream("ssh", extra...)
}

func containsFold(s, sub string) bool {
	return len(s) >= len(sub) && bytes.Contains(bytes.ToLower([]byte(s)), bytes.ToLower([]byte(sub)))
}
