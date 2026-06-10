package tenmicron

import "testing"

func TestHomeCommands(t *testing.T) {
	m, f := newMount(nil)
	if err := m.FindHome(); err != nil || f.LastWrite() != ":hF#" {
		t.Errorf("FindHome: %v wrote %q", err, f.LastWrite())
	}
	if err := m.SeekHomeAndStore(); err != nil || f.LastWrite() != ":hS#" {
		t.Errorf("SeekHomeAndStore: %v wrote %q", err, f.LastWrite())
	}
}

func TestHomeStatus(t *testing.T) {
	for _, c := range []struct {
		reply string
		want  HomeState
		at    bool
	}{
		{"0#", HomeFailed, false},
		{"1#", HomeFound, true},
		{"2#", HomeInProgress, false},
	} {
		m, _ := newMount(map[string]string{":h?#": c.reply})
		if st, err := m.HomeStatus(); err != nil || st != c.want {
			t.Errorf("HomeStatus(%q) = %v, %v; want %v", c.reply, st, err, c.want)
		}
		m2, _ := newMount(map[string]string{":h?#": c.reply})
		if at, _ := m2.AtHome(); at != c.at {
			t.Errorf("AtHome(%q) = %v, want %v", c.reply, at, c.at)
		}
	}
}
