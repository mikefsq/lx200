package rst

import (
	"math"
	"sync"
	"testing"
	"time"

	"github.com/mikefsq/lx200"
)

type fake struct {
	mu      sync.Mutex // PulseGuide's stop goroutine writes concurrently
	replies map[string]string
	writes  []string
	rbuf    []byte
}

func (f *fake) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writes = append(f.writes, string(p))
	if r, ok := f.replies[string(p)]; ok {
		f.rbuf = append(f.rbuf, []byte(r)...)
	}
	return len(p), nil
}
func (f *fake) Read(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.rbuf) == 0 {
		return 0, nil
	}
	n := copy(p, f.rbuf)
	f.rbuf = f.rbuf[n:]
	return n, nil
}
func (f *fake) Close() error { return nil }

func newMount(replies map[string]string) (*Mount, *fake) {
	f := &fake{replies: replies}
	return &Mount{Conn: lx200.New(f, 200*time.Millisecond)}, f
}

func last(f *fake) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.writes) == 0 {
		return ""
	}
	return f.writes[len(f.writes)-1]
}

// TestCoordPrefixAndSign is the key regression: RST replies echo the command
// prefix (:GD-52...#), so the sign is mid-string — must still parse negative.
func TestCoordPrefixAndSign(t *testing.T) {
	m, _ := newMount(map[string]string{
		":GR#": ":GR20:28:56.9#",
		":GD#": ":GD-52*13'21.9#",
	})
	if ra, err := m.RA(); err != nil || math.Abs(ra-(20+28.0/60+56.9/3600)) > 1e-6 {
		t.Errorf("RA = %v, %v", ra, err)
	}
	dec, err := m.Dec()
	want := -(52 + 13.0/60 + 21.9/3600)
	if err != nil || math.Abs(dec-want) > 1e-6 {
		t.Errorf("Dec = %v, %v; want %v (negative!)", dec, err, want)
	}
}

func TestTracking(t *testing.T) {
	m, _ := newMount(map[string]string{":AT#": ":AT1#"})
	if tr, err := m.Tracking(); err != nil || !tr {
		t.Errorf("Tracking = %v, %v; want true (:AT1#)", tr, err)
	}
	m2, _ := newMount(map[string]string{":AT#": ":AT0#"})
	if tr, _ := m2.Tracking(); tr {
		t.Errorf("Tracking = true, want false (:AT0#)")
	}
}

// TestSlewingViaToken exercises the unsolicited completion-token model.
func TestSlewingViaToken(t *testing.T) {
	m, f := newMount(map[string]string{}) // :MS# has no immediate reply
	if err := m.SlewToTarget(); err != nil || last(f) != ":MS#" {
		t.Fatalf("SlewToTarget: %v wrote %q", err, last(f))
	}
	// No token yet -> still slewing.
	if sl, _ := m.Slewing(); !sl {
		t.Errorf("Slewing = false before token, want true")
	}
	// Mount pushes the completion token; next poll drains it.
	f.rbuf = append(f.rbuf, []byte(":MM0#")...)
	time.Sleep(peekTTL) // allow the next peek (coalescing window)
	if sl, _ := m.Slewing(); sl {
		t.Errorf("Slewing = true after :MM0# token, want false")
	}
}

func TestSyncBuildsCk(t *testing.T) {
	m, f := newMount(map[string]string{
		":Sr20:30:00.0#":  "1",
		":Sd-52*00:00.0#": "1",
	})
	m.SetTargetRA(20.5)
	m.SetTargetDec(-52.0)
	if _, err := m.SyncToTarget(); err != nil {
		t.Fatalf("SyncToTarget: %v", err)
	}
	if got := last(f); got != ":Ck307.500-52.000#" {
		t.Errorf("Sync wrote %q, want :Ck307.500-52.000#", got)
	}
}

func TestSiteFormat(t *testing.T) {
	m, f := newMount(map[string]string{
		":St+37*30*00#":  "1",
		":Sg+122*30*00#": "1",
	})
	if err := m.SetSiteLatitude(37.5); err != nil || last(f) != ":St+37*30*00#" {
		t.Errorf("SetSiteLatitude: %v wrote %q", err, last(f))
	}
	if err := m.SetSiteLongitude(-122.5); err != nil || last(f) != ":Sg+122*30*00#" {
		t.Errorf("SetSiteLongitude(-122.5) wrote %q, want :Sg+122*30*00# (East-negative)", last(f))
	}
}

// TestAddAlignmentPoint: the :CN save-alignment sync variant, same coord build.
func TestAddAlignmentPoint(t *testing.T) {
	m, f := newMount(map[string]string{
		":Sr20:30:00.0#":  "1",
		":Sd-52*00:00.0#": "1",
	})
	m.SetTargetRA(20.5)
	m.SetTargetDec(-52.0)
	if err := m.AddAlignmentPoint(); err != nil {
		t.Fatalf("AddAlignmentPoint: %v", err)
	}
	if got := last(f); got != ":CN307.500-52.000#" {
		t.Errorf("AddAlignmentPoint wrote %q, want :CN307.500-52.000#", got)
	}
}

// TestTrackMode: :Ct?# readback mapping (capture-confirmed 0/1/2).
func TestTrackMode(t *testing.T) {
	for _, c := range []struct {
		reply string
		want  TrackMode
	}{
		{":CT0#", TrackModeSidereal},
		{":CT1#", TrackModeSolar},
		{":CT2#", TrackModeCustom},
	} {
		m, _ := newMount(map[string]string{":Ct?#": c.reply})
		if got, err := m.TrackMode(); err != nil || got != c.want {
			t.Errorf("TrackMode(%s) = %v, %v; want %v", c.reply, got, err, c.want)
		}
	}
}

func TestPierSide(t *testing.T) {
	// decAxis - alignOff = 232.22 - 0 = 232.22 > 90 -> West.
	mW, _ := newMount(map[string]string{
		":CG3#": ":CG3+000.0000#",
		":CY#":  ":CY+232.22/-090.06#",
	})
	if ps, err := mW.PierSide(); err != nil || ps != lx200.PierWest {
		t.Errorf("PierSide = %v, %v; want West", ps, err)
	}
	// decAxis - alignOff = 45.0 - 0 = 45 <= 90 -> East.
	mE, _ := newMount(map[string]string{
		":CG3#": ":CG3+000.0000#",
		":CY#":  ":CY+045.00/-010.00#",
	})
	if ps, err := mE.PierSide(); err != nil || ps != lx200.PierEast {
		t.Errorf("PierSide = %v, %v; want East", ps, err)
	}
}

// TestPulseGuideStop: the async stop must be the bare :Q# (RST has no :Q{dir}#).
func TestPulseGuideStop(t *testing.T) {
	m, f := newMount(nil)
	if err := m.PulseGuide(lx200.North, 20); err != nil {
		t.Fatalf("PulseGuide: %v", err)
	}
	time.Sleep(60 * time.Millisecond) // let the stop goroutine fire
	if got := last(f); got != ":Q#" {
		t.Errorf("PulseGuide stop wrote %q, want :Q#", got)
	}
	if m.IsPulseGuiding() {
		t.Errorf("IsPulseGuiding = true after stop, want false")
	}
}

// TestStopAxis: RST's per-axis stop collapses to the bare :Q#.
func TestStopAxis(t *testing.T) {
	m, f := newMount(nil)
	if err := m.StopAxis(lx200.AxisPrimary); err != nil || last(f) != ":Q#" {
		t.Errorf("StopAxis wrote %q, %v; want :Q#", last(f), err)
	}
}

func TestTrackRatesAndPark(t *testing.T) {
	m, f := newMount(map[string]string{
		":Sz180*00'00.0\"#": "1",
		":Sa+00*00'00.0\"#": "1",
	})
	if err := m.TrackSidereal(); err != nil || last(f) != ":CtR#" {
		t.Errorf("TrackSidereal wrote %q, want :CtR#", last(f))
	}
	if err := m.Park(); err != nil || last(f) != ":MA#" {
		t.Errorf("Park: %v final write %q, want :MA#", err, last(f))
	}
	if p, _ := m.AtPark(); !p {
		t.Errorf("AtPark = false after Park")
	}
}
