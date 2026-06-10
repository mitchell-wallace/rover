package config

import (
	"os"
	"path/filepath"
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
