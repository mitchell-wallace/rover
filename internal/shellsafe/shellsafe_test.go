package shellsafe

import "testing"

func TestAuthKey(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		want         string
		wantStripped bool
	}{
		{"alphanumeric", "tskey-auth-abc123XYZ", "tskey-auth-abc123XYZ", false},
		{"with dashes and underscores", "tskey_auth-abc-123_XYZ", "tskey_auth-abc-123_XYZ", false},
		{"strips single quotes", "tskey'auth", "tskeyauth", true},
		{"strips double quotes", `tskey"auth`, "tskeyauth", true},
		{"strips backticks", "tskey`auth", "tskeyauth", true},
		{"strips semicolons", "tskey;rm-rf", "tskeyrm-rf", true},
		{"strips dollar sign", "tskey$var", "tskeyvar", true},
		{"strips backslash", `tskey\auth`, "tskeyauth", true},
		{"strips spaces", "ts key auth", "tskeyauth", true},
		{"strips pipe", "tskey|evil", "tskeyevil", true},
		{"strips ampersand", "tskey&&evil", "tskeyevil", true},
		{"empty string", "", "", false},
		{"only special chars", "';\"`|&", "", true},
		{"dots stripped", "ts.key", "tskey", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, stripped := AuthKey(tt.input)
			if got != tt.want {
				t.Errorf("AuthKey(%q) clean = %q, want %q", tt.input, got, tt.want)
			}
			if stripped != tt.wantStripped {
				t.Errorf("AuthKey(%q) stripped = %v, want %v", tt.input, stripped, tt.wantStripped)
			}
		})
	}
}

func TestAuthKey_StripsWithWarning(t *testing.T) {
	key, stripped := AuthKey("tskey'inject")
	if key != "tskeyinject" {
		t.Errorf("expected stripped key, got %q", key)
	}
	if !stripped {
		t.Errorf("expected stripped to be true when characters were removed")
	}
}

func TestIsSafeAuthKeyChar(t *testing.T) {
	safe := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_"
	for _, r := range safe {
		if !isSafeAuthKeyChar(r) {
			t.Errorf("expected %q to be safe", r)
		}
	}
	unsafe := "'\"`;$\\|&!(){}[]<> \t\n"
	for _, r := range unsafe {
		if isSafeAuthKeyChar(r) {
			t.Errorf("expected %q to be unsafe", r)
		}
	}
}

func TestShellArg(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"rover-vm", "rover-vm"},
		{"tag:rover", "tag:rover"},
		{"rover-vm.tailnet.ts.net", "rover-vm.tailnet.ts.net"},
		{"rover';rm -rf /", "roverrm-rf"},
		{`rover"$(evil)`, "roverevil"},
		{"clean_name-123", "clean_name-123"},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ShellArg(tt.input)
			if got != tt.want {
				t.Errorf("ShellArg(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
