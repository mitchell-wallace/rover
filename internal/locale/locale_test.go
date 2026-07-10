package locale

import (
	"strings"
	"testing"
)

func TestEffectiveTimezone_FromEnv(t *testing.T) {
	t.Setenv("TZ", "Pacific/Auckland")
	got := EffectiveTimezone()
	if got != "Pacific/Auckland" {
		t.Fatalf("EffectiveTimezone() = %q, want %q", got, "Pacific/Auckland")
	}
}

func TestEffectiveTimezone_Fallback(t *testing.T) {
	t.Setenv("TZ", "")
	got := EffectiveTimezone()
	if got == "" {
		t.Fatalf("EffectiveTimezone() returned empty string")
	}
	if !strings.Contains(got, "/") && got != "UTC" {
		t.Fatalf("EffectiveTimezone() = %q, expected IANA timezone (with /) or UTC", got)
	}
}

func TestEffectiveLocale_FromLCAll(t *testing.T) {
	t.Setenv("LC_ALL", "de_DE.UTF-8")
	t.Setenv("LANG", "")
	t.Setenv("LANGUAGE", "")
	got := EffectiveLocale()
	if got != "de_DE.UTF-8" {
		t.Fatalf("EffectiveLocale() = %q, want %q", got, "de_DE.UTF-8")
	}
}

func TestEffectiveLocale_FromLang(t *testing.T) {
	t.Setenv("LC_ALL", "")
	t.Setenv("LANG", "fr_FR.UTF-8")
	t.Setenv("LANGUAGE", "")
	got := EffectiveLocale()
	if got != "fr_FR.UTF-8" {
		t.Fatalf("EffectiveLocale() = %q, want %q", got, "fr_FR.UTF-8")
	}
}

func TestEffectiveLocale_FromLanguage(t *testing.T) {
	t.Setenv("LC_ALL", "")
	t.Setenv("LANG", "")
	t.Setenv("LANGUAGE", "en_US:en")
	got := EffectiveLocale()
	if got != "en_US:en" {
		t.Fatalf("EffectiveLocale() = %q, want %q", got, "en_US:en")
	}
}

func TestEffectiveLocale_Fallback(t *testing.T) {
	t.Setenv("LC_ALL", "")
	t.Setenv("LANG", "")
	t.Setenv("LANGUAGE", "")
	got := EffectiveLocale()
	if got != "C.UTF-8" {
		t.Fatalf("EffectiveLocale() = %q, want %q", got, "C.UTF-8")
	}
}

func TestEffectiveLocale_PrefersLCAll(t *testing.T) {
	t.Setenv("LC_ALL", "ja_JP.UTF-8")
	t.Setenv("LANG", "en_US.UTF-8")
	got := EffectiveLocale()
	if got != "ja_JP.UTF-8" {
		t.Fatalf("EffectiveLocale() = %q, want %q", got, "ja_JP.UTF-8")
	}
}

func TestValidateTimezone(t *testing.T) {
	if err := ValidateTimezone("America/New_York"); err != nil {
		t.Fatalf("ValidateTimezone(America/New_York) = %v, want nil", err)
	}
	if err := ValidateTimezone("UTC"); err != nil {
		t.Fatalf("ValidateTimezone(UTC) = %v, want nil", err)
	}
	if err := ValidateTimezone(""); err == nil {
		t.Fatal("ValidateTimezone('') = nil, want error")
	}
	if err := ValidateTimezone("/etc/passwd"); err == nil {
		t.Fatal("ValidateTimezone(/etc/passwd) = nil, want error")
	}
	if err := ValidateTimezone(".."); err == nil {
		t.Fatal("ValidateTimezone(..) = nil, want error")
	}
}

func TestValidateLocale(t *testing.T) {
	if err := ValidateLocale("en_US.UTF-8"); err != nil {
		t.Fatalf("ValidateLocale(en_US.UTF-8) = %v, want nil", err)
	}
	if err := ValidateLocale("C"); err != nil {
		t.Fatalf("ValidateLocale(C) = %v, want nil", err)
	}
	if err := ValidateLocale("POSIX"); err != nil {
		t.Fatalf("ValidateLocale(POSIX) = %v, want nil", err)
	}
	if err := ValidateLocale(""); err == nil {
		t.Fatal("ValidateLocale('') = nil, want error")
	}
	if err := ValidateLocale("en_US"); err == nil {
		t.Fatal("ValidateLocale(en_US) = nil, want error")
	}
}
