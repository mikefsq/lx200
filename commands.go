package lx200

import "fmt"

// This file holds the command set common to essentially all LX200-family mounts.
// Vendor-specific commands (tracking on/off, park/home, pier side, status
// strings, site/time formats) diverge between mounts and belong in the per-mount
// library, which uses the exported Blind/Ack/Get/Slew primitives directly.

// --- Coordinate queries ---

// RA returns the current right ascension in hours (:GR#).
func (c *Conn) RA() (float64, error) {
	s, err := c.Get(":GR#")
	if err != nil {
		return 0, err
	}
	return ParseSexagesimal(s)
}

// Dec returns the current declination in degrees (:GD#).
func (c *Conn) Dec() (float64, error) {
	s, err := c.Get(":GD#")
	if err != nil {
		return 0, err
	}
	return ParseSexagesimal(s)
}

// Altitude returns the current altitude in degrees (:GA#).
func (c *Conn) Altitude() (float64, error) {
	s, err := c.Get(":GA#")
	if err != nil {
		return 0, err
	}
	return ParseSexagesimal(s)
}

// Azimuth returns the current azimuth in degrees, 0–360 (:GZ#).
func (c *Conn) Azimuth() (float64, error) {
	s, err := c.Get(":GZ#")
	if err != nil {
		return 0, err
	}
	return ParseSexagesimal(s)
}

// SiderealTime returns the local apparent sidereal time in hours (:GS#).
func (c *Conn) SiderealTime() (float64, error) {
	s, err := c.Get(":GS#")
	if err != nil {
		return 0, err
	}
	return ParseSexagesimal(s)
}

// Firmware returns the mount firmware version string (:GVN#). Some mounts use a
// different query; override in the per-mount library if so.
func (c *Conn) Firmware() (string, error) { return c.Get(":GVN#") }

// --- Target + goto ---

// SetTargetRA sets the target right ascension in hours (:Sr HH:MM:SS#). It
// returns whether the mount accepted the value.
func (c *Conn) SetTargetRA(hours float64) (bool, error) {
	return c.Ack(":Sr" + FormatHMS(hours) + "#")
}

// SetTargetDec sets the target declination in degrees (:Sd sDD*MM:SS#).
func (c *Conn) SetTargetDec(deg float64) (bool, error) {
	return c.Ack(":Sd" + FormatDMS(deg, '*') + "#")
}

// SlewToTarget starts a slew to the previously set target (:MS#). It returns nil
// once the slew has begun, or an error carrying the mount's refusal reason
// (e.g. below horizon, past a limit).
func (c *Conn) SlewToTarget() error { return c.Slew(":MS#") }

// SyncToTarget aligns the mount's current position to the set target (:CM#) and
// returns the mount's confirmation string.
func (c *Conn) SyncToTarget() (string, error) { return c.Get(":CM#") }

// Halt stops all motion immediately (:Q#).
func (c *Conn) Halt() error { return c.Blind(":Q#") }

// --- Manual motion ---

// Direction is a cardinal slew direction (the lowercase LX200 move/quit suffix).
type Direction byte

const (
	North Direction = 'n'
	South Direction = 's'
	East  Direction = 'e'
	West  Direction = 'w'
)

// Move starts a continuous slew in the given direction at the current rate
// (:Mn# / :Ms# / :Me# / :Mw#).
func (c *Conn) Move(d Direction) error { return c.Blind(":M" + string(d) + "#") }

// HaltMove stops motion in one direction (:Qn# / :Qs# / :Qe# / :Qw#).
func (c *Conn) HaltMove(d Direction) error { return c.Blind(":Q" + string(d) + "#") }

// Rate is a slew-speed preset (the uppercase LX200 :R_ suffix).
type Rate byte

const (
	RateGuide  Rate = 'G' // guide rate
	RateCenter Rate = 'C' // centering rate
	RateFind   Rate = 'M' // find/move rate
	RateMax    Rate = 'S' // slew (max) rate
)

// SetRate selects the manual-slew speed preset (:RG# / :RC# / :RM# / :RS#).
func (c *Conn) SetRate(r Rate) error { return c.Blind(":R" + string(r) + "#") }

// --- Pulse guide ---

// PulseGuide issues a timed guide pulse of ms milliseconds in one direction
// (:Mg{n,s,e,w}%04d#). The mount guides for the duration and replies nothing;
// the call returns once the command is sent (it does not block for the pulse).
// This is the standard LX200 pulse-guide and backs the Alpaca PulseGuide member.
func (c *Conn) PulseGuide(d Direction, ms int) error {
	if ms < 0 || ms > 9999 {
		return fmt.Errorf("lx200: pulse-guide duration %d ms out of range 0..9999", ms)
	}
	return c.Blind(fmt.Sprintf(":Mg%c%04d#", d, ms))
}

// --- MoveAxis (Alpaca-style continuous slew) ---

// Axis selects the mount axis for MoveAxis/StopAxis. AxisPrimary is RA/Azimuth
// (moves East/West); AxisSecondary is Dec/Altitude (moves North/South).
type Axis int

const (
	AxisPrimary   Axis = 0
	AxisSecondary Axis = 1
)

// axisDir maps an axis + sign to a cardinal direction. Positive on the primary
// axis is East, positive on the secondary axis is North (matching the convention
// Alpaca clients use for moveaxis).
func axisDir(a Axis, positive bool) Direction {
	switch {
	case a == AxisPrimary && positive:
		return East
	case a == AxisPrimary:
		return West
	case positive:
		return North
	default:
		return South
	}
}

// MoveAxis selects the slew-rate preset and starts a continuous slew on the given
// axis in the sign's direction — the LX200 backing for Alpaca MoveAxis (the
// per-mount/wrapper layer maps the Alpaca deg/s rate to a Rate preset, since the
// preset-to-speed mapping is mount-specific). Use StopAxis to stop.
func (c *Conn) MoveAxis(a Axis, positive bool, rate Rate) error {
	if err := c.SetRate(rate); err != nil {
		return err
	}
	return c.Move(axisDir(a, positive))
}

// StopAxis halts motion on an axis, both directions (harmless if only one was
// moving) — the LX200 backing for Alpaca MoveAxis with rate 0.
func (c *Conn) StopAxis(a Axis) error {
	d1, d2 := East, West
	if a == AxisSecondary {
		d1, d2 = North, South
	}
	if err := c.HaltMove(d1); err != nil {
		return err
	}
	return c.HaltMove(d2)
}

// --- Tracking rate ---

// TrackSidereal selects the sidereal tracking rate (:TQ#).
func (c *Conn) TrackSidereal() error { return c.Blind(":TQ#") }

// TrackLunar selects the lunar tracking rate (:TL#).
func (c *Conn) TrackLunar() error { return c.Blind(":TL#") }

// TrackSolar selects the solar tracking rate (:TS#).
func (c *Conn) TrackSolar() error { return c.Blind(":TS#") }
