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

// StartDithering starts dithering (:SditS#); reports whether the command succeeded.
func (m *Mount) StartDithering() (bool, error) { return m.getBool(":SditS#") }

// StopDithering stops dithering (:SditQ#); reports whether the command succeeded.
func (m *Mount) StopDithering() (bool, error) { return m.getBool(":SditQ#") }

// DitherNow dithers immediately (:SditN#); dithering must already be active. Reports
// whether the command succeeded.
func (m *Mount) DitherNow() (bool, error) { return m.getBool(":SditN#") }

// DitheringActive reports whether dithering is active (:GditS#).
func (m *Mount) DitheringActive() (bool, error) { return m.getBool(":GditS#") }

// SetDitherAmount sets the RA/Dec dither amplitude in arcseconds (range 0..30)
// (:SditM…#); reports whether the mount accepted it.
func (m *Mount) SetDitherAmount(raArcsec, decArcsec float64) (bool, error) {
	return m.getBool(fmt.Sprintf(":SditM%.0f,%.0f#", raArcsec, decArcsec))
}

// SetDitherTimer configures the automatic dithering timer (:SditT#): delaySec before the
// first dither (0..356400), exposureSec (0..356400), and intervalSec between dithers
// (5..356400). Reports whether the mount accepted it. (Firmware ≥ 2.14.)
func (m *Mount) SetDitherTimer(delaySec, exposureSec, intervalSec int) (bool, error) {
	if delaySec < 0 || delaySec > 356400 || exposureSec < 0 || exposureSec > 356400 ||
		intervalSec < 5 || intervalSec > 356400 {
		return false, fmt.Errorf("gotenmicron: dither timer out of range (delay/exposure 0..356400s, interval 5..356400s)")
	}
	return m.getBool(fmt.Sprintf(":SditT%d,%d,%d#", delaySec, exposureSec, intervalSec))
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
