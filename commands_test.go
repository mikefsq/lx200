package lx200

import (
	"math"
	"testing"
)

// TestMoveAxisDirections verifies axis+sign maps to the correct cardinal slew
// (a swapped E/W or N/S here would be a real pointing bug).
func TestMoveAxisDirections(t *testing.T) {
	c, f := newFake(map[string]string{})
	cases := []struct {
		a   Axis
		pos bool
		dir string
	}{
		{AxisPrimary, true, ":Me#"},    // primary + = East
		{AxisPrimary, false, ":Mw#"},   // primary - = West
		{AxisSecondary, true, ":Mn#"},  // secondary + = North
		{AxisSecondary, false, ":Ms#"}, // secondary - = South
	}
	for _, tc := range cases {
		f.writes = nil
		if err := c.MoveAxis(tc.a, tc.pos, RateGuide); err != nil {
			t.Errorf("MoveAxis(%v,%v): %v", tc.a, tc.pos, err)
		}
		if got := lastWrite(f); got != tc.dir {
			t.Errorf("MoveAxis(axis=%d,pos=%v) dir = %q, want %q", tc.a, tc.pos, got, tc.dir)
		}
	}
}

func lastWrite(f *fakeMount) string {
	if len(f.writes) == 0 {
		return ""
	}
	return f.writes[len(f.writes)-1]
}

// TestCommandWireFormat verifies every command method emits the exact LX200
// byte string and parses its reply correctly.
func TestCommandWireFormat(t *testing.T) {
	c, f := newFake(map[string]string{
		":GR#":          "12:00:00#",
		":GD#":          "+45*30:00#",
		":GA#":          "+30*00:00#",
		":GZ#":          "180*00:00#",
		":GS#":          "06:00:00#",
		":GVN#":         "2.16#",
		":Sr12:30:00#":  "1",
		":Sd+45*30:00#": "1",
		":MS#":          "0",
		":CM#":          "M42#",
	})

	// Getters: check (command emitted, value parsed).
	getters := []struct {
		name string
		fn   func() (float64, error)
		cmd  string
		want float64
	}{
		{"RA", c.RA, ":GR#", 12},
		{"Dec", c.Dec, ":GD#", 45.5},
		{"Altitude", c.Altitude, ":GA#", 30},
		{"Azimuth", c.Azimuth, ":GZ#", 180},
		{"SiderealTime", c.SiderealTime, ":GS#", 6},
	}
	for _, g := range getters {
		v, err := g.fn()
		if err != nil {
			t.Errorf("%s: %v", g.name, err)
		}
		if lastWrite(f) != g.cmd {
			t.Errorf("%s wrote %q, want %q", g.name, lastWrite(f), g.cmd)
		}
		if math.Abs(v-g.want) > 1e-6 {
			t.Errorf("%s = %v, want %v", g.name, v, g.want)
		}
	}

	if s, err := c.Firmware(); err != nil || s != "2.16" || lastWrite(f) != ":GVN#" {
		t.Errorf("Firmware = %q, %v (wrote %q)", s, err, lastWrite(f))
	}

	// Set target: exact formatting is the contract.
	if ok, err := c.SetTargetRA(12.5); err != nil || !ok || lastWrite(f) != ":Sr12:30:00#" {
		t.Errorf("SetTargetRA: ok=%v err=%v wrote %q", ok, err, lastWrite(f))
	}
	if ok, err := c.SetTargetDec(45.5); err != nil || !ok || lastWrite(f) != ":Sd+45*30:00#" {
		t.Errorf("SetTargetDec: ok=%v err=%v wrote %q", ok, err, lastWrite(f))
	}

	// Actions.
	if err := c.SlewToTarget(); err != nil || lastWrite(f) != ":MS#" {
		t.Errorf("SlewToTarget: err=%v wrote %q", err, lastWrite(f))
	}
	if s, err := c.SyncToTarget(); err != nil || s != "M42" || lastWrite(f) != ":CM#" {
		t.Errorf("SyncToTarget: %q %v wrote %q", s, err, lastWrite(f))
	}

	// Blind commands: just the exact bytes.
	blinds := []struct {
		name string
		fn   func() error
		cmd  string
	}{
		{"Halt", c.Halt, ":Q#"},
		{"Move(N)", func() error { return c.Move(North) }, ":Mn#"},
		{"Move(S)", func() error { return c.Move(South) }, ":Ms#"},
		{"Move(E)", func() error { return c.Move(East) }, ":Me#"},
		{"Move(W)", func() error { return c.Move(West) }, ":Mw#"},
		{"HaltMove(N)", func() error { return c.HaltMove(North) }, ":Qn#"},
		{"SetRate(Guide)", func() error { return c.SetRate(RateGuide) }, ":RG#"},
		{"SetRate(Center)", func() error { return c.SetRate(RateCenter) }, ":RC#"},
		{"SetRate(Find)", func() error { return c.SetRate(RateFind) }, ":RM#"},
		{"SetRate(Max)", func() error { return c.SetRate(RateMax) }, ":RS#"},
		{"TrackSidereal", c.TrackSidereal, ":TQ#"},
		{"TrackLunar", c.TrackLunar, ":TL#"},
		{"TrackSolar", c.TrackSolar, ":TS#"},
		{"PulseGuide(E)", func() error { return c.PulseGuide(East, 250) }, ":Mge0250#"},
	}
	for _, b := range blinds {
		f.writes = nil
		if err := b.fn(); err != nil {
			t.Errorf("%s: %v", b.name, err)
		}
		if lastWrite(f) != b.cmd {
			t.Errorf("%s wrote %q, want %q", b.name, lastWrite(f), b.cmd)
		}
	}
}
