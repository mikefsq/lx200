package tenmicron

import (
	"errors"
	"math"
	"testing"

	"github.com/mikefsq/lx200"
)

func TestAlignmentBuildSequence(t *testing.T) {
	pointCmd := ":newalpt20:30:00.0,-52:00:00,E,20:30:00.0,-52:00:00,10:00:00.0#"
	m, f := newMount(map[string]string{
		":newalig#": "V#",
		pointCmd:    "1#",
		":endalig#": "V#",
	})
	if err := m.StartAlignment(); err != nil {
		t.Fatalf("StartAlignment: %v", err)
	}
	n, err := m.AddAlignmentSpecPoint(AlignmentPoint{
		MountRA: 20.5, MountDec: -52, MountSide: lx200.PierEast,
		SolvedRA: 20.5, SolvedDec: -52, SiderealTime: 10,
	})
	if err != nil || n != 1 {
		t.Fatalf("AddAlignmentSpecPoint = %d, %v; want 1 (wrote %q)", n, err, f.LastWrite())
	}
	if f.Writes()[1] != pointCmd {
		t.Errorf("point cmd = %q, want %q", f.Writes()[1], pointCmd)
	}
	if err := m.EndAlignment(); err != nil {
		t.Errorf("EndAlignment: %v", err)
	}
}

func TestDeleteAlignmentAndCount(t *testing.T) {
	m, f := newMount(map[string]string{":delalig#": "#", ":getalst#": "3#"})
	if err := m.DeleteAlignment(); err != nil || f.LastWrite() != ":delalig#" {
		t.Errorf("DeleteAlignment: %v wrote %q", err, f.LastWrite())
	}
	if n, err := m.AlignmentStarCount(); err != nil || n != 3 {
		t.Errorf("AlignmentStarCount = %d, %v; want 3", n, err)
	}
}

func TestAlignmentInfo(t *testing.T) {
	m, _ := newMount(map[string]string{
		":getain#": "120.5000,+45.0000,0.0100,90.00,+0.0050,+1.20,-0.50,8,12.5#",
	})
	info, err := m.AlignmentInfo()
	if err != nil {
		t.Fatalf("AlignmentInfo: %v", err)
	}
	if info.Azimuth != 120.5 || info.Altitude != 45 || info.PolarError != 0.01 ||
		info.Terms != 8 || info.RMSArcsec != 12.5 {
		t.Errorf("AlignmentInfo = %+v", info)
	}

	// omitted fields ("E") -> NaN, and <2 stars -> ErrModelTooFewStars
	m2, _ := newMount(map[string]string{
		":getain#": "120.5,+45.0,0.01,90.0,E,E,E,E,E#",
	})
	info2, _ := m2.AlignmentInfo()
	if !math.IsNaN(info2.OrthoError) || info2.Terms != -1 || !math.IsNaN(info2.RMSArcsec) {
		t.Errorf("expected NaN/-1 for omitted fields, got %+v", info2)
	}
	m3, _ := newMount(map[string]string{":getain#": "E#"})
	if _, err := m3.AlignmentInfo(); !errors.Is(err, ErrModelTooFewStars) {
		t.Errorf("AlignmentInfo(E#) err = %v; want ErrModelTooFewStars", err)
	}
}
