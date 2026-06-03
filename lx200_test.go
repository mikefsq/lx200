package lx200

import (
	"math"
	"testing"
	"time"
)

// fakeMount is an in-memory Transport: each command written queues its scripted
// reply for the next reads. Commands with no scripted reply produce no bytes
// (exercising the Blind / timeout paths).
type fakeMount struct {
	replies map[string]string
	last    string
	writes  []string
	rbuf    []byte
}

func (f *fakeMount) Write(p []byte) (int, error) {
	f.last = string(p)
	f.writes = append(f.writes, f.last)
	if r, ok := f.replies[f.last]; ok {
		f.rbuf = append(f.rbuf, []byte(r)...)
	}
	return len(p), nil
}

func (f *fakeMount) Read(p []byte) (int, error) {
	if len(f.rbuf) == 0 {
		return 0, nil // no data yet (serial-style); the core loops until its deadline
	}
	n := copy(p, f.rbuf)
	f.rbuf = f.rbuf[n:]
	return n, nil
}

func (f *fakeMount) Close() error { return nil }

func newFake(replies map[string]string) (*Conn, *fakeMount) {
	f := &fakeMount{replies: replies}
	return New(f, 150*time.Millisecond), f
}

func TestParseSexagesimal(t *testing.T) {
	cases := []struct {
		in   string
		want float64
	}{
		{"12:34:56#", 12 + 34.0/60 + 56.0/3600},
		{"12:34.5", 12 + 34.5/60},
		{"+89*30:00", 89.5},
		{"-12*30:00#", -12.5},
		{"-00*30:00", -0.5},
		{"359*45:00", 359.75}, // azimuth, unsigned
		{"+47*18:00.0", 47.3},
	}
	for _, c := range cases {
		got, err := ParseSexagesimal(c.in)
		if err != nil {
			t.Errorf("ParseSexagesimal(%q) error: %v", c.in, err)
			continue
		}
		if math.Abs(got-c.want) > 1e-6 {
			t.Errorf("ParseSexagesimal(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestFormatRoundTrip(t *testing.T) {
	if s := FormatHMS(12 + 34.0/60 + 56.0/3600); s != "12:34:56" {
		t.Errorf("FormatHMS = %q, want 12:34:56", s)
	}
	if s := FormatHMS(-1.0); s != "23:00:00" { // wraps into [0,24)
		t.Errorf("FormatHMS(-1) = %q, want 23:00:00", s)
	}
	if s := FormatDMS(89.5, '*'); s != "+89*30:00" {
		t.Errorf("FormatDMS = %q, want +89*30:00", s)
	}
	if s := FormatDMS(-12.5, '*'); s != "-12*30:00" {
		t.Errorf("FormatDMS(-12.5) = %q, want -12*30:00", s)
	}
}

func TestPrimitives(t *testing.T) {
	c, f := newFake(map[string]string{
		":GR#":         "12:34:56#",
		":Sr12:34:56#": "1",
		":Sdbad#":      "0",
		":MS#":         "0",
		":MSbad#":      "1Below horizon#",
		":CM#":         "M31#",
	})

	if v, err := c.RA(); err != nil || math.Abs(v-(12+34.0/60+56.0/3600)) > 1e-6 {
		t.Errorf("RA() = %v, %v", v, err)
	}
	if ok, err := c.Ack(":Sr12:34:56#"); err != nil || !ok {
		t.Errorf("Ack ok = %v, %v; want true", ok, err)
	}
	if ok, err := c.Ack(":Sdbad#"); err != nil || ok {
		t.Errorf("Ack bad = %v, %v; want false", ok, err)
	}
	if err := c.Slew(":MS#"); err != nil {
		t.Errorf("Slew ok: %v", err)
	}
	if err := c.Slew(":MSbad#"); err == nil {
		t.Errorf("Slew bad: want error, got nil")
	}
	if s, err := c.SyncToTarget(); err != nil || s != "M31" {
		t.Errorf("SyncToTarget = %q, %v; want M31", s, err)
	}
	_ = f
}

func TestPulseGuideAndMoveAxis(t *testing.T) {
	c, f := newFake(map[string]string{})

	if err := c.PulseGuide(North, 500); err != nil {
		t.Errorf("PulseGuide: %v", err)
	}
	if f.last != ":Mgn0500#" {
		t.Errorf("PulseGuide wrote %q, want :Mgn0500#", f.last)
	}
	if err := c.PulseGuide(East, 99999); err == nil {
		t.Errorf("PulseGuide out-of-range: want error")
	}

	// MoveAxis(primary, +) = set rate then slew East.
	f.writes = nil
	if err := c.MoveAxis(AxisPrimary, true, RateGuide); err != nil {
		t.Errorf("MoveAxis: %v", err)
	}
	if got := f.writes; len(got) != 2 || got[0] != ":RG#" || got[1] != ":Me#" {
		t.Errorf("MoveAxis wrote %v, want [:RG# :Me#]", got)
	}

	// StopAxis(secondary) halts both N and S.
	f.writes = nil
	if err := c.StopAxis(AxisSecondary); err != nil {
		t.Errorf("StopAxis: %v", err)
	}
	if got := f.writes; len(got) != 2 || got[0] != ":Qn#" || got[1] != ":Qs#" {
		t.Errorf("StopAxis wrote %v, want [:Qn# :Qs#]", got)
	}
}

func TestBlindAndTimeout(t *testing.T) {
	c, f := newFake(map[string]string{})
	if err := c.Halt(); err != nil { // :Q# — no reply expected
		t.Errorf("Halt: %v", err)
	}
	if f.last != ":Q#" {
		t.Errorf("Halt wrote %q, want :Q#", f.last)
	}
	// A query with no scripted reply must time out, not hang.
	start := time.Now()
	if _, err := c.RA(); err != ErrTimeout {
		t.Errorf("RA with no reply: err = %v, want ErrTimeout", err)
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("timeout took %v, expected ~150ms", d)
	}
}
