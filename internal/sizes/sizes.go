// Package sizes defines the small | medium | large machine profiles. The
// authoritative SKU/disk mapping lives in Bicep (infra/bicep/main.bicep); this
// package mirrors it for CLI validation and human-readable display.
package sizes

import (
	"fmt"
	"strings"
)

// Profile describes a compute size option for display and validation. Disk size
// is intentionally NOT part of a profile — it is independent and persistent
// across resizes (see config.State.DiskSizeGB / the Bicep osDiskSizeGB param).
type Profile struct {
	Name   string
	SKU    string
	VCPU   int
	RAMGiB int
}

// Order is the canonical small→large ordering.
var Order = []string{"small", "medium", "large"}

// Profiles maps size name to its profile. Keep in sync with main.bicep.
var Profiles = map[string]Profile{
	"small":  {Name: "small", SKU: "Standard_B2ls_v2", VCPU: 2, RAMGiB: 4},
	"medium": {Name: "medium", SKU: "Standard_B2s_v2", VCPU: 2, RAMGiB: 8},
	"large":  {Name: "large", SKU: "Standard_B4s_v2", VCPU: 4, RAMGiB: 16},
}

// Get returns the profile for a size name.
func Get(name string) (Profile, bool) {
	p, ok := Profiles[name]
	return p, ok
}

// Validate returns an error if name is not a known size.
func Validate(name string) error {
	if _, ok := Profiles[name]; !ok {
		return fmt.Errorf("invalid size %q (choose one of: %s)", name, strings.Join(Order, ", "))
	}
	return nil
}

// Describe returns a one-line human summary, e.g.
// "small  — Standard_B2ls_v2 · 2 vCPU · 4 GiB RAM".
func (p Profile) Describe() string {
	return fmt.Sprintf("%-6s — %s · %d vCPU · %d GiB RAM",
		p.Name, p.SKU, p.VCPU, p.RAMGiB)
}
