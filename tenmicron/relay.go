package tenmicron

import "fmt"

// External relay control (:GRLYn# / :SRLYn,m#), a special-purpose-mount option. Relays
// 1–6 are user relays; 7 and 8 are the RA/Az and Dec/Alt motor-heater relays (readable
// only). Relays are in an undefined state at power-up, then default to open.
type Relay int

const (
	UserRelay1        Relay = 1 // user relay 1
	UserRelay2        Relay = 2 // user relay 2
	UserRelay3        Relay = 3 // user relay 3
	UserRelay4        Relay = 4 // user relay 4
	UserRelay5        Relay = 5 // user relay 5
	UserRelay6        Relay = 6 // user relay 6
	RAAzHeaterRelay   Relay = 7 // RA/azimuth motor heater (read-only)
	DecAltHeaterRelay Relay = 8 // declination/altitude motor heater (read-only)
)

// RelayClosed reports whether relay n is closed (:GRLYn#): true = closed, false = open.
// The reply is a single status byte with no '#'. (Firmware ≥ 2.3.0.)
func (m *Mount) RelayClosed(n Relay) (bool, error) {
	return m.getBoolByte(fmt.Sprintf(":GRLY%d#", int(n)))
}

// SetRelay opens (closed=false) or closes (closed=true) user relay n (1..6)
// (:SRLYn,m#). Only the user relays are settable. (Firmware ≥ 2.3.)
func (m *Mount) SetRelay(n Relay, closed bool) error {
	if n < UserRelay1 || n > UserRelay6 {
		return fmt.Errorf("gotenmicron: only user relays 1..6 are settable, got %d", int(n))
	}
	return must(m.Ack(fmt.Sprintf(":SRLY%d,%d#", int(n), b2i(closed))))
}
