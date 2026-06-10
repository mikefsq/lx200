package rst

import (
	"math"
	"testing"
	"time"

	"github.com/mikefsq/lx200"
	"github.com/mikefsq/lx200/internal/lx200test"
	"github.com/mikefsq/lx200/serial"
)

func newMount(replies map[string]string) (*Mount, *lx200test.Fake) {
	f := lx200test.New(replies)
	return &Mount{Conn: lx200.New(f, 200*time.Millisecond)}, f
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
	if err := m.SlewToTarget(); err != nil || f.LastWrite() != ":MS#" {
		t.Fatalf("SlewToTarget: %v wrote %q", err, f.LastWrite())
	}
	// No token yet -> still slewing.
	if sl, _ := m.Slewing(); !sl {
		t.Errorf("Slewing = false before token, want true")
	}
	// Mount pushes the completion token; next poll drains it.
	f.Push(":MM0#")
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
	if got := f.LastWrite(); got != ":Ck307.500-52.000#" {
		t.Errorf("Sync wrote %q, want :Ck307.500-52.000#", got)
	}
}

func TestSiteFormat(t *testing.T) {
	m, f := newMount(map[string]string{
		":St+37*30*00#":  "1",
		":Sg+122*30*00#": "1",
	})
	if err := m.SetSiteLatitude(37.5); err != nil || f.LastWrite() != ":St+37*30*00#" {
		t.Errorf("SetSiteLatitude: %v wrote %q", err, f.LastWrite())
	}
	if err := m.SetSiteLongitude(-122.5); err != nil || f.LastWrite() != ":Sg+122*30*00#" {
		t.Errorf("SetSiteLongitude(-122.5) wrote %q, want :Sg+122*30*00# (East-negative)", f.LastWrite())
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
	if got := f.LastWrite(); got != ":CN307.500-52.000#" {
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
	if got := f.LastWrite(); got != ":Q#" {
		t.Errorf("PulseGuide stop wrote %q, want :Q#", got)
	}
	if m.IsPulseGuiding() {
		t.Errorf("IsPulseGuiding = true after stop, want false")
	}
}

// TestStopAxis: RST's per-axis stop collapses to the bare :Q#.
func TestStopAxis(t *testing.T) {
	m, f := newMount(nil)
	if err := m.StopAxis(lx200.AxisPrimary); err != nil || f.LastWrite() != ":Q#" {
		t.Errorf("StopAxis wrote %q, %v; want :Q#", f.LastWrite(), err)
	}
}

// TestFindPort covers the port-selection logic for both enumeration regimes: the
// exact FTDI VID/PID match (linux/windows/BSD) and the macOS name fallback, which
// must fire ONLY when no VID is reported.
func TestFindPort(t *testing.T) {
	cases := []struct {
		name  string
		ports []serial.PortInfo
		want  string
	}{
		{
			name: "exact FTDI VID/PID match",
			ports: []serial.PortInfo{
				{Name: "/dev/ttyUSB0", IsUSB: true, VID: "1234", PID: "5678"},
				{Name: "/dev/ttyUSB1", IsUSB: true, VID: "0403", PID: "6001"},
			},
			want: "/dev/ttyUSB1",
		},
		{
			name: "VID present but wrong PID is not claimed by the name fallback",
			ports: []serial.PortInfo{
				{Name: "/dev/cu.usbserial-A1", IsUSB: true, VID: "0403", PID: "6015"},
			},
			want: "",
		},
		{
			name: "macOS: no VID, FTDI VCP name matches",
			ports: []serial.PortInfo{
				{Name: "/dev/cu.usbserial-1410", IsUSB: true},
			},
			want: "/dev/cu.usbserial-1410",
		},
		{
			name: "macOS: no VID, non-FTDI name (usbmodem) does not match",
			ports: []serial.PortInfo{
				{Name: "/dev/cu.usbmodem1411", IsUSB: true},
			},
			want: "",
		},
		{
			name: "exact match wins over a name-only candidate",
			ports: []serial.PortInfo{
				{Name: "/dev/cu.usbserial-novid", IsUSB: true},
				{Name: "/dev/cu.usbserial-rst", IsUSB: true, VID: "0403", PID: "6001"},
			},
			want: "/dev/cu.usbserial-rst",
		},
		{
			name:  "empty list",
			ports: nil,
			want:  "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := findPort(c.ports); got != c.want {
				t.Errorf("findPort = %q, want %q", got, c.want)
			}
		})
	}
}

func TestTrackRatesAndPark(t *testing.T) {
	m, f := newMount(map[string]string{
		":Sz180*00'00.0\"#": "1",
		":Sa+00*00'00.0\"#": "1",
	})
	if err := m.TrackSidereal(); err != nil || f.LastWrite() != ":CtR#" {
		t.Errorf("TrackSidereal wrote %q, want :CtR#", f.LastWrite())
	}
	if err := m.Park(); err != nil || f.LastWrite() != ":MA#" {
		t.Errorf("Park: %v final write %q, want :MA#", err, f.LastWrite())
	}
	if p, _ := m.AtPark(); !p {
		t.Errorf("AtPark = false after Park")
	}
}
