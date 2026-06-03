// Package config owns Rover's local state/config file — the single, explicit
// source of truth for the one Rover-managed VM. No implicit magic: everything
// Rover knows lives in this JSON file.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"
)

// Connection holds the last known reachability info for the VM. It is a cache
// for display/convenience; live Azure queries remain authoritative.
type Connection struct {
	Exists     bool   `json:"exists"`
	PowerState string `json:"powerState,omitempty"`
	VMSize     string `json:"vmSize,omitempty"`
	PublicIP   string `json:"publicIp,omitempty"`
	FQDN       string `json:"fqdn,omitempty"`
	PrivateIP  string `json:"privateIp,omitempty"`
	SSHTarget  string `json:"sshTarget,omitempty"`
}

// State is the persisted Rover configuration + last-known runtime state.
type State struct {
	Subscription   string     `json:"subscription,omitempty"`
	ResourceGroup  string     `json:"resourceGroup"`
	Location       string     `json:"location"`
	VMName         string     `json:"vmName"`
	Family         string     `json:"family,omitempty"`
	Size           string     `json:"size"`
	DiskSizeGB     int        `json:"diskSizeGB"`
	AdminUsername  string     `json:"adminUsername"`
	SSHPublicKey   string     `json:"sshPublicKey"`
	SSHPrivateKey  string     `json:"sshPrivateKey,omitempty"`
	Connection     Connection `json:"connection,omitempty"`
	AnsibleApplied bool       `json:"ansibleApplied"`

	// Tailscale (optional). The auth key is never stored here — it is read from
	// the TS_AUTHKEY environment variable at provision time.
	TailscaleHostname string `json:"tailscaleHostname,omitempty"`
	TailscaleTags     string `json:"tailscaleTags,omitempty"`
}

// DiskGB returns the configured OS disk size, clamped to the 30 GiB minimum.
func (s *State) DiskGB() int {
	if s.DiskSizeGB < 30 {
		return 30
	}
	return s.DiskSizeGB
}

// Fam returns the configured compute family, defaulting to burstable so state
// files written before families existed keep their original behavior.
func (s *State) Fam() string {
	if s.Family == "" {
		return "burstable"
	}
	return s.Family
}

// TSHostname returns the Tailscale node name, defaulting to the VM name.
func (s *State) TSHostname() string {
	if s.TailscaleHostname != "" {
		return s.TailscaleHostname
	}
	return s.VMName
}

// TSTags returns the Tailscale tags, defaulting to tag:rover.
func (s *State) TSTags() string {
	if s.TailscaleTags != "" {
		return s.TailscaleTags
	}
	return "tag:rover"
}

// Default returns a State pre-populated with sane defaults for a first run.
func Default() *State {
	admin := "rover"
	// Prefer the local username, but only if Azure would accept it; otherwise
	// fall back to "rover" so a reserved/odd login name doesn't fail late.
	if u, err := user.Current(); err == nil && ValidateAdminUsername(u.Username) == nil {
		admin = u.Username
	}
	return &State{
		ResourceGroup: "rover-rg",
		Location:      "australiaeast",
		VMName:        "rover-vm",
		Family:        "burstable",
		Size:          "small",
		DiskSizeGB:    30,
		AdminUsername: admin,
		SSHPublicKey:  defaultSSHKey(),
		TailscaleTags: "tag:rover",
	}
}

// defaultSSHKey picks an existing public key (preferring ed25519), falling back
// to the conventional ed25519 path so a fresh user is nudged toward it.
func defaultSSHKey() string {
	home, _ := os.UserHomeDir()
	candidates := []string{"id_ed25519.pub", "id_rsa.pub", "id_ecdsa.pub"}
	for _, c := range candidates {
		p := filepath.Join(home, ".ssh", c)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return filepath.Join(home, ".ssh", "id_ed25519.pub")
}

// Exists reports whether a Rover state file has been written yet. A fresh
// install has none — callers use this to drive the guided first run.
func Exists() (bool, error) {
	p, err := Path()
	if err != nil {
		return false, err
	}
	_, err = os.Stat(p)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// adminUsernamePattern mirrors Azure's Linux admin username rule: start with a
// letter or underscore, then letters/digits/hyphen/underscore.
var adminUsernamePattern = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_-]*$`)

// reservedAdminUsernames are names Azure rejects outright for VM admin users.
// Source: Azure VM "disallowed values for adminUsername".
var reservedAdminUsernames = map[string]bool{
	"administrator": true, "admin": true, "user": true, "user1": true,
	"test": true, "user2": true, "test1": true, "user3": true, "admin1": true,
	"1": true, "123": true, "a": true, "actuser": true, "adm": true,
	"admin2": true, "aspnet": true, "backup": true, "console": true,
	"david": true, "guest": true, "john": true, "owner": true, "root": true,
	"server": true, "sql": true, "support": true, "support_388945a0": true,
	"sys": true, "test2": true, "test3": true, "user4": true, "user5": true,
}

// ValidateAdminUsername returns an error if name is unusable as an Azure Linux
// VM admin username. Azure rejects reserved names and enforces a format, and a
// mismatch only surfaces late (at deploy time) otherwise.
func ValidateAdminUsername(name string) error {
	if name == "" {
		return fmt.Errorf("admin username is empty")
	}
	if len(name) > 64 {
		return fmt.Errorf("admin username %q is too long (max 64 characters)", name)
	}
	if strings.HasSuffix(name, ".") {
		return fmt.Errorf("admin username %q cannot end with a period", name)
	}
	if reservedAdminUsernames[strings.ToLower(name)] {
		return fmt.Errorf("admin username %q is reserved by Azure; pick another", name)
	}
	if !adminUsernamePattern.MatchString(name) {
		return fmt.Errorf("admin username %q is invalid: start with a letter or underscore and use only letters, digits, hyphens, or underscores", name)
	}
	return nil
}

// Path returns the location of the state file (honours XDG via UserConfigDir).
func Path() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "rover", "state.json"), nil
}

// Load reads the state file, returning defaults if it does not exist yet.
func Load() (*State, error) {
	p, err := Path()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return Default(), nil
	}
	if err != nil {
		return nil, err
	}
	st := Default()
	if err := json.Unmarshal(data, st); err != nil {
		return nil, fmt.Errorf("parse %s: %w", p, err)
	}
	return st, nil
}

// Save writes the state file, creating parent directories as needed.
func (s *State) Save() error {
	p, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, append(data, '\n'), 0o644)
}

// PrivateKeyPath returns the configured private key, deriving it from the
// public key path (strip .pub) when not set explicitly.
func (s *State) PrivateKeyPath() string {
	if s.SSHPrivateKey != "" {
		return s.SSHPrivateKey
	}
	if len(s.SSHPublicKey) > 4 && s.SSHPublicKey[len(s.SSHPublicKey)-4:] == ".pub" {
		return s.SSHPublicKey[:len(s.SSHPublicKey)-4]
	}
	return s.SSHPublicKey
}

// Env renders the state as ROVER_* environment variables for the shell scripts,
// appended onto the current process environment.
func (s *State) Env() []string {
	env := os.Environ()
	add := func(k, v string) {
		if v != "" {
			env = append(env, k+"="+v)
		}
	}
	add("ROVER_RESOURCE_GROUP", s.ResourceGroup)
	add("ROVER_LOCATION", s.Location)
	add("ROVER_VM_NAME", s.VMName)
	add("ROVER_DISK_GB", fmt.Sprintf("%d", s.DiskGB()))
	add("ROVER_ADMIN_USER", s.AdminUsername)
	add("ROVER_SSH_PUBKEY", s.SSHPublicKey)
	add("ROVER_SSH_KEY", s.PrivateKeyPath())
	add("ROVER_SUBSCRIPTION", s.Subscription)
	return env
}
