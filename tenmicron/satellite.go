package tenmicron

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// LoadTLE loads two-line orbital elements (:TLEL0…#). The string is sent verbatim
// and should use the protocol's escaped-newline mechanism between lines. Errors if
// the mount reports an invalid format.
func (m *Mount) LoadTLE(tle string) error {
	s, err := m.Get(":TLEL0" + tle + "#")
	if err != nil {
		return err
	}
	if strings.TrimSpace(s) != "V" {
		return fmt.Errorf("gotenmicron: TLE rejected (%q)", s)
	}
	return nil
}

// LoadedTLE returns the currently-loaded TLE text (:TLEG#), escaped per the
// protocol; errors if none is loaded.
func (m *Mount) LoadedTLE() (string, error) {
	s, err := m.Get(":TLEG#")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(s) == "E" {
		return "", errors.New("gotenmicron: no TLE loaded")
	}
	return s, nil
}

// SatelliteEquatorial returns the satellite's apparent RA (hours) and Dec (deg)
// for the loaded TLE at the given UTC Julian Date (:TLEGEQ…#).
func (m *Mount) SatelliteEquatorial(jd float64) (raHours, decDeg float64, err error) {
	return m.satPair(fmt.Sprintf(":TLEGEQ%.6f#", jd))
}

// SatelliteHorizontal returns the satellite's apparent altitude and azimuth (deg)
// for the loaded TLE at the given UTC Julian Date (:TLEGAZ…#).
func (m *Mount) SatelliteHorizontal(jd float64) (altDeg, azDeg float64, err error) {
	return m.satPair(fmt.Sprintf(":TLEGAZ%.6f#", jd))
}

func (m *Mount) satPair(cmd string) (float64, float64, error) {
	s, err := m.Get(cmd)
	if err != nil {
		return 0, 0, err
	}
	s = strings.TrimSpace(s)
	if s == "E" {
		return 0, 0, errors.New("gotenmicron: no TLE loaded")
	}
	f := strings.Split(s, ",")
	if len(f) != 2 {
		return 0, 0, fmt.Errorf("gotenmicron: bad satellite reply %q", s)
	}
	a, _ := strconv.ParseFloat(strings.TrimSpace(f[0]), 64)
	b, _ := strconv.ParseFloat(strings.TrimSpace(f[1]), 64)
	return a, b, nil
}

// Transit is a satellite pass / trajectory window in UTC Julian Dates.
type Transit struct {
	JDStart, JDEnd float64
	Flip           bool // mount will flip during the transit (flag "F")
}

// ErrNoSatellitePass is returned by PrecalcTransit when no pass falls in the
// requested window (mount replies "N#").
var ErrNoSatellitePass = errors.New("gotenmicron: no satellite pass in the given interval")

// PrecalcTransit precalculates the first transit of the loaded satellite starting
// at the given UTC Julian Date over min minutes (1..1440) (:TLEP…#).
func (m *Mount) PrecalcTransit(jd float64, min int) (Transit, error) {
	s, err := m.Get(fmt.Sprintf(":TLEP%.6f,%d#", jd, min))
	if err != nil {
		return Transit{}, err
	}
	switch strings.TrimSpace(s) {
	case "E":
		return Transit{}, errors.New("gotenmicron: no TLE loaded or invalid command")
	case "N":
		return Transit{}, ErrNoSatellitePass
	}
	return parseTransit(strings.TrimSpace(s))
}

// TransitSlewState is the status of a satellite-transit slew (:TLESCK#).
type TransitSlewState int

const (
	TransitSlewing  TransitSlewState = iota // V: slewing to transit start
	TransitWaiting                          // P: at start, waiting for the satellite
	TransitCatching                         // S: slewing to catch the satellite
	TransitTracking                         // T: tracking the satellite
	TransitEnded                            // Q: transit ended, not tracking
	TransitUnknown                          // unrecognized
)

// SlewToTransit slews to the precalculated transit start; the mount then auto-starts
// tracking the satellite (:TLES#). Errors if no transit is precalculated or the
// slew is blocked.
func (m *Mount) SlewToTransit() error {
	s, err := m.Get(":TLES#")
	if err != nil {
		return err
	}
	switch strings.TrimSpace(s) {
	case "V":
		m.invalidate()
		return nil
	case "E":
		return errors.New("gotenmicron: no transit precalculated")
	case "F":
		return errors.New("gotenmicron: transit slew blocked (parked or other state)")
	default:
		return fmt.Errorf("gotenmicron: unexpected :TLES# reply %q", s)
	}
}

// TransitSlewStatus returns the status of the satellite-transit slew (:TLESCK#).
func (m *Mount) TransitSlewStatus() (TransitSlewState, error) {
	s, err := m.Get(":TLESCK#")
	if err != nil {
		return TransitUnknown, err
	}
	switch strings.TrimSpace(s) {
	case "V":
		return TransitSlewing, nil
	case "P":
		return TransitWaiting, nil
	case "S":
		return TransitCatching, nil
	case "T":
		return TransitTracking, nil
	case "Q":
		return TransitEnded, nil
	default:
		return TransitUnknown, nil
	}
}

// NewTrajectory begins an arbitrary alt/az trajectory starting at the given UTC
// Julian Date (:TRNEW…#); add points with AddTrajectoryPoint, then PrecalcTrajectory.
func (m *Mount) NewTrajectory(jd float64) error {
	s, err := m.Get(fmt.Sprintf(":TRNEW%.6f#", jd))
	if err != nil {
		return err
	}
	if strings.TrimSpace(s) != "V" {
		return fmt.Errorf("gotenmicron: TRNEW rejected (%q)", s)
	}
	return nil
}

// AddTrajectoryPoint adds an alt/az point to the trajectory (1 s spacing, up to
// 900 points) (:TRADD…#) and returns the running point count.
func (m *Mount) AddTrajectoryPoint(azDeg, altDeg float64) (int, error) {
	s, err := m.Get(fmt.Sprintf(":TRADD%.5f,%.5f#", azDeg, altDeg))
	if err != nil {
		return 0, err
	}
	s = strings.TrimSpace(s)
	if s == "E" {
		return 0, errors.New("gotenmicron: invalid trajectory point or limit exceeded")
	}
	return strconv.Atoi(s)
}

// PrecalcTrajectory precalculates the loaded arbitrary trajectory (:TRP#),
// returning its UTC Julian-Date span. Follow it with SlewToTransit.
func (m *Mount) PrecalcTrajectory() (Transit, error) {
	s, err := m.Get(":TRP#")
	if err != nil {
		return Transit{}, err
	}
	if strings.TrimSpace(s) == "N" {
		return Transit{}, errors.New("gotenmicron: no trajectory defined or cannot be computed")
	}
	return parseTransit(strings.TrimSpace(s))
}

// --- TLE database (:TLEDN# / :TLEDLn#) --------------------------------------

// DatabaseTLECount returns the number of TLEs stored in the mount's database
// (:TLEDN#, firmware ≥ 2.13.20).
func (m *Mount) DatabaseTLECount() (int, error) { return m.getInt(":TLEDN#") }

// LoadDatabaseTLE loads the orbital elements at database index n (1..DatabaseTLECount)
// and returns the loaded TLE text (:TLEDLn#). Errors if no TLE has that index.
func (m *Mount) LoadDatabaseTLE(n int) (string, error) {
	s, err := m.Get(fmt.Sprintf(":TLEDL%d#", n))
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(s) == "E" {
		return "", fmt.Errorf("gotenmicron: no TLE at database index %d", n)
	}
	return s, nil
}

// --- Trajectory replay + real-time offsets (:TRREPLAY# / :TROFF*) -----------

// ReplayTrajectory precalculates the loaded arbitrary trajectory anchored to the
// current time (:TRREPLAY#) — like PrecalcTrajectory but starting now — and returns its
// UTC Julian-Date span. Follow it with SlewToTransit. (Firmware ≥ 3.0.)
func (m *Mount) ReplayTrajectory() (Transit, error) {
	s, err := m.Get(":TRREPLAY#")
	if err != nil {
		return Transit{}, err
	}
	switch strings.TrimSpace(s) {
	case "E":
		return Transit{}, errors.New("gotenmicron: no arbitrary trajectory defined")
	case "N": // trajectory defined but no valid pass in the window
		return Transit{}, ErrNoSatellitePass
	}
	return parseTransit(strings.TrimSpace(s))
}

// TrajectoryOffset selects which real-time offset the :TROFF* commands act on while
// the mount is following a satellite or arbitrary trajectory.
type TrajectoryOffset int

const (
	OffsetAxis1    TrajectoryOffset = 1 // first axis (RA/az), arcsec, ±1800.0
	OffsetAxis2    TrajectoryOffset = 2 // second axis (Dec/alt), arcsec, ±1800.0
	OffsetAxis1Sky TrajectoryOffset = 3 // first axis × cos(dec|alt)⁻¹ (constant sky angle), arcsec, ±1800.0
	OffsetTime     TrajectoryOffset = 4 // time offset, milliseconds, ±1000.0
)

// AddTrajectoryOffset adds to a trajectory-following offset (:TROFFADDid,±v#): value is
// arcseconds (ids 1–3, ±1800.0) or milliseconds (id 4, ±1000.0). Errors if the mount
// is not following a trajectory or the value is out of range. (Firmware ≥ 3.0.)
func (m *Mount) AddTrajectoryOffset(id TrajectoryOffset, value float64) error {
	return m.trajOffset("ADD", id, value)
}

// SetTrajectoryOffset replaces a trajectory-following offset with a new value
// (:TROFFSETid,±v#); see AddTrajectoryOffset for units and ranges.
func (m *Mount) SetTrajectoryOffset(id TrajectoryOffset, value float64) error {
	return m.trajOffset("SET", id, value)
}

func (m *Mount) trajOffset(verb string, id TrajectoryOffset, value float64) error {
	s, err := m.Get(fmt.Sprintf(":TROFF%s%d,%+07.1f#", verb, int(id), value))
	if err != nil {
		return err
	}
	if strings.TrimSpace(s) != "V" { // "V#" applied, "E#" not following (or invalid id/value)
		return fmt.Errorf("gotenmicron: trajectory offset %d not applied — not following a trajectory (or invalid id/value)", int(id))
	}
	return nil
}

// TrajectoryOffsetValue reads the current value of a trajectory-following offset
// (:TROFFGETid#): arcseconds (ids 1–3) or milliseconds (id 4). The mount replies "E#"
// when it is not following a trajectory (or the id is invalid) — the offsets only exist
// while following, so an idle mount returns "E#" for every id. (Firmware ≥ 3.1.4.)
func (m *Mount) TrajectoryOffsetValue(id TrajectoryOffset) (float64, error) {
	s, err := m.Get(fmt.Sprintf(":TROFFGET%d#", int(id)))
	if err != nil {
		return 0, err
	}
	s = strings.TrimSpace(s)
	if s == "E" {
		return 0, fmt.Errorf("gotenmicron: trajectory offset %d unavailable — not following a trajectory (or invalid id)", int(id))
	}
	return strconv.ParseFloat(s, 64)
}

// ClearTrajectoryOffsets clears all trajectory-following offsets (:TROFFCLR#). Errors
// if the mount is not following a trajectory. (Firmware ≥ 3.0.)
func (m *Mount) ClearTrajectoryOffsets() error {
	s, err := m.Get(":TROFFCLR#")
	if err != nil {
		return err
	}
	if strings.TrimSpace(s) != "V" { // "V#" cleared, "E#" not following
		return errors.New("gotenmicron: not following a trajectory")
	}
	return nil
}

// parseTransit decodes "JDstart,JDend,flags".
func parseTransit(s string) (Transit, error) {
	f := strings.Split(s, ",")
	if len(f) < 2 {
		return Transit{}, fmt.Errorf("gotenmicron: bad transit reply %q", s)
	}
	var tr Transit
	tr.JDStart, _ = strconv.ParseFloat(strings.TrimSpace(f[0]), 64)
	tr.JDEnd, _ = strconv.ParseFloat(strings.TrimSpace(f[1]), 64)
	if len(f) > 2 && strings.Contains(f[2], "F") {
		tr.Flip = true
	}
	return tr, nil
}
