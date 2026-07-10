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

	"github.com/mitchell-wallace/rover/internal/locale"
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

// AzureConfig controls Rover's isolated Azure CLI context. Credentials and
// token state written by az live under ConfigDir rather than the host's
// default ~/.azure directory.
type AzureConfig struct {
	ConfigDir    string `json:"config_dir,omitempty"`
	Subscription string `json:"subscription,omitempty"`
	Tenant       string `json:"tenant,omitempty"`
}

// SSHConfig controls how Rover opens interactive public SSH sessions.
type SSHConfig struct {
	Tmux bool `json:"tmux"`
}

// State is the persisted Rover configuration + last-known runtime state.
type State struct {
	// Subscription is the pre-Azure-section location retained only so state
	// files written by older Rover versions continue to load. New saves migrate
	// it to Azure.Subscription.
	Subscription   string       `json:"subscription,omitempty"`
	Azure          *AzureConfig `json:"azure,omitempty"`
	SSH            *SSHConfig   `json:"ssh,omitempty"`
	ResourceGroup  string       `json:"resourceGroup"`
	Location       string       `json:"location"`
	VMName         string       `json:"vmName"`
	Family         string       `json:"family,omitempty"`
	Size           string       `json:"size"`
	DiskSizeGB     int          `json:"diskSizeGB"`
	AdminUsername  string       `json:"adminUsername"`
	SSHListenPort  int          `json:"sshPort,omitempty"`
	SSHPublicKey   string       `json:"sshPublicKey"`
	SSHPrivateKey  string       `json:"sshPrivateKey,omitempty"`
	Connection     Connection   `json:"connection,omitempty"`
	AnsibleApplied bool         `json:"ansibleApplied"`

	// Locale and timezone detected from the host at provision time.
	Timezone string `json:"timezone,omitempty"`
	Locale   string `json:"locale,omitempty"`

	// Tailscale (optional). The auth key is never stored here — it is read from
	// the TS_AUTHKEY environment variable at provision time or generated via OAuth.
	TailscaleHostname     string `json:"tailscaleHostname,omitempty"`
	TailscaleTags         string `json:"tailscaleTags,omitempty"`
	TailscaleClientID     string `json:"tailscaleClientId,omitempty"`
	TailscaleClientSecret string `json:"tailscaleClientSecret,omitempty"`
	PublicSSHClosed       bool   `json:"publicSshClosed,omitempty"`
}

// DiskGB returns the configured OS disk size, clamped to the 30 GiB minimum.
func (s *State) DiskGB() int {
	if s.DiskSizeGB < 30 {
		return 30
	}
	return s.DiskSizeGB
}

// DefaultSSHPort is Rover's non-default public SSH port. It sits below the Linux
// ephemeral range (32768–60999) so a listening sshd never races an outbound
// socket that grabbed the same port.
const DefaultSSHPort = 29472

// SSHPort returns the configured public SSH port, defaulting to DefaultSSHPort
// so state files written before the field existed keep a sane value.
func (s *State) SSHPort() int {
	if s.SSHListenPort == 0 {
		return DefaultSSHPort
	}
	return s.SSHListenPort
}

// SSHTmux reports whether interactive public SSH sessions should attach to
// Rover's tmux session. A missing section means enabled so older state files
// adopt the new default without a migration.
func (s *State) SSHTmux() bool {
	return s.SSH == nil || s.SSH.Tmux
}

// SSHSettings returns the mutable SSH section, creating it with defaults for
// state files written before the section existed.
func (s *State) SSHSettings() *SSHConfig {
	if s.SSH == nil {
		s.SSH = &SSHConfig{Tmux: true}
	}
	return s.SSH
}

// Fam returns the configured compute family, defaulting to burstable so state
// files written before families existed keep their original behavior.
func (s *State) Fam() string {
	if s.Family == "" {
		return "burstable"
	}
	return s.Family
}

// EffectiveTimezone returns the configured IANA timezone, auto-detecting from
// the host if not yet persisted.
func (s *State) EffectiveTimezone() string {
	if s.Timezone != "" {
		return s.Timezone
	}
	return locale.EffectiveTimezone()
}

// EffectiveLocale returns the configured locale string, auto-detecting from the
// host if not yet persisted.
func (s *State) EffectiveLocale() string {
	if s.Locale != "" {
		return s.Locale
	}
	return locale.EffectiveLocale()
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

// TSClientID returns the Tailscale Client ID, checking environment variables first.
func (s *State) TSClientID() string {
	if v := os.Getenv("TS_OAUTH_CLIENT_ID"); v != "" {
		return v
	}
	return s.TailscaleClientID
}

// TSClientSecret returns the Tailscale Client Secret, checking environment variables first.
func (s *State) TSClientSecret() string {
	if v := os.Getenv("TS_OAUTH_CLIENT_SECRET"); v != "" {
		return v
	}
	return s.TailscaleClientSecret
}

// HasTSOAuth reports whether Tailscale OAuth credentials are set.
func (s *State) HasTSOAuth() bool {
	return s.TSClientID() != "" && s.TSClientSecret() != ""
}

// TSTagSlice returns the Tailscale tags as a slice of strings, ensuring each starts with "tag:".
func (s *State) TSTagSlice() []string {
	var tags []string
	for _, t := range strings.Fields(strings.ReplaceAll(s.TSTags(), ",", " ")) {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if !strings.HasPrefix(t, "tag:") {
			t = "tag:" + t
		}
		tags = append(tags, t)
	}
	if len(tags) == 0 {
		tags = append(tags, "tag:rover")
	}
	return tags
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
		Azure:         &AzureConfig{},
		SSH:           &SSHConfig{Tmux: true},
		ResourceGroup: "rover-rg",
		Location:      "australiaeast",
		VMName:        "rover-vm",
		Family:        "burstable",
		Size:          "small",
		DiskSizeGB:    30,
		AdminUsername: admin,
		SSHListenPort: DefaultSSHPort,
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

// ValidateSSHPort returns an error if port is not a usable TCP port. 0 is
// accepted as "unset" (callers fall back to DefaultSSHPort via State.SSHPort).
func ValidateSSHPort(port int) error {
	if port == 0 {
		return nil
	}
	if port < 1 || port > 65535 {
		return fmt.Errorf("ssh port %d is out of range (1–65535)", port)
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

// DefaultAzureConfigDir returns Rover's default isolated Azure CLI directory.
func DefaultAzureConfigDir() (string, error) {
	p, err := Path()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(p), "azure"), nil
}

// AzureSettings returns the mutable Azure section, creating it for state files
// written before the section existed.
func (s *State) AzureSettings() *AzureConfig {
	if s.Azure == nil {
		s.Azure = &AzureConfig{}
	}
	return s.Azure
}

// ConfiguredAzureConfigDir resolves config-file state without consulting the
// environment. An empty config_dir selects Rover's isolated default.
func (s *State) ConfiguredAzureConfigDir() (string, error) {
	if s.Azure != nil && s.Azure.ConfigDir != "" {
		return s.Azure.ConfigDir, nil
	}
	return DefaultAzureConfigDir()
}

// AzureConfigDir resolves the effective Azure CLI directory. An explicitly
// set AZURE_CONFIG_DIR wins over Rover's config-file value.
func (s *State) AzureConfigDir() (string, error) {
	if dir := os.Getenv("AZURE_CONFIG_DIR"); dir != "" {
		return dir, nil
	}
	return s.ConfiguredAzureConfigDir()
}

// AzureConfigDirOverridden reports whether the user explicitly selected an
// Azure CLI directory through the environment.
func AzureConfigDirOverridden() bool {
	return os.Getenv("AZURE_CONFIG_DIR") != ""
}

// AzureSubscription returns the nested Azure subscription, falling back to
// the legacy top-level field for callers holding an un-migrated State value.
func (s *State) AzureSubscription() string {
	if s.Azure != nil && s.Azure.Subscription != "" {
		return s.Azure.Subscription
	}
	return s.Subscription
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
	azure := st.AzureSettings()
	if st.Subscription != "" {
		if azure.Subscription == "" {
			azure.Subscription = st.Subscription
		}
		st.Subscription = ""
	}
	return st, nil
}

// Save writes the state file, creating parent directories as needed.
func (s *State) Save() error {
	p, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, append(data, '\n'), 0o600)
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
func (s *State) Env() ([]string, error) {
	azureConfigDir, err := s.AzureConfigDir()
	if err != nil {
		return nil, err
	}
	env := os.Environ()
	set := func(k, v string) {
		prefix := k + "="
		for i := len(env) - 1; i >= 0; i-- {
			if strings.HasPrefix(env[i], prefix) {
				env = append(env[:i], env[i+1:]...)
			}
		}
		env = append(env, prefix+v)
	}
	add := func(k, v string) {
		if v != "" {
			set(k, v)
		}
	}
	set("AZURE_CONFIG_DIR", azureConfigDir)
	add("ROVER_RESOURCE_GROUP", s.ResourceGroup)
	add("ROVER_LOCATION", s.Location)
	add("ROVER_VM_NAME", s.VMName)
	add("ROVER_DISK_GB", fmt.Sprintf("%d", s.DiskGB()))
	add("ROVER_SSH_PORT", fmt.Sprintf("%d", s.SSHPort()))
	add("ROVER_ADMIN_USER", s.AdminUsername)
	add("ROVER_SSH_PUBKEY", s.SSHPublicKey)
	add("ROVER_SSH_KEY", s.PrivateKeyPath())
	// Subscription is config-file controlled. Set an explicit empty value so a
	// standalone-script ROVER_SUBSCRIPTION from the host cannot leak into Rover.
	set("ROVER_SUBSCRIPTION", s.AzureSubscription())
	sshAccess := "Allow"
	if s.PublicSSHClosed {
		sshAccess = "Deny"
	}
	add("ROVER_PUBLIC_SSH_ACCESS", sshAccess)
	return env, nil
}
