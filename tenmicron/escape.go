package tenmicron

import (
	"fmt"
	"strconv"
	"strings"
)

// The 10Micron "escaped strings" scheme (protocol §"Escaped strings") is used for
// names and TLE payloads sent to / read from the mount: '$'→"$$", '#'→"$23",
// ','→"$2C", and any byte < 0x20 or > 0x7E → "$XX" (two hex digits); every other byte
// passes through. It lets a value contain the '#'/',' bytes that otherwise frame or
// separate commands.

func escapeString(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '$':
			b.WriteString("$$")
		case c == '#':
			b.WriteString("$23")
		case c == ',':
			b.WriteString("$2C")
		case c < 0x20 || c > 0x7E:
			fmt.Fprintf(&b, "$%02X", c)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

func unescapeString(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '$' {
			b.WriteByte(s[i])
			continue
		}
		if i+1 < len(s) && s[i+1] == '$' { // "$$" → '$'
			b.WriteByte('$')
			i++
			continue
		}
		if i+2 < len(s) { // "$XX" → the byte with that hex code
			if v, err := strconv.ParseUint(s[i+1:i+3], 16, 8); err == nil {
				b.WriteByte(byte(v))
				i += 2
				continue
			}
		}
		b.WriteByte('$') // malformed escape: pass the '$' through
	}
	return b.String()
}
