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

// Nudge moves the mount by an offset from the current position — raAzArcsec arcseconds
// on the RA/azimuth axis and decAltArcsec on the declination/altitude axis (:NUDGE#). It
// computes new target coordinates and slews to them (using the LX200 slew reply shape),
// so it is for re-centering an object after a slew, NOT for autoguiding. (Firmware ≥
// 2.7.4.)
func (m *Mount) Nudge(raAzArcsec, decAltArcsec int) error {
	return m.slewInvalidate(fmt.Sprintf(":NUDGE%+05d,%+05d#", raAzArcsec, decAltArcsec))
}

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

// SwapEastWest inverts the East/West manual-move direction sense (:EW#).
func (m *Mount) SwapEastWest() error { return m.Blind(":EW#") }

// SwapNorthSouth inverts the North/South manual-move direction sense (:NS#).
func (m *Mount) SwapNorthSouth() error { return m.Blind(":NS#") }

// Stop halts all movement INCLUDING tracking (:STOP#). Unlike Halt (:Q#, which
// stops slewing only), tracking must be restarted afterwards via SetTracking(true).
func (m *Mount) Stop() error {
	if err := m.Blind(":STOP#"); err != nil {
		return err
	}
	m.clearMoving()
	m.invalidate()
	return nil
}

// NoOp sends the do-nothing LX200-compatibility command (:P#).
func (m *Mount) NoOp() error { return m.Blind(":P#") }

// StopPreHeating stops the motor pre-heating procedure if it is underway (:STOPPH#).
func (m *Mount) StopPreHeating() error { return m.Blind(":STOPPH#") }

// UserWait stops the mount and blocks further movement until AuthorizeMovement is sent
// or movement is re-authorized from the keypad (:USERWAIT#). Use it to halt the mount
// when your software detects an inconsistency; the mount then reports Gstat 11
// (GstatNeedsUserOK).
func (m *Mount) UserWait() error {
	if err := m.Blind(":USERWAIT#"); err != nil {
		return err
	}
	m.invalidate()
	return nil
}

// AuthorizeMovement allows the mount to move again after an inconsistency was signaled
// with UserWait (:USEROK#) — it clears the Gstat 11 "needs user OK" state.
func (m *Mount) AuthorizeMovement() error {
	if err := m.Blind(":USEROK#"); err != nil {
		return err
	}
	m.invalidate()
	return nil
}

// SlewInProgress reports whether a slew is underway (or ended within the settle
// time) via :D#, queried directly rather than from the cached :Ginfo status.
func (m *Mount) SlewInProgress() (bool, error) {
	s, err := m.Get(":D#")
	if err != nil {
		return false, err
	}
	return len(s) > 0, nil // "■#" (0x7F) while slewing, "#" → "" when done
}

// PointingState returns the meridian side the mount reports (:pS#, "East"/"West"),
// applying the southern-hemisphere correction on old firmware (see pierInverted).
func (m *Mount) PointingState() (lx200.PierSide, error) {
	s, err := m.Get(":pS#")
	if err != nil {
		return lx200.PierUnknown, err
	}
	var p lx200.PierSide
	switch strings.TrimSpace(s) {
	case "East":
		p = lx200.PierEast
	case "West":
		p = lx200.PierWest
	default:
		return lx200.PierUnknown, nil
	}
	if m.pierInverted() {
		p = invertPier(p)
	}
	return p, nil
}
