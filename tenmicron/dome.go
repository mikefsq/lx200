package tenmicron

import (
	"fmt"
	"math"
	"time"
)

// DomeFlapStatus is the dome flap state (:GDF#).
type DomeFlapStatus int

const (
	DomeFlapNoDome DomeFlapStatus = 0 // no dome connected
	DomeFlapClosed DomeFlapStatus = 1 // flap closed
	DomeFlapOpen   DomeFlapStatus = 2 // flap open
	DomeFlapMoving DomeFlapStatus = 3 // flap moving
)

// DomeShutterStatus is the dome shutter state (:GDS#).
type DomeShutterStatus int

const (
	DomeShutterNoDome      DomeShutterStatus = 0 // no dome connected
	DomeShutterClosed      DomeShutterStatus = 1 // shutter closed
	DomeShutterOpen        DomeShutterStatus = 2 // shutter open
	DomeShutterMoving      DomeShutterStatus = 3 // shutter moving
	DomeShutterNotDetected DomeShutterStatus = 4 // shutter not detected
)

// DomeControl is the dome connection mode (:SDMn#).
type DomeControl int

const (
	DomeDisconnect DomeControl = 0 // disconnect dome
	DomeOnRS232    DomeControl = 1 // dome on RS-232 port
	DomeOnGPS      DomeControl = 2 // dome on GPS/aux port
)

// DomeAzimuth returns the dome azimuth in degrees (:GDA#); errors if no dome is
// connected (mount returns 9999).
func (m *Mount) DomeAzimuth() (float64, error) {
	n, err := m.getInt(":GDA#")
	if err != nil {
		return 0, err
	}
	if n == 9999 {
		return 0, fmt.Errorf("gotenmicron: dome azimuth unavailable")
	}
	return float64(n) / 10, nil // reply is tenths of a degree (0..3599)
}

// DomeFlap returns the dome flap status (:GDF#).
func (m *Mount) DomeFlap() (DomeFlapStatus, error) {
	n, err := m.getInt(":GDF#")
	return DomeFlapStatus(n), err
}

// DomeShutter returns the dome shutter status (:GDS#).
func (m *Mount) DomeShutter() (DomeShutterStatus, error) {
	n, err := m.getInt(":GDS#")
	return DomeShutterStatus(n), err
}

// DomeHoming reports whether a dome homing operation is in progress (:GDH#).
func (m *Mount) DomeHoming() (bool, error) { return m.getBool(":GDH#") }

// DomeSlewing reports whether the dome is slewing to its target (:GDW#). Valid when the
// dome is under the mount's internal control.
func (m *Mount) DomeSlewing() (bool, error) { return m.getBool(":GDW#") }

// DomeSlewingExternal reports the dome slew status when the dome is under EXTERNAL
// control via the :SDA commands (:GDw#): true = slewing / not at the manually set
// target. (Use DomeSlewing for internal-logic control.) (Firmware ≥ 2.9.11.)
func (m *Mount) DomeSlewingExternal() (bool, error) { return m.getBool(":GDw#") }

// DomeSettleTime returns the dome settle time during which :GDW#/:GDw# keep
// reporting slewing after the dome reaches target (:GDstm#).
func (m *Mount) DomeSettleTime() (time.Duration, error) {
	s, err := m.getFloat(":GDstm#")
	return time.Duration(s * float64(time.Second)), err
}

// SetDomeSettleTime sets the dome settle time (:SDstm…#, 0..99999 s): after a dome slew
// completes, :GDW#/:GDw# report slewing for this duration.
func (m *Mount) SetDomeSettleTime(d time.Duration) error { return m.setSettle(":SDstm", d) }

// CommandDomeFlap opens or closes the dome flap (:SDF2#/:SDF1#); reports whether
// the command was received (not completion).
func (m *Mount) CommandDomeFlap(open bool) (bool, error) {
	return m.getBool(fmt.Sprintf(":SDF%d#", flapShutterArg(open)))
}

// CommandDomeShutter opens or closes the dome shutter (:SDS2#/:SDS1#).
func (m *Mount) CommandDomeShutter(open bool) (bool, error) {
	return m.getBool(fmt.Sprintf(":SDS%d#", flapShutterArg(open)))
}

func flapShutterArg(open bool) int {
	if open {
		return 2
	}
	return 1
}

// StartDomeHoming starts dome homing (:SDH#); success means only that the command
// was received (a dome need not be connected).
func (m *Mount) StartDomeHoming() (bool, error) { return m.getBool(":SDH#") }

// SetDomeControl sets the dome connection mode (:SDMn#).
func (m *Mount) SetDomeControl(mode DomeControl) (bool, error) {
	return m.getBool(fmt.Sprintf(":SDM%d#", int(mode)))
}

// SetDomeMountType sets the mount type for dome control (:SDTn#, no reply):
// 1 = shoulders on the front side, 2 = shoulders on the back side.
func (m *Mount) SetDomeMountType(n int) error {
	return m.Blind(fmt.Sprintf(":SDT%d#", n))
}

// --- Dome slaving geometry + manual azimuth control (:SD* — no reply) --------
// These configure the mount's internal dome-slaving model or take direct control of the
// dome azimuth. All are firmware ≥ 1.6.4 except the manual-control pair (≥ 2.9.11).

// SetDomeRadius sets the dome radius in millimetres (:SDR#), used by the internal
// dome-slaving geometry.
func (m *Mount) SetDomeRadius(mm int) error { return m.Blind(fmt.Sprintf(":SDR%04d#", mm)) }

// SetDomeUpdateInterval sets how often (seconds) the mount recomputes the dome position
// (:SDU#).
func (m *Mount) SetDomeUpdateInterval(seconds int) error {
	return m.Blind(fmt.Sprintf(":SDU%02d#", seconds))
}

// SetDomeMountOffset sets the mount's position relative to the centre of the dome, in
// millimetres towards North, East and Zenith (:SDXM#/:SDYM#/:SDZM#), measured from the
// centre of the base of the mount.
func (m *Mount) SetDomeMountOffset(northMM, eastMM, zenithMM int) error {
	if err := m.Blind(fmt.Sprintf(":SDXM%+05d#", northMM)); err != nil {
		return err
	}
	if err := m.Blind(fmt.Sprintf(":SDYM%+05d#", eastMM)); err != nil {
		return err
	}
	return m.Blind(fmt.Sprintf(":SDZM%+05d#", zenithMM))
}

// SetDomeOpticalAxisOffset sets the telescope optical-axis position relative to the
// declination mounting flange, in millimetres (:SDX#/:SDY#): flangeToAxis is the
// distance from the flange to the optical axis (usually the OTA radius); lateral is the
// sideways displacement from the flange centre (positive to the right looking from the
// back of the OTA; 0 if not displaced).
func (m *Mount) SetDomeOpticalAxisOffset(flangeToAxisMM, lateralMM int) error {
	if err := m.Blind(fmt.Sprintf(":SDX%+05d#", flangeToAxisMM)); err != nil {
		return err
	}
	return m.Blind(fmt.Sprintf(":SDY%+05d#", lateralMM))
}

// SlewDomeToAzimuth slews the dome to the given azimuth in degrees (:SDA#, 0..360),
// overriding the mount's internal dome logic and taking direct control. Reports whether
// the angle was in range. Return control to the mount with ReleaseDomeControl (or by
// setting any dome parameter). (Firmware ≥ 2.9.11.)
func (m *Mount) SlewDomeToAzimuth(deg float64) (bool, error) {
	tenths := int(math.Round(deg * 10))
	if tenths < 0 || tenths > 3600 {
		return false, fmt.Errorf("gotenmicron: dome azimuth %.1f° outside [0, 360]", deg)
	}
	return m.getBool(fmt.Sprintf(":SDA%04d#", tenths))
}

// ReleaseDomeControl returns dome control to the mount's internal logic (:SDAr#),
// undoing a SlewDomeToAzimuth override. (Firmware ≥ 2.9.11.)
func (m *Mount) ReleaseDomeControl() error { return m.Blind(":SDAr#") }
