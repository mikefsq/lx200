package tenmicron

import (
	"fmt"
	"strconv"
	"strings"
)

// DitherParams is the dithering configuration (:GditP#).
type DitherParams struct {
	RAArcsec, DecArcsec                float64 // dither amplitude (arcsec)
	DelaySec, ExposureSec, IntervalSec float64 // seconds
}

// StartDithering / StopDithering / DitherNow control dithering (:SditS#/:SditQ#/
// :SditN#); each reports whether the command succeeded. DitherNow requires
// dithering to be active.
func (m *Mount) StartDithering() (bool, error) { return m.getBool(":SditS#") }
func (m *Mount) StopDithering() (bool, error)  { return m.getBool(":SditQ#") }
func (m *Mount) DitherNow() (bool, error)      { return m.getBool(":SditN#") }

// DitheringActive reports whether dithering is active (:GditS#).
func (m *Mount) DitheringActive() (bool, error) { return m.getBool(":GditS#") }

// SetDitherAmount sets the RA/Dec dither amplitude in arcseconds (range 0..30)
// (:SditM…#); reports whether the mount accepted it.
func (m *Mount) SetDitherAmount(raArcsec, decArcsec float64) (bool, error) {
	return m.getBool(fmt.Sprintf(":SditM%.0f,%.0f#", raArcsec, decArcsec))
}

// DitherParameters reads the dithering configuration (:GditP#).
func (m *Mount) DitherParameters() (DitherParams, error) {
	s, err := m.Get(":GditP#")
	if err != nil {
		return DitherParams{}, err
	}
	f := strings.Split(strings.TrimSpace(s), ",")
	if len(f) < 5 {
		return DitherParams{}, fmt.Errorf("gotenmicron: short :GditP# reply %q", s)
	}
	val := func(x string) float64 { v, _ := strconv.ParseFloat(strings.TrimSpace(x), 64); return v }
	return DitherParams{
		RAArcsec:    val(f[0]),
		DecArcsec:   val(f[1]),
		DelaySec:    val(f[2]),
		ExposureSec: val(f[3]),
		IntervalSec: val(f[4]),
	}, nil
}
