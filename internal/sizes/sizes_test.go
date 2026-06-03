package sizes

import "testing"

func TestMatrixWellFormed(t *testing.T) {
	for _, fam := range Families {
		sizesForFam := Available(fam)
		if len(sizesForFam) == 0 {
			t.Errorf("family %q offers no sizes", fam)
		}
		for _, size := range sizesForFam {
			p, ok := Get(fam, size)
			if !ok {
				t.Errorf("missing profile for %q/%q", fam, size)
				continue
			}
			if p.Family != fam || p.Size != size {
				t.Errorf("profile %q/%q has Family/Size %q/%q", fam, size, p.Family, p.Size)
			}
			if p.SKU == "" || p.VCPU == 0 || p.RAMGiB == 0 {
				t.Errorf("profile %q/%q has zero-valued field: %+v", fam, size, p)
			}
		}
	}
}

func TestXsmallIsBurstableOnly(t *testing.T) {
	if _, ok := Get("burstable", "xsmall"); !ok {
		t.Error("burstable should offer xsmall")
	}
	for _, fam := range []string{"balanced", "ramheavy"} {
		if _, ok := Get(fam, "xsmall"); ok {
			t.Errorf("%q should not offer xsmall", fam)
		}
		if err := Validate(fam, "xsmall"); err == nil {
			t.Errorf("Validate(%q, xsmall) = nil, want error", fam)
		}
	}
}

func TestNormalizeFamilyDefault(t *testing.T) {
	if got := NormalizeFamily(""); got != DefaultFamily {
		t.Errorf("NormalizeFamily(\"\") = %q, want %q", got, DefaultFamily)
	}
	// Empty family resolves to the default for lookups (back-compat).
	if _, ok := Get("", "small"); !ok {
		t.Error("Get(\"\", small) should resolve via default family")
	}
}

func TestValidate(t *testing.T) {
	if err := Validate("burstable", "small"); err != nil {
		t.Errorf("Validate(burstable, small) = %v, want nil", err)
	}
	if err := Validate("burstable", "huge"); err == nil {
		t.Error("Validate(burstable, huge) = nil, want error")
	}
	if err := Validate("nope", "small"); err == nil {
		t.Error("Validate(nope, small) = nil, want error")
	}
}
