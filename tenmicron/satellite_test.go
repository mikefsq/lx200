package tenmicron

import (
	"errors"
	"testing"
)

func TestTLELoadAndPosition(t *testing.T) {
	m, _ := newMount(map[string]string{
		":TLEL0NOAA#":            "V#",
		":TLEG#":                 "NOAA 14#",
		":TLEGEQ2459580.500000#": "12.50000,+45.0000#",
		":TLEGAZ2459580.500000#": "+30.0000,180.0000#",
	})
	if err := m.LoadTLE("NOAA"); err != nil {
		t.Errorf("LoadTLE: %v", err)
	}
	if s, err := m.LoadedTLE(); err != nil || s != "NOAA 14" {
		t.Errorf("LoadedTLE = %q, %v", s, err)
	}
	if ra, dec, err := m.SatelliteEquatorial(2459580.5); err != nil || ra != 12.5 || dec != 45 {
		t.Errorf("SatelliteEquatorial = %v,%v,%v; want 12.5,45", ra, dec, err)
	}
	if alt, az, err := m.SatelliteHorizontal(2459580.5); err != nil || alt != 30 || az != 180 {
		t.Errorf("SatelliteHorizontal = %v,%v,%v; want 30,180", alt, az, err)
	}
}

func TestTransitWorkflow(t *testing.T) {
	m, _ := newMount(map[string]string{
		":TLEP2459580.500000,10#": "2459580.60,2459580.61,F#",
		":TLES#":                  "V#",
		":TLESCK#":                "T#",
	})
	tr, err := m.PrecalcTransit(2459580.5, 10)
	if err != nil || tr.JDStart != 2459580.60 || tr.JDEnd != 2459580.61 || !tr.Flip {
		t.Errorf("PrecalcTransit = %+v, %v", tr, err)
	}
	if err := m.SlewToTransit(); err != nil {
		t.Errorf("SlewToTransit: %v", err)
	}
	if st, err := m.TransitSlewStatus(); err != nil || st != TransitTracking {
		t.Errorf("TransitSlewStatus = %v, %v; want tracking", st, err)
	}

	m2, _ := newMount(map[string]string{":TLEP2459580.500000,5#": "N#"})
	if _, err := m2.PrecalcTransit(2459580.5, 5); !errors.Is(err, ErrNoSatellitePass) {
		t.Errorf("PrecalcTransit(N#) err = %v; want ErrNoSatellitePass", err)
	}
}

func TestTrajectory(t *testing.T) {
	m, _ := newMount(map[string]string{
		":TRNEW2459580.500000#":     "V#",
		":TRADD180.00000,30.00000#": "1#",
		":TRP#":                     "2459580.60,2459580.61,#",
	})
	if err := m.NewTrajectory(2459580.5); err != nil {
		t.Errorf("NewTrajectory: %v", err)
	}
	if n, err := m.AddTrajectoryPoint(180, 30); err != nil || n != 1 {
		t.Errorf("AddTrajectoryPoint = %d, %v; want 1", n, err)
	}
	tr, err := m.PrecalcTrajectory()
	if err != nil || tr.JDStart != 2459580.60 || tr.Flip {
		t.Errorf("PrecalcTrajectory = %+v, %v", tr, err)
	}
}
