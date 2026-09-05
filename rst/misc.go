package rst

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/mikefsq/lx200"
)

// ForcePierFlip reads the forced-meridian-flip flag (:AF#). SetForcePierFlip writes it and is
// blind, so this is the only way to confirm it.
func (m *Mount) ForcePierFlip() (bool, error) {
	s, err := m.get(":AF#", ":AF")
	if err != nil {
		return false, err
	}
	return strings.HasPrefix(s, "1"), nil
}

// SetAutoResume enables or disables auto-resume (:Cr#). The argument is a letter, R or X; the
// firmware ignores anything else in silence. AutoResume reads it back.
func (m *Mount) SetAutoResume(on bool) error {
	if on {
		return m.Blind(":CrR#")
	}
	return m.Blind(":CrX#")
}

// TrackingRateHz reads the tracking rate as the emulated LX200 motor frequency in Hz (:GT#).
// 60.0 is sidereal. The reply carries no echo prefix.
func (m *Mount) TrackingRateHz() (float64, error) { return m.bareFloat(":GT#") }

// ModelName reads the model-name string (:AM#). Empty on the development mount.
func (m *Mount) ModelName() (string, error) {
	s, err := m.get(":AM#", ":AM")
	return strings.TrimSpace(s), err
}

// ClockFormat reads the 12- or 24-hour display format (:Gc#). The reply carries no echo prefix.
func (m *Mount) ClockFormat() (int, error) {
	s, err := m.Get(":Gc#")
	if err != nil {
		return 0, err
	}
	v, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0, fmt.Errorf("rainbow: :Gc#: %q: %w", s, err)
	}
	return v, nil
}

// ToggleClockFormat flips between 12- and 24-hour (:H#). The firmware handler is a toggle
// only; use SetClockFormat to select a specific value.
func (m *Mount) ToggleClockFormat() error { return m.Blind(":H#") }

// SetClockFormat selects 12- or 24-hour by reading the current value and toggling if needed.
func (m *Mount) SetClockFormat(hours int) error {
	if hours != 12 && hours != 24 {
		return fmt.Errorf("rainbow: clock format must be 12 or 24, not %d", hours)
	}
	cur, err := m.ClockFormat()
	if err != nil {
		return err
	}
	if cur == hours {
		return nil
	}
	return m.ToggleClockFormat()
}

// The write makes the mount recompute its planetary ephemeris, so it is not a routine clock
// sync. SetUTC calls it only when the mount's date is wrong.
func (m *Mount) SetDate(mm, dd, yy int) error {
	want := fmt.Sprintf("%02d/%02d/%02d", mm, dd, yy)
	if _, err := m.Ack(":SC" + want + "#"); err != nil && !errors.Is(err, lx200.ErrTimeout) {
		return err
	}
	for i := 0; i < 2; i++ { // "Updating Planetary Data#" and " #"
		if _, err := m.Await(dateReplyWindow); err != nil {
			break
		}
	}
	got, err := m.Date()
	if err != nil {
		return fmt.Errorf("rainbow: :SC#: cannot read the date back: %w", err)
	}
	if strings.TrimSpace(got) != want {
		return fmt.Errorf("rainbow: :SC#: asked for %s, the mount reports %s", want, got)
	}
	return nil
}

// SyncCurrent syncs the mount to its own current position (:CM#), the plain Meade sync rather
// than the coordinate-carrying :Ck# used elsewhere here. Answers a CM frame.
func (m *Mount) SyncCurrent() (string, error) { return m.syncAck(":CM#") }

// StarAlign sends :CS#, which writes the same alignment global as :CM# and :CN#. Part of the
// star-alignment sequence; its exact role is not established.
func (m *Mount) StarAlign() error { return m.Blind(":CS#") }

// SetDecAxisOffset writes the Dec-axis alignment offset (:Cg#), the hand controller's "DE
// Offset". Read it back with PierSideOffset.
func (m *Mount) SetDecAxisOffset(deg float64) error {
	return m.Blind(fmt.Sprintf(":Cg%+08.4f#", deg))
}

// PierSideOffset reads the Dec-axis alignment offset, the hand controller's "DE Offset"
// (:CG3#). This is the calibration offset PierSide applies, not the axis angle itself.
func (m *Mount) PierSideOffset() (float64, error) {
	s, err := m.get(":CG3#", ":CG3")
	if err != nil {
		return 0, err
	}
	return strconv.ParseFloat(strings.TrimSpace(s), 64)
}

// GotoOffsetFlag reads :CL#, a single digit associated with the handset's Goto Offset menu.
// Its meaning is not established.
func (m *Mount) GotoOffsetFlag() (string, error) {
	s, err := m.get(":CL#", ":CL")
	return strings.TrimSpace(s), err
}

// SetSlewLimit writes one of the six axis-limit registers SlewLimits reads (:Ca# to :Cf#,
// index 0 to 5). The motion controller enforces these, and a goto that violates one is refused
// with an MSZZ# frame. Read back with SlewLimits.
func (m *Mount) SetSlewLimit(i int, v float64) error {
	if i < 0 || i > 5 {
		return fmt.Errorf("rainbow: slew-limit index %d out of range 0..5", i)
	}
	return m.Blind(fmt.Sprintf(":C%c%.3f#", 'a'+byte(i), v))
}

// SlewToTargetAlt sends :MD#, a second goto using the same alt/az target as :MA# and refusing
// the same way. How it differs from :MA# is not established.
func (m *Mount) SlewToTargetAlt() error { return m.slewCmd(":MD#", "MD") }

// MotionP sends :MP#, which answers :MP1# when accepted and :MP2# when refused. It writes the
// tracking flag :AT# reports, so it is a motion command rather than a query. Not run on
// hardware.
func (m *Mount) MotionP() (string, error) {
	s, err := m.get(":MP#", ":MP")
	return strings.TrimSpace(s), err
}

// SetRateRegister writes the rate register :RG#, :RC#, :RM# and :RS# select, via :MB# or :MC#
// with a 4-digit argument. SetGuideRate and SetRate are the usual way in.
func (m *Mount) SetRateRegister(upper bool, v int) error {
	c := byte('C')
	if upper {
		c = 'B'
	}
	return m.Blind(fmt.Sprintf(":M%c%04d#", c, v))
}

// SetEncoderRate sends :Mb# or :Mc#, which exist only on the 135E and write globals the :CU#
// family reads. The meanings are not established.
func (m *Mount) SetEncoderRate(lower byte, arg string) error {
	if lower != 'b' && lower != 'c' {
		return fmt.Errorf("rainbow: :M%c is not an encoder-rate command (b or c)", lower)
	}
	return m.Blind(fmt.Sprintf(":M%c%s#", lower, arg))
}

// CounterI reads :CI#, a five-digit field that was zero on the development mount.
func (m *Mount) CounterI() (string, error) {
	s, err := m.get(":CI#", ":CI")
	return strings.TrimSpace(s), err
}

// CounterJ reads :CJ#, as CounterI.
func (m *Mount) CounterJ() (string, error) {
	s, err := m.get(":CJ#", ":CJ")
	return strings.TrimSpace(s), err
}

// DebugDumpZ reads :CZ#, which emits an unframed debug line with no terminator of its own.
func (m *Mount) DebugDumpZ() (string, error) {
	if _, err := m.Get(":CZ#"); err != nil { // the framed, empty ":CZ" acknowledgement
		return "", err
	}
	// The text borrows the '#' of whatever reply comes next, and nothing marks where it ends,
	// so draining cannot separate them. Send a throwaway query to supply the terminator and
	// split on its echoed prefix: everything before ":GR" is the debug output.
	s, err := m.Get(":GR#")
	if err != nil {
		return "", err
	}
	if i := strings.Index(s, ":GR"); i >= 0 {
		return strings.TrimSpace(s[:i]), nil
	}
	return strings.TrimSpace(s), nil
}

// SetDebugZ sends the :Cz# counterpart of DebugDumpZ.
func (m *Mount) SetDebugZ(arg string) error { return m.Blind(":Cz" + arg + "#") }

// StatusW reads :CW#. A bare :CW# does not answer, so it takes an argument that has not been
// identified; pass one here.
func (m *Mount) StatusW(arg string) (string, error) { return m.Get(":CW" + arg + "#") }

// SetInitFlags sends :Cw#, which reads the firmware's "Flag Init" initialisation flags.
func (m *Mount) SetInitFlags(arg string) error { return m.Blind(":Cw" + arg + "#") }

// SetQ sends :CQ#, which writes one global nothing else in the parser reads.
func (m *Mount) SetQ(arg string) error { return m.Blind(":CQ" + arg + "#") }

// SetTrackingRelatedO sends :Co#, which shares globals with the :Ct* tracking family.
func (m *Mount) SetTrackingRelatedO(arg string) error { return m.Blind(":Co" + arg + "#") }

// SetTemperatureCal sends :Cj#, which writes a temperature global that :CT# reports.
func (m *Mount) SetTemperatureCal(arg string) error { return m.Blind(":Cj" + arg + "#") }

// GetX sends :GX#, the only silent :G command; it touches no global directly.
func (m *Mount) GetX(arg string) error { return m.Blind(":GX" + arg + "#") }

// AdjustDateTime sends :SM#, a signed two-digit adjustment to a field of the mount's date and
// time structure. It is not the Meade site-name setter. Which field it moves is not
// established.
func (m *Mount) AdjustDateTime(delta int) error { return m.Blind(fmt.Sprintf(":SM%+03d#", delta)) }

// ResetDateTimeField sends :SN#, which zeroes a field of the same structure. Also not a
// site-name setter.
func (m *Mount) ResetDateTimeField(arg string) error { return m.Blind(":SN" + arg + "#") }

// SetSS sends :SS#. Not established.
func (m *Mount) SetSS(arg string) error { return m.Blind(":SS" + arg + "#") }

// SetSm sends :Sm#, which writes a global :MD# also uses. Not established.
func (m *Mount) SetSm(arg string) error { return m.Blind(":Sm" + arg + "#") }

// SetSn sends :Sn#, as SetSm.
func (m *Mount) SetSn(arg string) error { return m.Blind(":Sn" + arg + "#") }
