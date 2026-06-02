package sizes

import "testing"

func TestProfilesMatchOrder(t *testing.T) {
	if len(Profiles) != len(Order) {
		t.Fatalf("Profiles (%d) and Order (%d) length mismatch", len(Profiles), len(Order))
	}
	for _, name := range Order {
		p, ok := Get(name)
		if !ok {
			t.Errorf("missing profile for %q", name)
			continue
		}
		if p.Name != name {
			t.Errorf("profile %q has Name %q", name, p.Name)
		}
		if p.SKU == "" || p.VCPU == 0 || p.RAMGiB == 0 {
			t.Errorf("profile %q has zero-valued field: %+v", name, p)
		}
	}
}

func TestValidate(t *testing.T) {
	for _, name := range Order {
		if err := Validate(name); err != nil {
			t.Errorf("Validate(%q) = %v, want nil", name, err)
		}
	}
	if err := Validate("huge"); err == nil {
		t.Error("Validate(\"huge\") = nil, want error")
	}
}
