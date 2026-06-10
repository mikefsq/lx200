package tenmicron

import (
	"fmt"
	"time"
)

// DomeFlapStatus is the dome flap state (:GDF#).
type DomeFlapStatus int

const (
	DomeFlapNoDome DomeFlapStatus = 0
	DomeFlapClosed DomeFlapStatus = 1
	DomeFlapOpen   DomeFlapStatus = 2
	DomeFlapMoving DomeFlapStatus = 3
)

// DomeShutterStatus is the dome shutter state (:GDS#).
type DomeShutterStatus int

const (
	DomeShutterNoDome      DomeShutterStatus = 0
	DomeShutterClosed      DomeShutterStatus = 1
	DomeShutterOpen        DomeShutterStatus = 2
	DomeShutterMoving      DomeShutterStatus = 3
	DomeShutterNotDetected DomeShutterStatus = 4
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

// DomeFlap / DomeShutter return the flap / shutter status (:GDF#/:GDS#).
func (m *Mount) DomeFlap() (DomeFlapStatus, error) {
	n, err := m.getInt(":GDF#")
	return DomeFlapStatus(n), err
}

func (m *Mount) DomeShutter() (DomeShutterStatus, error) {
	n, err := m.getInt(":GDS#")
	return DomeShutterStatus(n), err
}

// DomeHoming reports whether a dome homing operation is in progress (:GDH#).
func (m *Mount) DomeHoming() (bool, error) { return m.getBool(":GDH#") }

// DomeSlewing reports whether the dome is slewing to its target (:GDW#).
func (m *Mount) DomeSlewing() (bool, error) { return m.getBool(":GDW#") }

// DomeSettleTime returns the dome settle time during which :GDW#/:GDw# keep
// reporting slewing after the dome reaches target (:GDstm#).
func (m *Mount) DomeSettleTime() (time.Duration, error) {
	s, err := m.getFloat(":GDstm#")
	return time.Duration(s * float64(time.Second)), err
}

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
