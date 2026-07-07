package tenmicron

import (
	"fmt"
	"math"
	"strings"
)

// Coordinate formatting for the 10Micron ultra-precision (:U2#) mode that Connect
// forces. In that mode the mount accepts — and the vendor ASCOM driver emits —
// fractional-second target coordinates:
//
//	:Sr HH:MM:SS.ss   (RA,  centisecond)
//	:Sd sDD*MM:SS.s   (Dec, decisecond)
//	:Sa sDD*MM:SS.s   (Alt, decisecond)
//	:Sz DDD*MM:SS.s   (Az,  decisecond)
//
// The shared lx200 core's FormatHMS/FormatDMS emit whole seconds, which would
// quantise a target to ~15" in RA and ~1" in Dec/Alt — discarding exactly the
// precision :U2# exists to provide. So the 10Micron target setters use the encoders
// below instead. (Reads are unaffected: they come from :Ginfo# decimal fields or go
// through the lenient ParseSexagesimal.)

// hmsPrec formats hours as "HH:MM:SS" plus `decimals` fractional-second digits,
// wrapping into [0,24). It matches the vendor's :Sr encoding at decimals=2.
func hmsPrec(hours float64, decimals int) string {
	hours = math.Mod(hours, 24)
	if hours < 0 {
		hours += 24
	}
	return encodeSex(hours, "::", false, 2, decimals)
}

// dmsPrec formats signed degrees as "sDD*MM:SS" plus `decimals` fractional-second
// digits, with a fixed degree-field width (2 for Dec/Alt, 3 for Az). forceSign adds
// a leading '+' on non-negative values (Dec/Alt); azimuth passes forceSign=false.
func dmsPrec(deg float64, degWidth, decimals int, forceSign bool) string {
	return encodeSex(deg, "*:", forceSign, degWidth, decimals)
}

// encodeSex renders a decimal value (hours or degrees) as a sexagesimal string with
// the given two field separators, leading-field width, and fractional-second digits.
// It rounds half-up at the finest emitted unit, mirroring the vendor's EncodeLXString
// (which biases by half an ULP before truncating each field), so a value that lands
// just shy of a second boundary carries correctly instead of truncating down.
func encodeSex(value float64, seps string, forceSign bool, firstWidth, decimals int) string {
	sign := ""
	if value < 0 {
		sign, value = "-", -value
	} else if forceSign {
		sign = "+"
	}
	// Half-up bias at the emitted resolution: 0.5 unit in the last fractional digit,
	// expressed in the top unit (÷60 per separator, ÷10^decimals for the fraction).
	value += 0.5 / (math.Pow(10, float64(decimals)) * math.Pow(60, float64(len(seps))))

	var b strings.Builder
	b.WriteString(sign)
	for i := 0; i <= len(seps); i++ {
		n := int(math.Floor(value))
		value -= float64(n)
		w := 2
		if i == 0 {
			w = firstWidth
		}
		fmt.Fprintf(&b, "%0*d", w, n)
		if i < len(seps) {
			b.WriteByte(seps[i])
			value *= 60
		}
	}
	if decimals > 0 {
		frac := int(math.Floor(value * math.Pow(10, float64(decimals))))
		fmt.Fprintf(&b, ".%0*d", decimals, frac)
	}
	return b.String()
}

// SetTargetRA sets the target right ascension in hours (:Sr HH:MM:SS.ss#). It
// overrides the lx200 core's whole-second SetTargetRA so the ultra-precision mode set
// at Connect isn't wasted (see coords.go). Reports whether the mount accepted it.
func (m *Mount) SetTargetRA(hours float64) (bool, error) {
	return m.Ack(":Sr" + hmsPrec(hours, 2) + "#")
}

// SetTargetDec sets the target declination in degrees (:Sd sDD*MM:SS.s#), overriding
// the core's whole-second form for the same reason as SetTargetRA.
func (m *Mount) SetTargetDec(deg float64) (bool, error) {
	return m.Ack(":Sd" + dmsPrec(deg, 2, 1, true) + "#")
}
