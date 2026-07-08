package tenmicron

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/mikefsq/lx200"
)

// --- Alignment-model build sequence (:newalig# / :newalpt / :endalig#) -------

// StartAlignment begins a new alignment specification (:newalig#). It does not
// clear the current model until EndAlignment recomputes it. Add points with
// AddAlignmentSpecPoint, then call EndAlignment.
func (m *Mount) StartAlignment() error {
	s, err := m.Get(":newalig#")
	if err != nil {
		return err
	}
	if strings.TrimSpace(s) != "V" {
		return fmt.Errorf("gotenmicron: newalig rejected (%q)", s)
	}
	return nil
}

// AlignmentPoint is one plate-solved alignment sample for AddAlignmentSpecPoint:
// the mount-reported pointing, the side of pier, the plate-solved pointing, and
// the local sidereal time at the measurement (RA/sidereal in hours, Dec in deg).
type AlignmentPoint struct {
	MountRA, MountDec   float64
	MountSide           lx200.PierSide
	SolvedRA, SolvedDec float64
	SiderealTime        float64
}

// AddAlignmentSpecPoint adds a point to the in-progress alignment specification
// (:newalpt…#) and returns the running point count. Must be between StartAlignment
// and EndAlignment.
func (m *Mount) AddAlignmentSpecPoint(p AlignmentPoint) (int, error) {
	side := "E"
	if p.MountSide == lx200.PierWest {
		side = "W"
	}
	cmd := fmt.Sprintf(":newalpt%s,%s,%s,%s,%s,%s#",
		hmsTenths(p.MountRA), dmsColon(p.MountDec), side,
		hmsTenths(p.SolvedRA), dmsColon(p.SolvedDec), hmsTenths(p.SiderealTime))
	s, err := m.Get(cmd)
	if err != nil {
		return 0, err
	}
	s = strings.TrimSpace(s)
	if s == "E" {
		return 0, fmt.Errorf("gotenmicron: alignment point rejected")
	}
	return strconv.Atoi(s)
}

// EndAlignment finalizes the alignment specification and computes a new model
// (:endalig#). Errors if the model can't be computed (reply "E#").
func (m *Mount) EndAlignment() error {
	s, err := m.Get(":endalig#")
	if err != nil {
		return err
	}
	if strings.TrimSpace(s) != "V" {
		return fmt.Errorf("gotenmicron: endalig failed (%q)", s)
	}
	m.invalidate()
	return nil
}

// DeleteAlignment deletes the current alignment model and its stars (:delalig#).
func (m *Mount) DeleteAlignment() error {
	if _, err := m.Get(":delalig#"); err != nil { // reply is an empty "#"
		return err
	}
	m.invalidate()
	return nil
}

// --- Alignment-model query (:getalst# / :getain# / :getaliN#) ----------------

// AlignmentStarCount returns the number of alignment stars in the current model
// (:getalst#).
func (m *Mount) AlignmentStarCount() (int, error) { return m.getInt(":getalst#") }

// MaxAlignmentStars returns the maximum number of alignment stars the model can hold
// (:getalstm#, firmware ≥ 3.3.1).
func (m *Mount) MaxAlignmentStars() (int, error) { return m.getInt(":getalstm#") }

// ErrModelTooFewStars is returned by AlignmentInfo when the model has fewer than
// two stars (mount replies "E#").
var ErrModelTooFewStars = errors.New("gotenmicron: alignment model has fewer than two stars")

// AlignmentInfo is the decoded :getain# model summary. Fields the mount does not
// compute (per mount type / star count) are NaN (or Terms = -1).
type AlignmentInfo struct {
	Azimuth, Altitude float64 // direction of the RA axis (deg)
	PolarError        float64 // polar-align error (deg)
	PositionAngle     float64 // RA-axis position angle wrt celestial pole (deg)
	OrthoError        float64 // optical/declination-axis orthogonality error (deg)
	AzTurns, AltTurns float64 // adjustment-knob turns (az +: move left; alt +: down)
	Terms             int     // number of modeling terms (-1 if not computed)
	RMSArcsec         float64 // expected RMS error (arcsec)
}

// AlignmentInfo reads the current model summary (:getain#).
func (m *Mount) AlignmentInfo() (AlignmentInfo, error) {
	s, err := m.Get(":getain#")
	if err != nil {
		return AlignmentInfo{}, err
	}
	s = strings.TrimSpace(s)
	if s == "E" {
		return AlignmentInfo{}, ErrModelTooFewStars
	}
	f := strings.Split(s, ",")
	if len(f) < 9 {
		return AlignmentInfo{}, fmt.Errorf("gotenmicron: short :getain# reply %q", s)
	}
	info := AlignmentInfo{
		Azimuth:       floatOrNaN(f[0]),
		Altitude:      floatOrNaN(f[1]),
		PolarError:    floatOrNaN(f[2]),
		PositionAngle: floatOrNaN(f[3]),
		OrthoError:    floatOrNaN(f[4]),
		AzTurns:       floatOrNaN(f[5]),
		AltTurns:      floatOrNaN(f[6]),
		RMSArcsec:     floatOrNaN(f[8]),
		Terms:         -1,
	}
	if n, e := strconv.Atoi(strings.TrimSpace(f[7])); e == nil {
		info.Terms = n
	}
	return info, nil
}

// AlignmentStar returns the raw :getaliN# record for star n (1..AlignmentStarCount).
// The per-star reply is a long CSV the spec documents field-by-field; it is
// returned unparsed for callers that need it. Errors ("E#") surface as the string.
func (m *Mount) AlignmentStar(n int) (string, error) {
	return m.Get(fmt.Sprintf(":getali%d#", n))
}

// AlignmentPointInfo is the decoded :getalpN# per-star residual.
type AlignmentPointInfo struct {
	HourAngle   float64 // hour angle of the alignment star, hours (0..24)
	Dec         float64 // declination of the alignment star, degrees
	ErrorArcsec float64 // error between the star and the model, arcseconds
	PolarAngle  int     // polar angle of the measured vs modeled star, degrees (0..359, 0=north, 90=east); 0 when the firmware omits the field
}

// AlignmentPointInfo reads the residual for alignment star n (1..AlignmentStarCount)
// (:getalpN#). Errors ("E#") if n is out of range. (Firmware ≥ 2.8.15; note a
// pre-2.15.18 bug can require an Align-Info refresh — see the spec.)
func (m *Mount) AlignmentPointInfo(n int) (AlignmentPointInfo, error) {
	s, err := m.Get(fmt.Sprintf(":getalp%d#", n))
	if err != nil {
		return AlignmentPointInfo{}, err
	}
	s = strings.TrimSpace(s)
	if s == "E" {
		return AlignmentPointInfo{}, fmt.Errorf("gotenmicron: alignment star %d out of range", n)
	}
	f := strings.Split(s, ",")
	if len(f) < 3 {
		return AlignmentPointInfo{}, fmt.Errorf("gotenmicron: short :getalp# reply %q", s)
	}
	var info AlignmentPointInfo
	info.HourAngle, _ = lx200.ParseSexagesimal(f[0])
	info.Dec, _ = lx200.ParseSexagesimal(f[1])
	info.ErrorArcsec, _ = strconv.ParseFloat(strings.TrimSpace(f[2]), 64)
	if len(f) >= 4 { // the polar-angle field is absent on some firmware (e.g. 3.3.4)
		info.PolarAngle, _ = strconv.Atoi(strings.TrimSpace(f[3]))
	}
	return info, nil
}

// DeleteAlignmentStar deletes alignment star n and recomputes the model (:delalstN#).
// Errors ("0#") if n is out of range or the model cannot be recomputed. (Firmware ≥
// 2.8.15; the first two stars cannot be deleted before 2.10.6.)
func (m *Mount) DeleteAlignmentStar(n int) error {
	s, err := m.Get(fmt.Sprintf(":delalst%d#", n))
	if err != nil {
		return err
	}
	if strings.TrimSpace(s) != "1" { // "1#" success, "0#" failure
		return fmt.Errorf("gotenmicron: delete alignment star %d failed (%q)", n, s)
	}
	m.invalidate()
	return nil
}

// --- Named alignment-model slots (:modelcnt / :modelnam / :modelld / …) ------
// Save, name, load and delete whole alignment models stored in the mount (firmware ≥
// 2.13.3). Model names are case-sensitive, at most 15 characters, with trailing spaces
// ignored, and are escaped on the wire (see escape.go).

// SavedModelCount returns the number of alignment models stored in the mount
// (:modelcnt#).
func (m *Mount) SavedModelCount() (int, error) { return m.getInt(":modelcnt#") }

// SavedModelName returns the name of stored model n (1..SavedModelCount) (:modelnamN#).
// Errors if n is out of range (the mount replies a bare "#").
func (m *Mount) SavedModelName(n int) (string, error) {
	s, err := m.Get(fmt.Sprintf(":modelnam%d#", n))
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(s) == "" {
		return "", fmt.Errorf("gotenmicron: no saved model at index %d", n)
	}
	return strings.TrimRight(unescapeString(s), " "), nil
}

// LoadModel loads a stored alignment model by name (:modelld0NAME#).
func (m *Mount) LoadModel(name string) error {
	if err := m.modelSlot("modelld0", name, "load"); err != nil {
		return err
	}
	m.invalidate()
	return nil
}

// SaveModel stores the current alignment model under a name (:modelsv0NAME#).
func (m *Mount) SaveModel(name string) error { return m.modelSlot("modelsv0", name, "save") }

// DeleteModel deletes a stored alignment model by name (:modeldel0NAME#).
func (m *Mount) DeleteModel(name string) error { return m.modelSlot("modeldel0", name, "delete") }

func (m *Mount) modelSlot(verb, name, action string) error {
	s, err := m.Get(":" + verb + escapeString(name) + "#")
	if err != nil {
		return err
	}
	if strings.TrimSpace(s) != "1" { // "1#" success, "0#" error
		return fmt.Errorf("gotenmicron: model %s %q failed (%q)", action, name, s)
	}
	return nil
}

// --- formatters -------------------------------------------------------------

// hmsTenths formats hours as "HH:MM:SS.S" (wrapping into [0,24)).
func hmsTenths(hours float64) string {
	t := int(math.Round(hours * 36000)) // tenths of a second of time
	t = ((t % 864000) + 864000) % 864000
	return fmt.Sprintf("%02d:%02d:%02d.%01d", t/36000, (t/600)%60, (t/10)%60, t%10)
}

// dmsColon formats signed degrees as "sDD:MM:SS".
func dmsColon(deg float64) string {
	sign := byte('+')
	if deg < 0 {
		sign, deg = '-', -deg
	}
	t := int(math.Round(deg * 3600))
	return fmt.Sprintf("%c%02d:%02d:%02d", sign, t/3600, (t/60)%60, t%60)
}

func floatOrNaN(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "E" || s == "" {
		return math.NaN()
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return math.NaN()
	}
	return v
}
