package tenmicron

import "fmt"

// PECTraining selects a periodic-error-correction training duration (:pRX# family).
type PECTraining int

const (
	PECShort  PECTraining = 0 // ~15 min at sidereal speed
	PECMedium PECTraining = 1 // ~30 min
	PECLong   PECTraining = 2 // ~60 min
)

// StopPEC stops periodic error correction (:p#). No effect on HPS mounts, which have
// no PEC.
func (m *Mount) StopPEC() error { return m.Blind(":p#") }

// StartPEC activates periodic error correction (:pP#). No effect on HPS mounts, which
// have no PEC.
func (m *Mount) StartPEC() error { return m.Blind(":pP#") }

// TrainPEC starts PEC training at the mount's default length (:pR#).
func (m *Mount) TrainPEC() error { return m.Blind(":pR#") }

// TrainPECLength starts PEC training of the given duration (:pRX#).
func (m *Mount) TrainPECLength(d PECTraining) error {
	return m.Blind(fmt.Sprintf(":pR%d#", int(d)))
}

// TrainPECAltitude starts PEC training of the altitude axis of an altazimuth mount
// (:pRaX#).
func (m *Mount) TrainPECAltitude(d PECTraining) error {
	return m.Blind(fmt.Sprintf(":pRa%d#", int(d)))
}

// TrainPECAzimuth starts PEC training of the azimuth axis of an altazimuth mount
// (:pRzX#).
func (m *Mount) TrainPECAzimuth(d PECTraining) error {
	return m.Blind(fmt.Sprintf(":pRz%d#", int(d)))
}
