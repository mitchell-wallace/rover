// Package tailscale is Rover's local-side Tailscale integration: it inspects the
// tailnet via the `tailscale` CLI to find the Rover VM and connects to it over
// Tailscale SSH, independent of the Azure public IP.
package tailscale

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// Peer is the subset of `tailscale status --json` we care about.
type Peer struct {
	HostName     string   `json:"HostName"`
	DNSName      string   `json:"DNSName"`
	Online       bool     `json:"Online"`
	TailscaleIPs []string `json:"TailscaleIPs"`
}

type statusJSON struct {
	BackendState string           `json:"BackendState"`
	Peer         map[string]*Peer `json:"Peer"`
}

// Device is the subset of the Tailscale API device response Rover needs for
// cleanup.
type Device struct {
	ID                 string   `json:"id"`
	NodeID             string   `json:"nodeId"`
	Name               string   `json:"name"`
	Hostname           string   `json:"hostname"`
	Tags               []string `json:"tags"`
	ConnectedToControl bool     `json:"connectedToControl"`
	IsExternal         bool     `json:"isExternal"`
}

// CleanupResult describes a Tailscale cleanup run.
type CleanupResult struct {
	Matched     []Device
	Deleted     []Device
	WouldDelete []Device
	Skipped     []Device
}

var apiClient = &http.Client{Timeout: 20 * time.Second}

// Available reports whether the local `tailscale` CLI is installed.
func Available() bool {
	_, err := exec.LookPath("tailscale")
	return err == nil
}

// ErrNotInstalled is returned when the tailscale CLI is missing.
var ErrNotInstalled = fmt.Errorf("tailscale CLI not found; install it from https://tailscale.com/download and run 'tailscale up'")

// ErrNotRunning is returned when the local tailscale backend isn't connected.
var ErrNotRunning = fmt.Errorf("local Tailscale is not connected; run 'tailscale up'")

// PeerNotFoundError indicates the named host isn't in the tailnet.
type PeerNotFoundError struct{ Host string }

func (e *PeerNotFoundError) Error() string {
	return fmt.Sprintf("%q is not in your tailnet", e.Host)
}

// FindPeer returns the tailnet peer matching host (by short hostname or the
// leading label of its MagicDNS name).
func FindPeer(host string) (*Peer, error) {
	if !Available() {
		return nil, ErrNotInstalled
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "tailscale", "status", "--json").Output()
	if err != nil {
		return nil, fmt.Errorf("tailscale status: %w", err)
	}
	var st statusJSON
	if err := json.Unmarshal(out, &st); err != nil {
		return nil, fmt.Errorf("parse tailscale status: %w", err)
	}
	if st.BackendState != "Running" {
		return nil, ErrNotRunning
	}
	want := strings.ToLower(host)
	var online, offline []*Peer
	for _, p := range st.Peer {
		if !matchesPeer(p, want) {
			continue
		}
		if p.Online {
			online = append(online, p)
		} else {
			offline = append(offline, p)
		}
	}
	sort.Slice(online, func(i, j int) bool { return online[i].HostName < online[j].HostName })
	sort.Slice(offline, func(i, j int) bool { return offline[i].HostName < offline[j].HostName })
	if len(online) > 0 {
		return online[0], nil
	}
	if len(offline) > 0 {
		return offline[0], nil
	}
	return nil, &PeerNotFoundError{Host: host}
}
func matchesPeer(p *Peer, want string) bool {
	if p == nil {
		return false
	}
	return strings.EqualFold(p.HostName, want) || strings.HasPrefix(strings.ToLower(p.DNSName), want+".")
}

// Target returns the best address to connect to (MagicDNS name, else IP).
func (p *Peer) Target() string {
	if p.DNSName != "" {
		return strings.TrimSuffix(p.DNSName, ".")
	}
	if len(p.TailscaleIPs) > 0 {
		return p.TailscaleIPs[0]
	}
	return p.HostName
}

// PingPeer checks whether the peer is reachable on the Tailscale data plane.
// The control-plane Online bit can be stale or optimistic after VM restarts, so
// callers use this before trusting the peer for SSH.
func PingPeer(p *Peer) bool {
	if p == nil || !p.Online {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "tailscale", "ping", "--timeout=3s", "--c", "1", p.Target()).Run() == nil
}

// Connect opens an interactive Tailscale SSH session to user@host's peer.
func Connect(user, host string, extra ...string) error {
	if !Available() {
		return ErrNotInstalled
	}
	args := append([]string{"ssh", user + "@" + host}, extra...)
	cmd := exec.Command("tailscale", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// CleanupDevices removes Rover-owned Tailscale devices. By default it deletes
// only stale/offline matches; when deleteOnline is true it deletes all matching
// Rover devices, which is appropriate after `tailscale logout` during teardown.
// When dryRun is true it reports what would be removed without deleting.
func CleanupDevices(clientID, clientSecret string, tags []string, hostname string, deleteOnline, dryRun bool) (CleanupResult, error) {
	token, err := getAccessToken(clientID, clientSecret)
	if err != nil {
		return CleanupResult{}, err
	}
	devices, err := listDevices(token)
	if err != nil {
		return CleanupResult{}, err
	}
	tagSet := map[string]bool{}
	for _, tag := range tags {
		tagSet[tag] = true
	}

	var res CleanupResult
	for _, d := range devices {
		if !matchesDevice(d, hostname, tagSet) {
			continue
		}
		res.Matched = append(res.Matched, d)
		if d.ConnectedToControl && !deleteOnline {
			res.Skipped = append(res.Skipped, d)
			continue
		}
		if dryRun {
			res.WouldDelete = append(res.WouldDelete, d)
			continue
		}
		if err := deleteDevice(token, d.DeviceID()); err != nil {
			return res, err
		}
		res.Deleted = append(res.Deleted, d)
	}
	return res, nil
}

func matchesDevice(d Device, hostname string, tags map[string]bool) bool {
	if d.IsExternal {
		return false
	}
	want := strings.ToLower(hostname)
	if !strings.EqualFold(d.Hostname, want) &&
		!strings.HasPrefix(strings.ToLower(d.Hostname), want+"-") &&
		!strings.HasPrefix(strings.ToLower(d.Name), want+".") &&
		!strings.HasPrefix(strings.ToLower(d.Name), want+"-") {
		return false
	}
	if len(tags) == 0 {
		return true
	}
	for _, tag := range d.Tags {
		if tags[tag] {
			return true
		}
	}
	return false
}

// DeviceID returns the preferred identifier for API calls.
func (d Device) DeviceID() string {
	if d.NodeID != "" {
		return d.NodeID
	}
	return d.ID
}

// DisplayName returns the most useful human-readable name for CLI output.
func (d Device) DisplayName() string {
	if d.Name != "" {
		return d.Name
	}
	if d.Hostname != "" {
		return d.Hostname
	}
	return d.DeviceID()
}

func listDevices(token string) ([]Device, error) {
	req, err := http.NewRequest(http.MethodGet, "https://api.tailscale.com/api/v2/tailnet/-/devices?fields=all", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := apiClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list tailscale devices: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		return nil, fmt.Errorf("list tailscale devices failed (status %d): %s", resp.StatusCode, string(body))
	}
	var out struct {
		Devices []Device `json:"devices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode tailscale devices: %w", err)
	}
	return out.Devices, nil
}

func deleteDevice(token, deviceID string) error {
	if deviceID == "" {
		return fmt.Errorf("delete tailscale device: empty device id")
	}
	req, err := http.NewRequest(http.MethodDelete, "https://api.tailscale.com/api/v2/device/"+url.PathEscape(deviceID), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := apiClient.Do(req)
	if err != nil {
		return fmt.Errorf("delete tailscale device %s: %w", deviceID, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		return fmt.Errorf("delete tailscale device %s failed (status %d): %s", deviceID, resp.StatusCode, string(body))
	}
	return nil
}

// GetAuthKey generates an ephemeral Tailscale auth key using Tailscale OAuth client credentials.
func GetAuthKey(clientID, clientSecret string, tags []string) (string, error) {
	token, err := getAccessToken(clientID, clientSecret)
	if err != nil {
		return "", err
	}

	keyURL := "https://api.tailscale.com/api/v2/tailnet/-/keys"

	type deviceCreate struct {
		Reusable      bool     `json:"reusable"`
		Ephemeral     bool     `json:"ephemeral"`
		Preauthorized bool     `json:"preauthorized"`
		Tags          []string `json:"tags"`
	}
	type keyCapabilities struct {
		Devices struct {
			Create deviceCreate `json:"create"`
		} `json:"devices"`
	}
	type createKeyPayload struct {
		Capabilities  keyCapabilities `json:"capabilities"`
		ExpirySeconds int             `json:"expirySeconds"`
		Description   string          `json:"description"`
	}

	payload := createKeyPayload{
		ExpirySeconds: 3600,
		Description:   "Automated key generated by Rover CLI",
	}
	payload.Capabilities.Devices.Create.Reusable = false
	payload.Capabilities.Devices.Create.Ephemeral = true
	payload.Capabilities.Devices.Create.Preauthorized = true
	payload.Capabilities.Devices.Create.Tags = tags

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal key payload: %w", err)
	}

	req, err := http.NewRequest("POST", keyURL, bytes.NewReader(payloadBytes))
	if err != nil {
		return "", fmt.Errorf("create key request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	keyResp, err := apiClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("execute create key request: %w", err)
	}
	defer func() { _ = keyResp.Body.Close() }()

	if keyResp.StatusCode != http.StatusOK && keyResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(io.LimitReader(keyResp.Body, 64*1024))
		return "", fmt.Errorf("create key failed (status %d): %s", keyResp.StatusCode, string(body))
	}

	var keyRes struct {
		Key string `json:"key"`
	}
	if err := json.NewDecoder(keyResp.Body).Decode(&keyRes); err != nil {
		return "", fmt.Errorf("decode key response: %w", err)
	}

	return keyRes.Key, nil
}

func getAccessToken(clientID, clientSecret string) (string, error) {
	tokenURL := "https://api.tailscale.com/api/v2/oauth/token"
	data := url.Values{}
	data.Set("client_id", clientID)
	data.Set("client_secret", clientSecret)
	data.Set("grant_type", "client_credentials")

	resp, err := apiClient.PostForm(tokenURL, data)
	if err != nil {
		return "", fmt.Errorf("oauth token request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		return "", fmt.Errorf("oauth token request failed (status %d): %s", resp.StatusCode, string(body))
	}

	var tokenRes struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenRes); err != nil {
		return "", fmt.Errorf("decode oauth response: %w", err)
	}
	return tokenRes.AccessToken, nil
}
