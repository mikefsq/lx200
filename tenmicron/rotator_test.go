package tenmicron

import "testing"

func TestRotatorQueries(t *testing.T) {
	m, _ := newMount(map[string]string{
		":RotQ1#":   "1#",
		":RotI1#":   "MyRot,field,SN9#",
		":RotGR1#":  "+045.5000#",
		":RotGr1#":  "+090.0000#",
		":RotD1#":   "1#",
		":Rotd1#":   "3#",
		":RotGms1#": "010#",
		":RotGof1#": "+010.0000#",
	})
	r := m.Rotator(1)
	if ok, err := r.Available(); err != nil || !ok {
		t.Errorf("Available = %v, %v; want true", ok, err)
	}
	if fi, err := r.Info(); err != nil || fi.Name != "MyRot" || fi.Serial != "SN9" {
		t.Errorf("Info = %+v, %v", fi, err)
	}
	if a, err := r.Angle(RotatorEquatorial); err != nil || a != 45.5 {
		t.Errorf("Angle(equ) = %v, %v; want 45.5", a, err)
	}
	if d, err := r.Destination(RotatorEquatorial); err != nil || d != 90 {
		t.Errorf("Destination(equ) = %v, %v; want 90", d, err)
	}
	if mv, err := r.Moving(); err != nil || !mv {
		t.Errorf("Moving = %v, %v; want true", mv, err)
	}
	if st, err := r.Status(); err != nil || st != 3 {
		t.Errorf("Status = %v, %v; want 3", st, err)
	}
	if sp, err := r.MaxSpeed(); err != nil || sp != 10 {
		t.Errorf("MaxSpeed = %v, %v; want 10", sp, err)
	}
	if o, err := r.Offset(); err != nil || o != 10 {
		t.Errorf("Offset = %v, %v; want 10", o, err)
	}
}

func TestRotatorCommands(t *testing.T) {
	m, f := newMount(map[string]string{
		":RotSr1,+090.0000#": "1",
		":RotSmZ1#":          "V#",
		":RotHS1#":           "1",
		":RotHG1#":           "2#",
	})
	r := m.Rotator(1)
	if ok, err := r.SetDestination(RotatorEquatorial, 90); err != nil || !ok || f.LastWrite() != ":RotSr1,+090.0000#" {
		t.Errorf("SetDestination: ok=%v err=%v wrote %q", ok, err, f.LastWrite())
	}
	if ok, err := r.ZeroMechanical(); err != nil || !ok || f.LastWrite() != ":RotSmZ1#" {
		t.Errorf("ZeroMechanical: ok=%v err=%v wrote %q", ok, err, f.LastWrite())
	}
	if ok, err := r.StartHoming(); err != nil || !ok {
		t.Errorf("StartHoming = %v, %v; want true", ok, err)
	}
	if hs, err := r.HomingStatus(); err != nil || hs != FocuserHomingCompleted {
		t.Errorf("HomingStatus = %v, %v; want completed", hs, err)
	}
	if err := r.Stop(); err != nil || f.LastWrite() != ":RotSq1#" {
		t.Errorf("Stop: %v wrote %q", err, f.LastWrite())
	}
}
