// Package shellsafe provides UI-free helpers for shell-safe Rover inputs.
package shellsafe

import "strings"

// AuthKey strips auth-key characters outside Rover's safe allowlist.
func AuthKey(key string) (clean string, stripped bool) {
	var b strings.Builder
	for _, r := range key {
		if isSafeAuthKeyChar(r) {
			b.WriteRune(r)
		} else {
			stripped = true
		}
	}
	return b.String(), stripped
}

func isSafeAuthKeyChar(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_'
}

// ShellArg strips shell-unsafe characters from a remote argument.
func ShellArg(s string) string {
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == ':' || r == '.' {
			return r
		}
		return -1
	}, s)
}
