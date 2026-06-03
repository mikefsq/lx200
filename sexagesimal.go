package lx200

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// ParseSexagesimal parses an LX200 angle/time reply into a decimal value in the
// top unit (hours for RA, degrees for Dec/Alt/Az). It is deliberately lenient:
// it accepts any field separators (':', '*', '°', '\”, '"', spaces), an
// optional leading sign, a trailing '#', and 2 or 3 fields with an optional
// decimal in the last (so "HH:MM.M", "HH:MM:SS", "sDD*MM:SS.S" all parse).
func ParseSexagesimal(s string) (float64, error) {
	s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "#"))
	if s == "" {
		return 0, fmt.Errorf("lx200: empty sexagesimal value")
	}
	sign := 1.0
	switch s[0] {
	case '-':
		sign, s = -1.0, s[1:]
	case '+':
		s = s[1:]
	}
	// Split on any run of characters that is neither a digit nor a decimal point.
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return !(r >= '0' && r <= '9' || r == '.')
	})
	if len(fields) == 0 {
		return 0, fmt.Errorf("lx200: no numeric fields in %q", s)
	}
	var val, scale float64 = 0, 1
	for i, f := range fields {
		if i > 3 { // top unit, minutes, seconds — ignore anything beyond
			break
		}
		n, err := strconv.ParseFloat(f, 64)
		if err != nil {
			return 0, fmt.Errorf("lx200: bad sexagesimal field %q in %q: %w", f, s, err)
		}
		val += n / scale
		scale *= 60
	}
	return sign * val, nil
}

// FormatHMS formats hours as "HH:MM:SS" (high precision), wrapping into [0,24).
// This is the format LX200 set-RA commands expect (e.g. :Sr HH:MM:SS#). It
// rounds on the integer-seconds value and wraps the carry, so 23.9999 h yields
// "00:00:00" rather than an invalid "24:00:00".
func FormatHMS(hours float64) string {
	sec := int(math.Round(math.Mod(hours, 24) * 3600))
	sec = ((sec % 86400) + 86400) % 86400 // normalize into [0, 86400)
	return fmt.Sprintf("%02d:%02d:%02d", sec/3600, (sec/60)%60, sec%60)
}

// FormatDMS formats degrees as "sDD*MM:SS" (sign, then the given degree
// separator), the format LX200 set-Dec/Alt/Az commands expect. Use '*' for the
// classic Meade form (:Sd sDD*MM:SS#).
func FormatDMS(deg float64, sep byte) string {
	sign := byte('+')
	if deg < 0 {
		sign, deg = '-', -deg
	}
	d, m, s := splitSex(deg)
	return fmt.Sprintf("%c%02d%c%02d:%02d", sign, d, sep, m, s)
}

// splitSex breaks a non-negative decimal value into whole top-unit, minutes, and
// seconds, rounding to the nearest second and carrying overflow (60s→+1m, 60m→+1).
func splitSex(v float64) (top, min, sec int) {
	total := int(math.Round(v * 3600)) // total seconds
	sec = total % 60
	total /= 60
	min = total % 60
	top = total / 60
	return
}
