package tenmicron

import (
	"fmt"
	"strings"

	"github.com/mikefsq/lx200"
)

// :MSfsn# pier-side selectors (n per the :GMF/:SMF meridian convention).
const (
	slewSideWest = 2
	slewSideEast = 3
)

// SlewToTargetOnSide slews to the selected target on a specific meridian side
// (:MSfs2# west / :MSfs3# east). Uses the LX200 slew reply shape.
func (m *Mount) SlewToTargetOnSide(side lx200.PierSide) error {
	n := slewSideEast
	if side == lx200.PierWest {
		n = slewSideWest
	}
	return m.slewInvalidate(fmt.Sprintf(":MSfs%d#", n))
}

// SlewToTargetNoFineLimit slews to the target, disregarding the fine-movement
// limit for keeping the same meridian side (:MSnf#).
func (m *Mount) SlewToTargetNoFineLimit() error { return m.slewInvalidate(":MSnf#") }

// SlewPolarAlign slews missing the target by a computed amount for assisted polar
// alignment (:MSap#). Requires an active model with ≥2 stars; the model becomes
// invalid afterwards and should be cleared.
func (m *Mount) SlewPolarAlign() error { return m.slewInvalidate(":MSap#") }

// SlewOrthoAlign slews missing the target for orthogonality correction (:MSao#).
// Requires an active model with ≥3 stars; the model becomes invalid afterwards.
func (m *Mount) SlewOrthoAlign() error { return m.slewInvalidate(":MSao#") }

func (m *Mount) slewInvalidate(cmd string) error {
	if err := m.Slew(cmd); err != nil {
		return err
	}
	m.invalidate()
	return nil
}

// SyncToTargetR syncs the mount to the selected target (:CMR#, identical to :CM#),
// returning the mount's reply.
func (m *Mount) SyncToTargetR() (string, error) {
	s, err := m.Get(":CMR#")
	if err == nil {
		m.invalidate()
	}
	return s, err
}

// AddAlignmentPoint adds an alignment point to the active model by syncing on the
// selected target and recomputing the model (:CMS#). Errors if the model can't be
// refined (mount replies "E#" instead of "V#").
func (m *Mount) AddAlignmentPoint() error {
	s, err := m.Get(":CMS#")
	if err != nil {
		return err
	}
	if strings.TrimSpace(s) != "V" {
		return fmt.Errorf("gotenmicron: alignment point rejected (%q)", s)
	}
	m.invalidate()
	return nil
}

// SwapEastWest / SwapNorthSouth invert the manual-move direction sense
// (:EW#/:NS#).
func (m *Mount) SwapEastWest() error   { return m.Blind(":EW#") }
func (m *Mount) SwapNorthSouth() error { return m.Blind(":NS#") }

// Stop halts all movement INCLUDING tracking (:STOP#). Unlike Halt (:Q#, which
// stops slewing only), tracking must be restarted afterwards via SetTracking(true).
func (m *Mount) Stop() error {
	if err := m.Blind(":STOP#"); err != nil {
		return err
	}
	m.invalidate()
	return nil
}

// NoOp sends the do-nothing LX200-compatibility command (:P#).
func (m *Mount) NoOp() error { return m.Blind(":P#") }

// SlewInProgress reports whether a slew is underway (or ended within the settle
// time) via :D#, queried directly rather than from the cached :Ginfo status.
func (m *Mount) SlewInProgress() (bool, error) {
	s, err := m.Get(":D#")
	if err != nil {
		return false, err
	}
	return len(s) > 0, nil // "■#" (0x7F) while slewing, "#" → "" when done
}

// PointingState returns the meridian side the mount reports (:pS#, "East"/"West").
func (m *Mount) PointingState() (lx200.PierSide, error) {
	s, err := m.Get(":pS#")
	if err != nil {
		return lx200.PierUnknown, err
	}
	switch strings.TrimSpace(s) {
	case "East":
		return lx200.PierEast, nil
	case "West":
		return lx200.PierWest, nil
	default:
		return lx200.PierUnknown, nil
	}
}
