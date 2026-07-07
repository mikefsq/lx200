package tenmicron

import (
	"fmt"
	"math"
)

// Axis backlash compensation (:Bd# / :Br#), given in arcseconds and sent as the mount's
// DD*MM:SS angle form.

// SetDecBacklash sets the declination/altitude axis backlash compensation, in
// arcseconds (:BdDD*MM:SS#).
func (m *Mount) SetDecBacklash(arcsec float64) error {
	return must(m.Ack(":Bd" + backlashDMS(arcsec) + "#"))
}

// SetRABacklash sets the RA/azimuth axis backlash compensation, in arcseconds
// (:BrDD*MM:SS#).
func (m *Mount) SetRABacklash(arcsec float64) error {
	return must(m.Ack(":Br" + backlashDMS(arcsec) + "#"))
}

// backlashDMS formats a non-negative arcsecond value as DD*MM:SS.
func backlashDMS(arcsec float64) string {
	if arcsec < 0 {
		arcsec = 0
	}
	t := int(math.Round(arcsec))
	return fmt.Sprintf("%02d*%02d:%02d", t/3600, (t/60)%60, t%60)
}
