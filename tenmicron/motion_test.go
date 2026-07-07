package tenmicron

import (
	"testing"

	"github.com/mikefsq/lx200"
)

func TestSlewVariants(t *testing.T) {
	cases := []struct {
		call func(*Mount) error
		want string
	}{
		{func(m *Mount) error { return m.SlewToTargetOnSide(lx200.PierWest) }, ":MSfs2#"},
		{func(m *Mount) error { return m.SlewToTargetOnSide(lx200.PierEast) }, ":MSfs3#"},
		{(*Mount).SlewToTargetNoFineLimit, ":MSnf#"},
		{func(m *Mount) error { return m.Nudge(100, -50) }, ":NUDGE+0100,-0050#"},
		{(*Mount).SlewPolarAlign, ":MSap#"},
		{(*Mount).SlewOrthoAlign, ":MSao#"},
	}
	for _, c := range cases {
		m, f := newMount(map[string]string{c.want: "0"}) // slew started
		if err := c.call(m); err != nil {
			t.Errorf("%s: %v", c.want, err)
		}
		if f.LastWrite() != c.want {
			t.Errorf("wrote %q, want %q", f.LastWrite(), c.want)
		}
	}
}

func TestBlindMotionExtras(t *testing.T) {
	cases := []struct {
		call func(*Mount) error
		want string
	}{
		{(*Mount).SwapEastWest, ":EW#"},
		{(*Mount).SwapNorthSouth, ":NS#"},
		{(*Mount).Stop, ":STOP#"},
		{(*Mount).NoOp, ":P#"},
	}
	for _, c := range cases {
		m, f := newMount(nil)
		if err := c.call(m); err != nil {
			t.Errorf("%s: %v", c.want, err)
		}
		if f.LastWrite() != c.want {
			t.Errorf("wrote %q, want %q", f.LastWrite(), c.want)
		}
	}
}

func TestAddAlignmentPoint(t *testing.T) {
	m, _ := newMount(map[string]string{":CMS#": "V#"})
	if err := m.AddAlignmentPoint(); err != nil {
		t.Errorf("AddAlignmentPoint(V#): %v", err)
	}
	m2, _ := newMount(map[string]string{":CMS#": "E#"})
	if err := m2.AddAlignmentPoint(); err == nil {
		t.Errorf("AddAlignmentPoint(E#): want error")
	}
}

func TestSlewInProgress(t *testing.T) {
	m, _ := newMount(map[string]string{":D#": "\x7f#"})
	if sl, err := m.SlewInProgress(); err != nil || !sl {
		t.Errorf("SlewInProgress(slewing) = %v, %v; want true", sl, err)
	}
	m2, _ := newMount(map[string]string{":D#": "#"})
	if sl, _ := m2.SlewInProgress(); sl {
		t.Errorf("SlewInProgress(done) = true, want false")
	}
}

func TestPointingState(t *testing.T) {
	m, _ := newMount(map[string]string{":pS#": "East#"})
	if ps, err := m.PointingState(); err != nil || ps != lx200.PierEast {
		t.Errorf("PointingState = %v, %v; want East", ps, err)
	}
	m2, _ := newMount(map[string]string{":pS#": "West#"})
	if ps, _ := m2.PointingState(); ps != lx200.PierWest {
		t.Errorf("PointingState = %v, want West", ps)
	}
}

func TestSyncToTargetR(t *testing.T) {
	m, f := newMount(map[string]string{":CMR#": "M31#"})
	if s, err := m.SyncToTargetR(); err != nil || s != "M31" || f.LastWrite() != ":CMR#" {
		t.Errorf("SyncToTargetR = %q, %v wrote %q", s, err, f.LastWrite())
	}
}
