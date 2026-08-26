package rst

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// FindQuality is the minimum object quality the mount's FIND will accept.
type FindQuality string

// The quality codes, superb through very poor.
const (
	QualitySuperb    FindQuality = "SU"
	QualityExcellent FindQuality = "EX"
	QualityVeryGood  FindQuality = "VG"
	QualityGood      FindQuality = "GD"
	QualityFair      FindQuality = "FR"
	QualityPoor      FindQuality = "PR"
	QualityVeryPoor  FindQuality = "VP"
)

// trimUnit strips the unit character the mount appends to some replies and parses the rest.
func trimUnit(s string) (float64, error) {
	return strconv.ParseFloat(strings.TrimRight(strings.TrimSpace(s), "'*\""), 64)
}

func (m *Mount) bareFloat(cmd string) (float64, error) {
	s, err := m.Get(cmd)
	if err != nil {
		return 0, err
	}
	v, err := trimUnit(s)
	if err != nil {
		return 0, fmt.Errorf("rainbow: %s: %q: %w", cmd, s, err)
	}
	return v, nil
}

// FieldDiameter reads the FIND field diameter in arcminutes (:GF#).
func (m *Mount) FieldDiameter() (float64, error) { return m.bareFloat(":GF#") }

// SetFieldDiameter sets the FIND field diameter in arcminutes (:SF#).
func (m *Mount) SetFieldDiameter(arcmin int) error {
	return must1(m.Ack(fmt.Sprintf(":SF%03d#", arcmin)))
}

// BrightMagnitudeLimit reads the browse bright-magnitude limit (:Gb#).
func (m *Mount) BrightMagnitudeLimit() (float64, error) { return m.bareFloat(":Gb#") }

// SetBrightMagnitudeLimit sets the bright-magnitude limit (:Sb#).
func (m *Mount) SetBrightMagnitudeLimit(mag float64) error {
	return must1(m.Ack(fmt.Sprintf(":Sb%+04d#", int(math.Round(mag*10)))))
}

// FaintMagnitudeLimit reads the browse faint-magnitude limit (:Gf#).
func (m *Mount) FaintMagnitudeLimit() (float64, error) { return m.bareFloat(":Gf#") }

// SetFaintMagnitudeLimit sets the faint-magnitude limit (:Sf#).
func (m *Mount) SetFaintMagnitudeLimit(mag float64) error {
	return must1(m.Ack(fmt.Sprintf(":Sf%+04d#", int(math.Round(mag*10)))))
}

// HigherAltitudeLimit reads the upper altitude limit in degrees (:Gh#).
func (m *Mount) HigherAltitudeLimit() (float64, error) { return m.bareFloat(":Gh#") }

// SetHigherAltitudeLimit sends :Sh#.
func (m *Mount) SetHigherAltitudeLimit(deg int) error {
	return must1(m.Ack(fmt.Sprintf(":Sh%02d#", deg)))
}

// SmallerSizeLimit reads the smaller object-size limit for FIND, in arcminutes (:Gs#).
func (m *Mount) SmallerSizeLimit() (float64, error) { return m.bareFloat(":Gs#") }

// SetSmallerSizeLimit sets the smaller size limit. It sends :Sl#; see SmallerSizeLimit.
func (m *Mount) SetSmallerSizeLimit(arcmin int) error {
	return must1(m.Ack(fmt.Sprintf(":Sl%03d#", arcmin)))
}

// LargerSizeLimit reads the larger object-size limit in arcminutes (:Gl#).
func (m *Mount) LargerSizeLimit() (float64, error) { return m.bareFloat(":Gl#") }

// SetLargerSizeLimit sets the larger size limit. It sends :Ss#; see SmallerSizeLimit.
func (m *Mount) SetLargerSizeLimit(arcmin int) error {
	return must1(m.Ack(fmt.Sprintf(":Ss%03d#", arcmin)))
}

// MinimumQuality reads the minimum quality FIND will accept (:Gq#).
func (m *Mount) MinimumQuality() (FindQuality, error) {
	s, err := m.Get(":Gq#")
	return FindQuality(strings.TrimSpace(s)), err
}

// SetMinimumQuality sets the minimum FIND quality (:Sq#).
func (m *Mount) SetMinimumQuality(q FindQuality) error { return m.Blind(":Sq" + string(q) + "#") }

// SearchString reads the deep-sky object filter (:Gy#).
func (m *Mount) SearchString() (string, error) {
	s, err := m.Get(":Gy#")
	return strings.TrimSpace(s), err
}

// SetSearchString sets the deep-sky filter (:Sy#).
func (m *Mount) SetSearchString(flags string) error { return m.Blind(":Sy" + flags + "#") }

// must1 turns the mount's ack byte into an error.
func must1(ok bool, err error) error {
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("rainbow: the mount rejected the value")
	}
	return nil
}
