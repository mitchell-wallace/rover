package locale

import (
	"fmt"
	"os"
	"strings"
)

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

func ValidateTimezone(tz string) error {
	if tz == "" {
		return fmt.Errorf("timezone must not be empty")
	}
	if strings.Contains(tz, "..") || strings.HasPrefix(tz, "/") {
		return fmt.Errorf("timezone %q is not a valid IANA timezone", tz)
	}
	return nil
}

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
