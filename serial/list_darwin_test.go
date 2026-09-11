//go:build darwin

package serial

import "testing"

// On macOS the device node carries the USB serial, so a caller can pin a unit without IOKit.
func TestSerialFromNodeName(t *testing.T) {
	cases := map[string]string{
		"/dev/cu.usbserial-A10KLC4K":  "A10KLC4K",
		"/dev/tty.usbserial-A10KLC4K": "A10KLC4K",
		"/dev/cu.usbmodem14201":       "14201",
		"/dev/cu.Bluetooth-Incoming":  "",
		"/dev/ttyUSB0":                "",
	}
	for name, want := range cases {
		if got := serialFromName(name); got != want {
			t.Errorf("serialFromName(%q) = %q, want %q", name, got, want)
		}
	}
}
