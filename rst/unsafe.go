package rst

import (
	"fmt"
	"strings"
)

// Commands that write persistent configuration or drive undocumented hardware.
//
// Every command here is write only.
type Unsafe struct{ m *Mount }

// Unsafe exposes the destructive and unverified command families. See the Unsafe type.
func (m *Mount) Unsafe() Unsafe { return Unsafe{m} }

// --- factory calibration (:A) -----------------------------------------------

// SetGearRatio writes the RA and Dec gear ratios (:Ag#). Persists to NVM. Every rate and slew
// distance derives from these, and they are per-unit.
func (u Unsafe) SetGearRatio(ra, dec int) error {
	return u.m.Blind(fmt.Sprintf(":Ag%09d*%09d#", ra, dec))
}

// SetWormCount writes the RA and Dec worm counts (:Ap#). Persists to NVM.
func (u Unsafe) SetWormCount(ra, dec int) error {
	return u.m.Blind(fmt.Sprintf(":Ap%04d*%04d#", ra, dec))
}

// SetSerialNumber writes the mount's 6-digit serial number (:As#). Persists to NVM, and the
// WiFi SSID is derived from it.
func (u Unsafe) SetSerialNumber(n int) error { return u.m.Blind(fmt.Sprintf(":As%06d#", n)) }

// SetModelName writes the model-name string (:Am#). Persists to NVM.
func (u Unsafe) SetModelName(s string) error { return u.m.Blind(":Am" + s + "#") }

// WriteFactoryBlockA writes model, gear, worm and serial together (:Aa#). The field order is
// inferred from the firmware's format string and has not been verified; raw is passed through
// verbatim.
func (u Unsafe) WriteFactoryBlockA(raw string) error { return u.m.Blind(":Aa" + raw + "#") }

// WriteFactoryBlockB writes the four calibration values of :Ab#, as WriteFactoryBlockA.
func (u Unsafe) WriteFactoryBlockB(raw string) error { return u.m.Blind(":Ab" + raw + "#") }

// SetDebugReport enables (:AX#) or disables (:Ax#) the firmware's debug report.
func (u Unsafe) SetDebugReport(on bool) error {
	if on {
		return u.m.Blind(":AX#")
	}
	return u.m.Blind(":Ax#")
}

// SelectLX200Dialect switches the mount out of the Rainbow dialect (:AL#), clearing both the
// prefix echo and high-precision coordinates. This driver depends on the echo, so every later
// read fails until :AR# restores it. Use EchoPrefix and SetPrecision for the individual flags.
func (u Unsafe) SelectLX200Dialect() error { return u.m.Blind(":AL#") }

// SelectWiFiTransport switches the mount to its WiFi transport (:AW#), which ends a USB
// session.
func (u Unsafe) SelectWiFiTransport() error { return u.m.Blind(":AW#") }

// SetTransmitFlag writes the byte flag :Ba# that the firmware reads in its transmit routine.
// What it gates is not established.
func (u Unsafe) SetTransmitFlag(v int) error { return u.m.Blind(fmt.Sprintf(":Ba%d#", v)) }

// --- external SPI memory (:F) -----------------------------------------------
//
// The :F family bit-bangs an external SPI non-volatile memory using the 25-series command set.
// What that memory holds is not known, and none of these has been run on hardware.

// SPIWriteA sends :Fa# with a 6-digit address and 4-digit datum, through the write-enable path.
func (u Unsafe) SPIWriteA(addr, data int) error {
	return u.m.Blind(fmt.Sprintf(":Fa%06d?%04d#", addr, data))
}

// SPIWriteD sends :Fd#, the second write variant, with the same argument shape.
func (u Unsafe) SPIWriteD(addr, data int) error {
	return u.m.Blind(fmt.Sprintf(":Fd%06d?%04d#", addr, data))
}

// SPIRaw sends any other :F command with a verbatim argument. Valid second characters are
// B b C c E e F g r t; their roles are not established.
func (u Unsafe) SPIRaw(c byte, arg string) error {
	if !strings.ContainsRune("BbCcEeFgrt", rune(c)) {
		return fmt.Errorf("rainbow: :F%c is not a known SPI-memory command", c)
	}
	return u.m.Blind(fmt.Sprintf(":F%c%s#", c, arg))
}

// --- engineering diagnostics (:X) -------------------------------------------

// Diagnostic sends a :X factory-diagnostic command with a verbatim argument. Valid second
// characters are A B C D E e F G H P R. None has been run on hardware.
//
// Several are destructive. :XR controls the Renishaw absolute encoder on the 135E, :XP can
// erase recorded PEC data, and :XC and :XD reach the NVM block holding gear ratio, worm count
// and serial number. Replies are unframed "%Q" text, which leaves loose bytes in the stream
// for the next read.
func (u Unsafe) Diagnostic(c byte, arg string) error {
	if !strings.ContainsRune("ABCDEeFGHPR", rune(c)) {
		return fmt.Errorf("rainbow: :X%c is not a known diagnostic command", c)
	}
	return u.m.Blind(fmt.Sprintf(":X%c%s#", c, arg))
}

// --- periodic error correction (:P) -----------------------------------------

// PEC sends one of the :P commands. Valid second characters are A a D F P p U u; A/a and P/p
// are on/off pairs and U/u read the recorded data.
//
// The family is absent from the 135E, which has absolute encoders instead. It is documented
// from the RST-300 image and has never been run.
func (u Unsafe) PEC(c byte) error {
	if !strings.ContainsRune("AaDFPpUu", rune(c)) {
		return fmt.Errorf("rainbow: :P%c is not a known PEC command", c)
	}
	return u.m.Blind(fmt.Sprintf(":P%c#", c))
}
