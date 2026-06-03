// Package sizes defines the family × size machine profiles. The authoritative
// SKU mapping lives in Bicep (infra/bicep/main.bicep); this package mirrors it
// for CLI validation and human-readable display.
//
// Two orthogonal dimensions:
//   - family: burstable (cheap, CPU-credit) | balanced (sustained CPU) |
//     ramheavy (memory-optimized).
//   - size:   xsmall | small | medium | large (a t-shirt size within a family).
//
// The matrix is intentionally ragged: only burstable offers an xsmall tier —
// Azure's balanced (Dasv7) and ramheavy (Easv7) families have no sub-2-vCPU SKU.
package sizes

import (
	"fmt"
	"strings"
)

// Profile describes a compute option for display and validation. Disk size is
// intentionally NOT part of a profile — it is independent and persistent across
// resizes (see config.State.DiskSizeGB / the Bicep osDiskSizeGB param).
type Profile struct {
	Family string
	Size   string
	SKU    string
	VCPU   int
	RAMGiB int
}

// DefaultFamily is used when none is configured (preserves pre-family behavior).
const DefaultFamily = "burstable"

// Families is the canonical family ordering.
var Families = []string{"burstable", "balanced", "ramheavy"}

// Order is the canonical xsmall→large size ordering.
var Order = []string{"xsmall", "small", "medium", "large"}

// familyDesc gives a one-word human hint per family.
var familyDesc = map[string]string{
	"burstable": "cheap, CPU-credit burst",
	"balanced":  "sustained general-purpose CPU",
	"ramheavy":  "memory-optimized",
}

// matrix maps family → size → profile. Keep in sync with main.bicep. A size
// absent for a family (e.g. balanced/xsmall) is simply not offered.
var matrix = map[string]map[string]Profile{
	"burstable": {
		"xsmall": {SKU: "Standard_B2als_v2", VCPU: 2, RAMGiB: 4},
		"small":  {SKU: "Standard_B2as_v2", VCPU: 2, RAMGiB: 8},
		"medium": {SKU: "Standard_B4als_v2", VCPU: 4, RAMGiB: 8},
		"large":  {SKU: "Standard_B4as_v2", VCPU: 4, RAMGiB: 16},
	},
	"balanced": {
		"small":  {SKU: "Standard_D2as_v7", VCPU: 2, RAMGiB: 8},
		"medium": {SKU: "Standard_D4as_v7", VCPU: 4, RAMGiB: 16},
		"large":  {SKU: "Standard_D8as_v7", VCPU: 8, RAMGiB: 32},
	},
	"ramheavy": {
		"small":  {SKU: "Standard_E2as_v7", VCPU: 2, RAMGiB: 16},
		"medium": {SKU: "Standard_E4as_v7", VCPU: 4, RAMGiB: 32},
		"large":  {SKU: "Standard_E8as_v7", VCPU: 8, RAMGiB: 64},
	},
}

// NormalizeFamily returns the family to use, defaulting an empty value.
func NormalizeFamily(family string) string {
	if family == "" {
		return DefaultFamily
	}
	return family
}

// Get returns the profile for a family/size combination.
func Get(family, size string) (Profile, bool) {
	family = NormalizeFamily(family)
	p, ok := matrix[family][size]
	if !ok {
		return Profile{}, false
	}
	p.Family, p.Size = family, size
	return p, true
}

// Available returns the size names offered by a family, in canonical order.
func Available(family string) []string {
	family = NormalizeFamily(family)
	out := make([]string, 0, len(Order))
	for _, s := range Order {
		if _, ok := matrix[family][s]; ok {
			out = append(out, s)
		}
	}
	return out
}

// ValidateFamily returns an error if family is not known.
func ValidateFamily(family string) error {
	if _, ok := matrix[NormalizeFamily(family)]; !ok {
		return fmt.Errorf("invalid family %q (choose one of: %s)", family, strings.Join(Families, ", "))
	}
	return nil
}

// Validate returns an error if the family/size combination is not offered.
func Validate(family, size string) error {
	if err := ValidateFamily(family); err != nil {
		return err
	}
	if _, ok := Get(family, size); !ok {
		return fmt.Errorf("size %q is not available for the %q family (choose one of: %s)",
			size, NormalizeFamily(family), strings.Join(Available(family), ", "))
	}
	return nil
}

// DescribeFamily returns a one-line human summary, e.g.
// "burstable — cheap, CPU-credit burst".
func DescribeFamily(family string) string {
	family = NormalizeFamily(family)
	return fmt.Sprintf("%-9s — %s", family, familyDesc[family])
}

// Describe returns a one-line human summary, e.g.
// "small  — Standard_B2as_v2 · 2 vCPU · 8 GiB RAM".
func (p Profile) Describe() string {
	return fmt.Sprintf("%-6s — %s · %d vCPU · %d GiB RAM",
		p.Size, p.SKU, p.VCPU, p.RAMGiB)
}
