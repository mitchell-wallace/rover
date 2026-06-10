// Package azure is Rover's boundary to Azure. For the MVP it shells out to the
// scripts in scripts/azure/*, but the surface here (Up/Down/Status/SSH/Info) is
// intentionally script-agnostic so a direct Azure SDK implementation can
// replace the script calls later without touching callers.
package azure

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mitchell-wallace/rover/internal/config"
)

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

func (i Info) Running() bool {
	return i.Exists && containsFold(i.PowerState, "running")
}

func (i Info) Host() string {
	if i.FQDN != "" {
		return i.FQDN
	}
	return i.PublicIP
}

type Client struct {
	state    *config.State
	assetDir string
}

func New(state *config.State, assetDir string) *Client {
	return &Client{state: state, assetDir: assetDir}
}

func (c *Client) scriptPath(name string) string {
	return filepath.Join(c.assetDir, "scripts", "azure", name)
}

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

func (c *Client) capture(script string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", append([]string{c.scriptPath(script)}, args...)...)
	cmd.Env = c.state.Env()
	cmd.Stderr = os.Stderr
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s: %w", script, err)
	}
	return out.Bytes(), nil
}

func (c *Client) stream(script string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", append([]string{c.scriptPath(script)}, args...)...)
	cmd.Env = c.state.Env()
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", script, err)
	}
	return nil
}

func (c *Client) Up(family, size string) (Info, error) {
	return c.runJSON("up", "--family", family, size)
}

func (c *Client) Down(del, yes bool) (Info, error) {
	args := []string{}
	if del {
		args = append(args, "--delete")
	}
	if yes {
		args = append(args, "--yes")
	}
	return c.runJSON("down", args...)
}

func (c *Client) Status() (Info, error) {
	return c.runJSON("status")
}

func (c *Client) ResizeDisk(gb int) (Info, error) {
	return c.runJSON("disk", strconv.Itoa(gb))
}

func (c *Client) Info() (Info, error) {
	return c.runJSON("ip")
}

func (c *Client) SSH(extra ...string) error {
	return c.stream("ssh", extra...)
}

func (c *Client) SetPublicSSH(allowed bool) error {
	action := "allow"
	if !allowed {
		action = "deny"
	}
	return c.stream("ssh-access", action)
}

func (c *Client) RunCommand(script string) error {
	args := []string{
		"vm", "run-command", "invoke",
		"-g", c.state.ResourceGroup,
		"-n", c.state.VMName,
		"--command-id", "RunShellScript",
		"--scripts", script,
		"-o", "none",
	}
	if c.state.Subscription != "" {
		args = append(args, "--subscription", c.state.Subscription)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "az", args...)
	cmd.Env = c.state.Env()
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("az vm run-command invoke: %w", err)
	}
	return nil
}

func containsFold(s, sub string) bool {
	return len(s) >= len(sub) && strings.Contains(
		strings.ToLower(s), strings.ToLower(sub),
	)
}
