// Package locale detects and validates host locale and timezone settings.
package locale

import (
	"fmt"
	"os"
	"strings"
)

// EffectiveTimezone returns the host's configured IANA timezone, falling back
// to UTC when it cannot be detected.
func EffectiveTimezone() string {
	if tz := strings.TrimSpace(os.Getenv("TZ")); tz != "" {
		return tz
	}
	if data, err := os.ReadFile("/etc/timezone"); err == nil {
		if tz := strings.TrimSpace(string(data)); tz != "" {
			return tz
		}
	}
	if target, err := os.Readlink("/etc/localtime"); err == nil {
		const prefix = "/usr/share/zoneinfo/"
		if after, ok := strings.CutPrefix(target, prefix); ok {
			if tz := strings.TrimSpace(after); tz != "" {
				return tz
			}
		}
		if i := strings.Index(target, prefix); i >= 0 {
			return target[i+len(prefix):]
		}
	}
	return "UTC"
}

// EffectiveLocale returns the host's configured locale, falling back to
// C.UTF-8 when no locale environment variable is set.
func EffectiveLocale() string {
	if loc := strings.TrimSpace(os.Getenv("LC_ALL")); loc != "" {
		return loc
	}
	if loc := strings.TrimSpace(os.Getenv("LANG")); loc != "" {
		return loc
	}
	if loc := strings.TrimSpace(os.Getenv("LANGUAGE")); loc != "" {
		return loc
	}
	return "C.UTF-8"
}

// ValidateTimezone rejects empty, absolute, or parent-traversing timezone
// names.
func ValidateTimezone(tz string) error {
	if tz == "" {
		return fmt.Errorf("timezone must not be empty")
	}
	if strings.Contains(tz, "..") || strings.HasPrefix(tz, "/") {
		return fmt.Errorf("timezone %q is not a valid IANA timezone", tz)
	}
	return nil
}

// ValidateLocale checks that loc is a usable locale with an explicit encoding,
// or one of the encoding-independent C/POSIX locales.
func ValidateLocale(loc string) error {
	if loc == "" {
		return fmt.Errorf("locale must not be empty")
	}
	if !strings.Contains(loc, ".") && !strings.Contains(loc, "@") {
		if loc == "C" || loc == "POSIX" {
			return nil
		}
		return fmt.Errorf("locale %q should include a character encoding (e.g. en_US.UTF-8)", loc)
	}
	return nil
}
