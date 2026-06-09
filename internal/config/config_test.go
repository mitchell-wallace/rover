package config

import (
	"path/filepath"
	"strconv"
	"testing"
)

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

func TestDefaultAdminUsernameAlwaysValid(t *testing.T) {
	// Default() must never hand back a username Azure would reject, regardless
	// of the local login name.
	if err := ValidateAdminUsername(Default().AdminUsername); err != nil {
		t.Errorf("Default().AdminUsername invalid: %v", err)
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
	for _, p := range []int{0, 1, 22, 29472, 65535} { // 0 = unset, accepted
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
