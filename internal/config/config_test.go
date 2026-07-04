package config

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestSaveCreatesDirWithRestrictedPerms(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	st := Default()
	if err := st.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	configDir := filepath.Join(dir, "rover")
	info, err := os.Stat(configDir)
	if err != nil {
		t.Fatalf("stat config dir: %v", err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Errorf("config dir permissions = %o, want 0o700", info.Mode().Perm())
	}
}

func TestSaveFilePerms(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	st := Default()
	if err := st.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	p, _ := Path()
	info, err := os.Stat(p)
	if err != nil {
		t.Fatalf("stat state file: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("state file permissions = %o, want 0o600", info.Mode().Perm())
	}
}

func TestDefaultStateWellFormed(t *testing.T) {
	st := Default()
	if st.ResourceGroup == "" {
		t.Error("ResourceGroup is empty")
	}
	if st.Location == "" {
		t.Error("Location is empty")
	}
	if st.VMName == "" {
		t.Error("VMName is empty")
	}
	if st.AdminUsername == "" {
		t.Error("AdminUsername is empty")
	}
	if st.SSHPublicKey == "" {
		t.Error("SSHPublicKey is empty")
	}
	if st.DiskSizeGB < 30 {
		t.Errorf("DiskSizeGB = %d, want >= 30", st.DiskSizeGB)
	}
	if err := ValidateAdminUsername(st.AdminUsername); err != nil {
		t.Errorf("Default AdminUsername invalid: %v", err)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	st := Default()
	st.ResourceGroup = "rg-test"
	st.Size = "large"
	st.AnsibleApplied = true
	if err := st.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	p, _ := Path()
	if filepath.Dir(filepath.Dir(p)) != dir {
		t.Errorf("Path %q not under XDG dir %q", p, dir)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.ResourceGroup != "rg-test" || got.Size != "large" || !got.AnsibleApplied {
		t.Errorf("round trip mismatch: %+v", got)
	}
}

func TestLoadMissingReturnsDefaults(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	st, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if st.Location == "" || st.VMName == "" {
		t.Errorf("expected populated defaults, got %+v", st)
	}
}

func TestExistsTracksStateFile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if ok, err := Exists(); err != nil || ok {
		t.Fatalf("Exists() before save = %v (err %v), want false", ok, err)
	}
	if err := Default().Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if ok, err := Exists(); err != nil || !ok {
		t.Fatalf("Exists() after save = %v (err %v), want true", ok, err)
	}
}

func TestValidateAdminUsername(t *testing.T) {
	valid := []string{"rover", "mitchell", "dev_user", "a1", "_svc", "user-x"}
	for _, name := range valid {
		if err := ValidateAdminUsername(name); err != nil {
			t.Errorf("ValidateAdminUsername(%q) = %v, want nil", name, err)
		}
	}
	invalid := []string{"", "admin", "root", "ADMIN", "1user", "-bad", "has space", "trailing."}
	for _, name := range invalid {
		if err := ValidateAdminUsername(name); err == nil {
			t.Errorf("ValidateAdminUsername(%q) = nil, want error", name)
		}
	}
}

func TestSSHPortDefaulting(t *testing.T) {
	if got := (&State{}).SSHPort(); got != DefaultSSHPort {
		t.Errorf("unset SSHPort() = %d, want default %d", got, DefaultSSHPort)
	}
	if got := (&State{SSHListenPort: 2222}).SSHPort(); got != 2222 {
		t.Errorf("SSHPort() = %d, want 2222", got)
	}
	if got := Default().SSHPort(); got != DefaultSSHPort {
		t.Errorf("Default().SSHPort() = %d, want %d", got, DefaultSSHPort)
	}
}

func TestValidateSSHPort(t *testing.T) {
	for _, p := range []int{0, 1, 22, 29472, 65535} {
		if err := ValidateSSHPort(p); err != nil {
			t.Errorf("ValidateSSHPort(%d) = %v, want nil", p, err)
		}
	}
	for _, p := range []int{-1, 65536, 100000} {
		if err := ValidateSSHPort(p); err == nil {
			t.Errorf("ValidateSSHPort(%d) = nil, want error", p)
		}
	}
}

func TestDefaultTimezoneLocaleEmpty(t *testing.T) {
	st := Default()
	if st.Timezone != "" {
		t.Errorf("Default().Timezone = %q, want empty (auto-detect)", st.Timezone)
	}
	if st.Locale != "" {
		t.Errorf("Default().Locale = %q, want empty (auto-detect)", st.Locale)
	}
}

func TestTimezoneLocaleRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	st := Default()
	st.Timezone = "America/New_York"
	st.Locale = "en_US.UTF-8"
	if err := st.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Timezone != "America/New_York" {
		t.Errorf("Timezone = %q, want America/New_York", got.Timezone)
	}
	if got.Locale != "en_US.UTF-8" {
		t.Errorf("Locale = %q, want en_US.UTF-8", got.Locale)
	}
}

func TestTimezoneGetterUsesStored(t *testing.T) {
	st := &State{Timezone: "Europe/London"}
	if got := st.EffectiveTimezone(); got != "Europe/London" {
		t.Errorf("EffectiveTimezone() = %q, want Europe/London", got)
	}
}

func TestTimezoneGetterFallsBackToDetection(t *testing.T) {
	t.Setenv("TZ", "Asia/Tokyo")
	st := &State{}
	got := st.EffectiveTimezone()
	if got != "Asia/Tokyo" {
		t.Errorf("EffectiveTimezone() = %q, want Asia/Tokyo", got)
	}
}

func TestLocaleGetterUsesStored(t *testing.T) {
	st := &State{Locale: "fr_FR.UTF-8"}
	if got := st.EffectiveLocale(); got != "fr_FR.UTF-8" {
		t.Errorf("EffectiveLocale() = %q, want fr_FR.UTF-8", got)
	}
}

func TestLocaleGetterFallsBackToDetection(t *testing.T) {
	t.Setenv("LC_ALL", "")
	t.Setenv("LANG", "de_DE.UTF-8")
	st := &State{}
	got := st.EffectiveLocale()
	if got != "de_DE.UTF-8" {
		t.Errorf("EffectiveLocale() = %q, want de_DE.UTF-8", got)
	}
}

func TestPrivateKeyPathDerivation(t *testing.T) {
	s := &State{SSHPublicKey: "/home/u/.ssh/id_ed25519.pub"}
	if got := s.PrivateKeyPath(); got != "/home/u/.ssh/id_ed25519" {
		t.Errorf("PrivateKeyPath() = %q, want /home/u/.ssh/id_ed25519", got)
	}
	s.SSHPrivateKey = "/custom/key"
	if got := s.PrivateKeyPath(); got != "/custom/key" {
		t.Errorf("explicit PrivateKeyPath() = %q, want /custom/key", got)
	}
}

func TestEnvRendersRoverVars(t *testing.T) {
	st := Default()
	st.Subscription = "sub-1"
	env := st.Env()
	want := map[string]bool{
		"ROVER_RESOURCE_GROUP=" + st.ResourceGroup:       false,
		"ROVER_SUBSCRIPTION=sub-1":                       false,
		"ROVER_SSH_PORT=" + strconv.Itoa(DefaultSSHPort): false,
	}
	for _, e := range env {
		if _, ok := want[e]; ok {
			want[e] = true
		}
	}
	for k, found := range want {
		if !found {
			t.Errorf("Env() missing %q", k)
		}
	}
}

func TestTSOAuthDetection(t *testing.T) {
	st := Default()
	t.Setenv("TS_OAUTH_CLIENT_ID", "")
	t.Setenv("TS_OAUTH_CLIENT_SECRET", "")
	if st.HasTSOAuth() {
		t.Error("should not have OAuth with empty fields")
	}

	st.TailscaleClientID = "client-id"
	st.TailscaleClientSecret = "client-secret"
	if !st.HasTSOAuth() {
		t.Error("should have OAuth with both fields set")
	}

	t.Setenv("TS_OAUTH_CLIENT_ID", "env-id")
	t.Setenv("TS_OAUTH_CLIENT_SECRET", "env-secret")
	st.TailscaleClientID = ""
	st.TailscaleClientSecret = ""
	if !st.HasTSOAuth() {
		t.Error("should have OAuth via env vars")
	}
}

func TestTSTagSlice(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"tag:rover", []string{"tag:rover"}},
		{"tag:rover,tag:web", []string{"tag:rover", "tag:web"}},
		{"rover", []string{"tag:rover"}},
		{"", []string{"tag:rover"}},
		{"tag:rover tag:web", []string{"tag:rover", "tag:web"}},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			st := &State{TailscaleTags: tt.input}
			got := st.TSTagSlice()
			if len(got) != len(tt.want) {
				t.Fatalf("TSTagSlice(%q) = %v, want %v", tt.input, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("TSTagSlice(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
				}
			}
		})
	}
}
