package tenmicron

import (
	"fmt"
	"strconv"
	"strings"
)

// Version is a parsed 10Micron firmware version (:GVN#, "maj.min[.fix]"). It is read
// once at Connect and read-only thereafter. Several commands and behaviours are gated
// on the firmware level — e.g. the plain goto command (:MSnf#, ≥2.11.0) and the
// ASCOM-saved park (:PsX#, ≥2.9.9) — so the driver can pick the vendor-correct form
// or fall back on older firmware. The zero value (unknown firmware, e.g. a
// directly-constructed Mount in tests) compares below every real version.
type Version struct{ Major, Minor, Fix int }

// atLeast reports whether v ≥ maj.min.fix (lexicographic on major, minor, fix).
func (v Version) atLeast(maj, min, fix int) bool {
	switch {
	case v.Major != maj:
		return v.Major > maj
	case v.Minor != min:
		return v.Minor > min
	default:
		return v.Fix >= fix
	}
}

// String formats the version as "maj.min.fix".
func (v Version) String() string { return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Fix) }

// parseVersion parses a :GVN# reply "maj.min[.fix]" (the .fix part is optional and
// defaults to 0, per the spec).
func parseVersion(s string) (Version, error) {
	s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "#"))
	parts := strings.Split(s, ".")
	if len(parts) < 2 {
		return Version{}, fmt.Errorf("gotenmicron: bad firmware version %q", s)
	}
	var v Version
	var err error
	if v.Major, err = strconv.Atoi(parts[0]); err != nil {
		return Version{}, fmt.Errorf("gotenmicron: bad firmware major in %q: %w", s, err)
	}
	if v.Minor, err = strconv.Atoi(parts[1]); err != nil {
		return Version{}, fmt.Errorf("gotenmicron: bad firmware minor in %q: %w", s, err)
	}
	if len(parts) >= 3 {
		v.Fix, _ = strconv.Atoi(parts[2]) // best-effort; absent/garbage → 0
	}
	return v, nil
}

// FirmwareVersion returns the parsed mount firmware version read at Connect (the zero
// Version if it could not be read — e.g. a Mount constructed directly rather than via
// Connect). The raw :GVN# string is available from the embedded core's Firmware().
func (m *Mount) FirmwareVersion() Version { return m.firmware }

// FirmwareAtLeast reports whether the connected firmware is at least maj.min.fix. A
// Mount with unknown firmware (zero Version) reports false, so firmware-gated features
// fall back to their conservative, universally-supported form.
func (m *Mount) FirmwareAtLeast(maj, min, fix int) bool { return m.firmware.atLeast(maj, min, fix) }
