package config

import (
	"path/filepath"
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

func TestEnvRendersRoverVars(t *testing.T) {
	st := Default()
	st.Subscription = "sub-1"
	env := st.Env()
	want := map[string]bool{
		"ROVER_RESOURCE_GROUP=" + st.ResourceGroup: false,
		"ROVER_SUBSCRIPTION=sub-1":                 false,
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
