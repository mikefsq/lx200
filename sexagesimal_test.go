package lx200

import (
	"math"
	"testing"
)

func TestParseSexagesimalSeparators(t *testing.T) {
	// The same angle expressed with every separator style the mounts use.
	cases := []string{
		"45:30:00", "45*30:00", "45°30'00\"", "45 30 00", "+45*30:00#",
	}
	for _, in := range cases {
		got, err := ParseSexagesimal(in)
		if err != nil {
			t.Errorf("ParseSexagesimal(%q) error: %v", in, err)
			continue
		}
		if math.Abs(got-45.5) > 1e-6 {
			t.Errorf("ParseSexagesimal(%q) = %v, want 45.5", in, got)
		}
	}
}

func TestParseSexagesimalErrors(t *testing.T) {
	for _, in := range []string{"", "   ", "#", "abc", "+#", "**:**"} {
		if v, err := ParseSexagesimal(in); err == nil {
			t.Errorf("ParseSexagesimal(%q) = %v, want error", in, v)
		}
	}
}

func TestFormatHMSEdges(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{0, "00:00:00"},
		{12.5, "12:30:00"},
		{24, "00:00:00"},                       // wraps
		{-1, "23:00:00"},                       // negative wraps
		{23.9999, "00:00:00"},                  // rounds up past 24h, must wrap (regression)
		{11 + 59.0/60 + 59.6/3600, "12:00:00"}, // second carries up
	}
	for _, c := range cases {
		if got := FormatHMS(c.in); got != c.want {
			t.Errorf("FormatHMS(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFormatDMSEdges(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{0, "+00*00:00"},
		{89.5, "+89*30:00"},
		{-12.5, "-12*30:00"},
		{-0.5, "-00*30:00"},
		{89 + 59.0/60 + 59.6/3600, "+90*00:00"}, // carries to 90
	}
	for _, c := range cases {
		if got := FormatDMS(c.in, '*'); got != c.want {
			t.Errorf("FormatDMS(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestSexagesimalRoundTrip checks parse(format(x)) recovers x to within the
// half-second quantization the high-precision format imposes.
func TestSexagesimalRoundTrip(t *testing.T) {
	const tol = 0.5 / 3600 // half a second, in hours or degrees
	for _, h := range []float64{0, 0.1, 6.25, 12.3456, 18.9999, 23.5} {
		got, err := ParseSexagesimal(FormatHMS(h))
		if err != nil {
			t.Fatalf("round-trip HMS %v: %v", h, err)
		}
		if d := math.Abs(got - h); d > tol {
			t.Errorf("HMS round-trip %v -> %q -> %v (off %v)", h, FormatHMS(h), got, d)
		}
	}
	for _, d := range []float64{0, -0.25, 12.5, -47.3, 89.9, -89.9} {
		got, err := ParseSexagesimal(FormatDMS(d, '*'))
		if err != nil {
			t.Fatalf("round-trip DMS %v: %v", d, err)
		}
		if diff := math.Abs(got - d); diff > tol {
			t.Errorf("DMS round-trip %v -> %q -> %v (off %v)", d, FormatDMS(d, '*'), got, diff)
		}
	}
}
