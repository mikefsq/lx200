package tenmicron

import (
	"math"
	"strings"
	"testing"
	"time"
)

func TestEscapeString(t *testing.T) {
	cases := []struct{ in, out string }{
		{"Home", "Home"},
		{"a,b", "a$2Cb"},
		{"x#y", "x$23y"},
		{"$", "$$"},
		{"tab\tend", "tab$09end"},
	}
	for _, c := range cases {
		if got := escapeString(c.in); got != c.out {
			t.Errorf("escapeString(%q) = %q, want %q", c.in, got, c.out)
		}
		if got := unescapeString(c.out); got != c.in {
			t.Errorf("unescapeString(%q) = %q, want %q", c.out, got, c.in)
		}
	}
}

func TestModelSlots(t *testing.T) {
	m, f := newMount(map[string]string{
		":modelcnt#":       "3#",
		":modelnam2#":      "My Model  #", // trailing spaces ignored
		":modelld0Winter#": "1#",
		":modelsv0Winter#": "1#",
		":modeldel0Bad#":   "0#",
	})
	if n, err := m.SavedModelCount(); err != nil || n != 3 {
		t.Errorf("SavedModelCount = %v, %v", n, err)
	}
	if name, err := m.SavedModelName(2); err != nil || name != "My Model" {
		t.Errorf("SavedModelName = %q, %v; want \"My Model\"", name, err)
	}
	if err := m.LoadModel("Winter"); err != nil || f.LastWrite() != ":modelld0Winter#" {
		t.Errorf("LoadModel: %v wrote %q", err, f.LastWrite())
	}
	if err := m.SaveModel("Winter"); err != nil {
		t.Errorf("SaveModel: %v", err)
	}
	if err := m.DeleteModel("Bad"); err == nil { // reply "0#" = failure
		t.Errorf("DeleteModel(0#): want error")
	}
}

func TestAlignmentStarDetail(t *testing.T) {
	m, f := newMount(map[string]string{
		":getalp1#":  "12:30:00.00,+45*30:00.0,0012.3,090#",
		":delalst2#": "1#",
	})
	info, err := m.AlignmentPointInfo(1)
	if err != nil {
		t.Fatalf("AlignmentPointInfo: %v", err)
	}
	if math.Abs(info.HourAngle-12.5) > 1e-6 || math.Abs(info.Dec-45.5) > 1e-6 ||
		math.Abs(info.ErrorArcsec-12.3) > 1e-6 || info.PolarAngle != 90 {
		t.Errorf("AlignmentPointInfo = %+v", info)
	}
	if err := m.DeleteAlignmentStar(2); err != nil || f.LastWrite() != ":delalst2#" {
		t.Errorf("DeleteAlignmentStar: %v wrote %q", err, f.LastWrite())
	}
	m2, _ := newMount(map[string]string{":getalstm#": "100#"})
	if n, err := m2.MaxAlignmentStars(); err != nil || n != 100 {
		t.Errorf("MaxAlignmentStars = %v, %v; want 100", n, err)
	}
}

func TestTLEDatabase(t *testing.T) {
	m, _ := newMount(map[string]string{
		":TLEDN#":  "5#",
		":TLEDL2#": "ISS-DATA#",
		":TLEDL9#": "E#",
	})
	if n, err := m.DatabaseTLECount(); err != nil || n != 5 {
		t.Errorf("DatabaseTLECount = %v, %v", n, err)
	}
	if s, err := m.LoadDatabaseTLE(2); err != nil || s != "ISS-DATA" {
		t.Errorf("LoadDatabaseTLE(2) = %q, %v", s, err)
	}
	if _, err := m.LoadDatabaseTLE(9); err == nil {
		t.Errorf("LoadDatabaseTLE(9): want error")
	}
}

func TestTrajectoryOffsets(t *testing.T) {
	m, f := newMount(map[string]string{
		":TRREPLAY#":          "2459580.60,2459580.61,F#",
		":TROFFADD1,+0005.0#": "V#",
		":TROFFSET2,-0300.5#": "V#",
		":TROFFGET1#":         "+0005.0#",
		":TROFFCLR#":          "V#",
	})
	if tr, err := m.ReplayTrajectory(); err != nil || !tr.Flip || tr.JDStart != 2459580.60 {
		t.Errorf("ReplayTrajectory = %+v, %v", tr, err)
	}
	if err := m.AddTrajectoryOffset(OffsetAxis1, 5.0); err != nil || f.LastWrite() != ":TROFFADD1,+0005.0#" {
		t.Errorf("AddTrajectoryOffset: %v wrote %q", err, f.LastWrite())
	}
	if err := m.SetTrajectoryOffset(OffsetAxis2, -300.5); err != nil || f.LastWrite() != ":TROFFSET2,-0300.5#" {
		t.Errorf("SetTrajectoryOffset: %v wrote %q", err, f.LastWrite())
	}
	if v, err := m.TrajectoryOffsetValue(OffsetAxis1); err != nil || math.Abs(v-5.0) > 1e-6 {
		t.Errorf("TrajectoryOffsetValue = %v, %v", v, err)
	}
	if err := m.ClearTrajectoryOffsets(); err != nil {
		t.Errorf("ClearTrajectoryOffsets: %v", err)
	}
	// Not following a trajectory: Add/Set reply "E#" — the error must say "not
	// following" (benign), not "rejected", mirroring TrajectoryOffsetValue.
	m2, _ := newMount(map[string]string{":TROFFADD1,+0005.0#": "E#", ":TROFFSET1,+0005.0#": "E#"})
	if err := m2.AddTrajectoryOffset(OffsetAxis1, 5.0); err == nil || !strings.Contains(err.Error(), "not following") {
		t.Errorf("AddTrajectoryOffset(E#) err = %v; want 'not following'", err)
	}
	if err := m2.SetTrajectoryOffset(OffsetAxis1, 5.0); err == nil || !strings.Contains(err.Error(), "not following") {
		t.Errorf("SetTrajectoryOffset(E#) err = %v; want 'not following'", err)
	}
}

func TestClockDetail(t *testing.T) {
	m, f := newMount(map[string]string{
		":SLDT2026-06-02,15:04:05#": "1",
		":GULEAP#":                  "2026-12-31#",
		":GDUTV#":                   "V,2027-01-01#",
	})
	tm := time.Date(2026, 6, 2, 15, 4, 5, 0, time.UTC)
	if err := m.SetLocalDateTime(tm); err != nil || f.LastWrite() != ":SLDT2026-06-02,15:04:05#" {
		t.Errorf("SetLocalDateTime: %v wrote %q", err, f.LastWrite())
	}
	if d, ok, err := m.NextLeapSecond(); err != nil || !ok || !d.Equal(time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("NextLeapSecond = %v, %v, %v", d, ok, err)
	}
	if valid, exp, err := m.DeltaTStatus(); err != nil || !valid || !exp.Equal(time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("DeltaTStatus = %v, %v, %v", valid, exp, err)
	}
	m2, _ := newMount(map[string]string{":GULEAP#": "E#"}) // none scheduled
	if _, ok, err := m2.NextLeapSecond(); err != nil || ok {
		t.Errorf("NextLeapSecond(E) = ok %v, %v; want false", ok, err)
	}
}

func TestClockGPS(t *testing.T) {
	m, f := newMount(map[string]string{
		":GDGPS#":               "18#",
		":SJD2459580.50000000#": "1",
		":gtgpps#":              "2#",
		":GGPSRW#":              "2048#",
		":SGPSRW#":              "V#",
		":GTTRK#":               "1", // single status byte, no '#'
	})
	if n, err := m.GPSMinusUTC(); err != nil || n != 18 {
		t.Errorf("GPSMinusUTC = %v, %v; want 18", n, err)
	}
	if err := m.SetJulianDate(2459580.5); err != nil || f.LastWrite() != ":SJD2459580.50000000#" {
		t.Errorf("SetJulianDate: %v wrote %q", err, f.LastWrite())
	}
	if s, err := m.GPSSyncState(); err != nil || s != GPSSyncPPS {
		t.Errorf("GPSSyncState = %v, %v; want GPSSyncPPS", s, err)
	}
	if n, err := m.GPSWeekRollover(); err != nil || n != 2048 {
		t.Errorf("GPSWeekRollover = %v, %v; want 2048", n, err)
	}
	if err := m.ResetGPSWeekRollover(); err != nil {
		t.Errorf("ResetGPSWeekRollover: %v", err)
	}
	if ok, err := m.TargetTrackable(); err != nil || !ok {
		t.Errorf("TargetTrackable = %v, %v; want true", ok, err)
	}
}

func TestSettleTimeSetters(t *testing.T) {
	m, f := newMount(map[string]string{
		":Sstm00005.500#":  "1",
		":SDstm00012.000#": "1",
	})
	if err := m.SetSlewSettleTime(5500 * time.Millisecond); err != nil || f.LastWrite() != ":Sstm00005.500#" {
		t.Errorf("SetSlewSettleTime: %v wrote %q", err, f.LastWrite())
	}
	if err := m.SetDomeSettleTime(12 * time.Second); err != nil || f.LastWrite() != ":SDstm00012.000#" {
		t.Errorf("SetDomeSettleTime: %v wrote %q", err, f.LastWrite())
	}
	if err := m.SetSlewSettleTime(200000 * time.Second); err == nil { // > 99999 s
		t.Errorf("SetSlewSettleTime(200000s): want range error")
	}
}

func TestTemperatureSensors(t *testing.T) {
	m, _ := newMount(map[string]string{
		":GTMP9#":   "+023.4#",
		":GTMP15#":  "Unavailable#",
		":GTMPLT#":  "1", // single status byte, no '#'
		":GTMPOH7#": "+070.0#",
		":GTMPTH8#": "-010.0,-005.0,+002.0#",
	})
	if v, err := m.Temperature(TempElectronicsBox); err != nil || math.Abs(v-23.4) > 1e-6 {
		t.Errorf("Temperature(box) = %v, %v", v, err)
	}
	if _, err := m.Temperature(TempRAAzHeater); err != ErrTemperatureUnavailable {
		t.Errorf("Temperature(heater) err = %v; want ErrTemperatureUnavailable", err)
	}
	if lim, err := m.LowTemperatureLimited(); err != nil || !lim {
		t.Errorf("LowTemperatureLimited = %v, %v; want true", lim, err)
	}
	if v, err := m.MotorOverheatThreshold(TempRAAzMotor); err != nil || math.Abs(v-70) > 1e-6 {
		t.Errorf("MotorOverheatThreshold = %v, %v", v, err)
	}
	if t0, t1, t2, err := m.MotorTemperatureThresholds(TempDecAltMotor); err != nil ||
		math.Abs(t0-(-10)) > 1e-6 || math.Abs(t1-(-5)) > 1e-6 || math.Abs(t2-2) > 1e-6 {
		t.Errorf("MotorTemperatureThresholds = %v,%v,%v, %v", t0, t1, t2, err)
	}
	if _, err := m.MotorOverheatThreshold(TempElectronicsBox); err == nil { // not a motor
		t.Errorf("MotorOverheatThreshold(non-motor): want error")
	}
}

func TestTemperatureSetters(t *testing.T) {
	m, f := newMount(map[string]string{
		":STMPOH7,+070.0#":               "1",
		":STMPTH8,-010.0,-005.0,+002.0#": "1",
	})
	if err := m.SetMotorOverheatThreshold(TempRAAzMotor, 70); err != nil || f.LastWrite() != ":STMPOH7,+070.0#" {
		t.Errorf("SetMotorOverheatThreshold: %v wrote %q", err, f.LastWrite())
	}
	if err := m.SetMotorTemperatureThresholds(TempDecAltMotor, -10, -5, 2); err != nil ||
		f.LastWrite() != ":STMPTH8,-010.0,-005.0,+002.0#" {
		t.Errorf("SetMotorTemperatureThresholds: %v wrote %q", err, f.LastWrite())
	}
	if err := m.SetMotorTemperatureThresholds(TempDecAltMotor, 5, 2, 10); err == nil { // T0<T1<T2 violated
		t.Errorf("SetMotorTemperatureThresholds(unordered): want error")
	}
	if err := m.SetMotorOverheatThreshold(TempRAAzMotor, 200); err == nil { // > 80
		t.Errorf("SetMotorOverheatThreshold(200): want range error")
	}
}

func TestWirelessReads(t *testing.T) {
	m, _ := newMount(map[string]string{
		":GWOL#":  "1#",
		":GWRSC#": "1#",
		":GWRAP#": "1HomeNet,Guest$2CNet#", // second AP name contains an escaped comma
	})
	if s, err := m.WakeOnLANState(); err != nil || s != FeatureEnabled {
		t.Errorf("WakeOnLANState = %v, %v; want enabled", s, err)
	}
	if avail, err := m.StartWirelessScan(); err != nil || !avail {
		t.Errorf("StartWirelessScan = %v, %v; want true", avail, err)
	}
	aps, err := m.WirelessAccessPoints()
	if err != nil || len(aps) != 2 || aps[0] != "HomeNet" || aps[1] != "Guest,Net" {
		t.Errorf("WirelessAccessPoints = %q, %v", aps, err)
	}
	// no wireless adapter → empty
	m2, _ := newMount(map[string]string{":GWRAP#": "0#"})
	if aps, err := m2.WirelessAccessPoints(); err != nil || len(aps) != 0 {
		t.Errorf("WirelessAccessPoints(no wifi) = %q, %v", aps, err)
	}
}

func TestFinalApproach(t *testing.T) {
	m, f := newMount(map[string]string{
		":GFAtc#":     "1.50#",
		":GFAlm#":     "3.25#",
		":GFAmd#":     "1#",
		":SFAlm5.00#": "1#",
		":SFAmd0#":    "1#",
		":SFAtc2.50#": "1#",
	})
	if v, err := m.FinalApproachTimeConstant(); err != nil || math.Abs(v-1.5) > 1e-6 {
		t.Errorf("FinalApproachTimeConstant = %v, %v", v, err)
	}
	if v, err := m.FinalApproachDistanceLimit(); err != nil || math.Abs(v-3.25) > 1e-6 {
		t.Errorf("FinalApproachDistanceLimit = %v, %v", v, err)
	}
	if mode, err := m.FinalApproachMode(); err != nil || mode != FinalApproachCustom {
		t.Errorf("FinalApproachMode = %v, %v; want custom", mode, err)
	}
	if err := m.SetFinalApproachDistanceLimit(5.0); err != nil || f.LastWrite() != ":SFAlm5.00#" {
		t.Errorf("SetFinalApproachDistanceLimit: %v wrote %q", err, f.LastWrite())
	}
	if err := m.SetFinalApproachMode(FinalApproachStandard); err != nil || f.LastWrite() != ":SFAmd0#" {
		t.Errorf("SetFinalApproachMode: %v wrote %q", err, f.LastWrite())
	}
	if err := m.SetFinalApproachTimeConstant(2.5); err != nil || f.LastWrite() != ":SFAtc2.50#" {
		t.Errorf("SetFinalApproachTimeConstant: %v wrote %q", err, f.LastWrite())
	}
	if err := m.SetFinalApproachTimeConstant(10); err == nil { // > 5.00 s
		t.Errorf("SetFinalApproachTimeConstant(10): want range error")
	}
	if err := m.SetFinalApproachDistanceLimit(12); err == nil { // > 9.99'
		t.Errorf("SetFinalApproachDistanceLimit(12): want range error")
	}
	// unsupported mount → E#
	m2, _ := newMount(map[string]string{":GFAlm#": "E#"})
	if _, err := m2.FinalApproachDistanceLimit(); err != ErrNotSupported {
		t.Errorf("FinalApproachDistanceLimit(E) = %v; want ErrNotSupported", err)
	}
}

func TestMotionExtras(t *testing.T) {
	m, f := newMount(map[string]string{
		":NUtim+250#": "1#",
		":NUtim-999#": "1#",
		":Rc100#":     "",
		":Rs800#":     "",
	})
	if err := m.NudgeTime(250); err != nil || f.LastWrite() != ":NUtim+250#" {
		t.Errorf("NudgeTime(250): %v wrote %q", err, f.LastWrite())
	}
	if err := m.NudgeTime(-999); err != nil || f.LastWrite() != ":NUtim-999#" {
		t.Errorf("NudgeTime(-999): %v wrote %q", err, f.LastWrite())
	}
	if err := m.NudgeTime(1500); err == nil { // out of ±999
		t.Errorf("NudgeTime(1500): want range error")
	}
	if err := m.StopPreHeating(); err != nil || f.LastWrite() != ":STOPPH#" {
		t.Errorf("StopPreHeating: %v wrote %q", err, f.LastWrite())
	}
	if err := m.SetCenteringRateSidereal(100); err != nil || f.LastWrite() != ":Rc100#" {
		t.Errorf("SetCenteringRateSidereal: %v wrote %q", err, f.LastWrite())
	}
	if err := m.SetSlewRateSidereal(800); err != nil || f.LastWrite() != ":Rs800#" {
		t.Errorf("SetSlewRateSidereal: %v wrote %q", err, f.LastWrite())
	}
	if err := m.SetSlewRateSidereal(5000); err == nil { // > 1200
		t.Errorf("SetSlewRateSidereal(5000): want range error")
	}
}

func TestNetworkConfig(t *testing.T) {
	m, f := newMount(map[string]string{
		":GMAC#":                                 "00:1A:2B:3C:4D:5E#",
		":GMACW#":                                "#", // no wireless interface
		":SIP1#":                                 "1#",
		":SIP0,10.0.0.9,255.255.255.0,10.0.0.1#": "1#",
		":SWOL1#":                                "1", // Ack (single byte)
		":SWRLC#":                                "1#",
		":SWRL0,my$2Cnet,WPA,secret,DHCP#":       "1#",
	})
	if mac, err := m.EthernetMAC(); err != nil || mac != "00:1A:2B:3C:4D:5E" {
		t.Errorf("EthernetMAC = %q, %v", mac, err)
	}
	if mac, err := m.WirelessMAC(); err != nil || mac != "" {
		t.Errorf("WirelessMAC = %q, %v; want \"\"", mac, err)
	}
	if err := m.SetLANDHCP(); err != nil || f.LastWrite() != ":SIP1#" {
		t.Errorf("SetLANDHCP: %v wrote %q", err, f.LastWrite())
	}
	if err := m.SetLANStatic("10.0.0.9", "255.255.255.0", "10.0.0.1"); err != nil ||
		f.LastWrite() != ":SIP0,10.0.0.9,255.255.255.0,10.0.0.1#" {
		t.Errorf("SetLANStatic: %v wrote %q", err, f.LastWrite())
	}
	if err := m.SetWakeOnLAN(true); err != nil || f.LastWrite() != ":SWOL1#" {
		t.Errorf("SetWakeOnLAN: %v wrote %q", err, f.LastWrite())
	}
	if err := m.ShutdownWireless(); err != nil || f.LastWrite() != ":SWRLC#" {
		t.Errorf("ShutdownWireless: %v wrote %q", err, f.LastWrite())
	}
	// SSID with a comma must be escaped so it doesn't break the field list.
	if err := m.ConfigureWirelessClientDHCP("my,net", WirelessWPA, "secret"); err != nil ||
		f.LastWrite() != ":SWRL0,my$2Cnet,WPA,secret,DHCP#" {
		t.Errorf("ConfigureWirelessClientDHCP: %v wrote %q", err, f.LastWrite())
	}
}

func TestCustomTrackRateArcsec(t *testing.T) {
	// 15.041"/s sidereal → wire value 4× = 60.164 → "060.164".
	m, f := newMount(map[string]string{
		":T060.164#":  "1",
		":ST060.164#": "1",
	})
	if err := m.SetCustomTrackRateArcsec(15.041); err != nil || f.LastWrite() != ":T060.164#" {
		t.Errorf("SetCustomTrackRateArcsec: %v wrote %q", err, f.LastWrite())
	}
	if err := m.SetTrackRateArcsec(15.041); err != nil || f.LastWrite() != ":ST060.164#" {
		t.Errorf("SetTrackRateArcsec: %v wrote %q", err, f.LastWrite())
	}
	if err := m.SetTrackRateArcsec(300); err == nil { // 4× = 1200, overflows DDD.DDD
		t.Errorf("SetTrackRateArcsec(300): want range error")
	}
}

func TestSystemAndRelay(t *testing.T) {
	m, f := newMount(map[string]string{
		":shutdown#": "1", // Ack byte
		":GAPO#":     "1#",
		":SAPO0#":    "1", // Ack byte
		":GRLY3#":    "1", // single byte
		":SRLY2,1#":  "1", // Ack byte
		":USERWAIT#": "",  // Blind
		":USEROK#":   "",  // Blind
		":startlog#": "",  // Blind
		":EMULX#":    "",  // Blind
	})
	if err := m.Shutdown(); err != nil || f.LastWrite() != ":shutdown#" {
		t.Errorf("Shutdown: %v wrote %q", err, f.LastWrite())
	}
	if s, err := m.AutoPowerOnState(); err != nil || s != FeatureEnabled {
		t.Errorf("AutoPowerOnState = %v, %v; want enabled", s, err)
	}
	if err := m.SetAutoPowerOn(false); err != nil || f.LastWrite() != ":SAPO0#" {
		t.Errorf("SetAutoPowerOn: %v wrote %q", err, f.LastWrite())
	}
	if closed, err := m.RelayClosed(UserRelay3); err != nil || !closed {
		t.Errorf("RelayClosed(3) = %v, %v; want closed", closed, err)
	}
	if err := m.SetRelay(UserRelay2, true); err != nil || f.LastWrite() != ":SRLY2,1#" {
		t.Errorf("SetRelay: %v wrote %q", err, f.LastWrite())
	}
	if err := m.SetRelay(RAAzHeaterRelay, true); err == nil { // heater relays not settable
		t.Errorf("SetRelay(heater): want error")
	}
	if err := m.UserWait(); err != nil || f.LastWrite() != ":USERWAIT#" {
		t.Errorf("UserWait: %v wrote %q", err, f.LastWrite())
	}
	if err := m.AuthorizeMovement(); err != nil || f.LastWrite() != ":USEROK#" {
		t.Errorf("AuthorizeMovement: %v wrote %q", err, f.LastWrite())
	}
	if err := m.StartCommLog(); err != nil || f.LastWrite() != ":startlog#" {
		t.Errorf("StartCommLog: %v wrote %q", err, f.LastWrite())
	}
	if err := m.EmulateLX200(); err != nil || f.LastWrite() != ":EMULX#" {
		t.Errorf("EmulateLX200: %v wrote %q", err, f.LastWrite())
	}
}

func TestMiscSetters(t *testing.T) {
	m, f := newMount(map[string]string{
		":Slmt15#":     "1",
		":Slms10#":     "1",
		":RMs20#":      "0", // NOTE: :RMs is inverted — '0' = valid
		":SB0#":        "1",
		":Bd00*00:45#": "1",
		":Br00*01:30#": "1",
		":GDw#":        "1#",
	})
	if ok, err := m.SetMeridianTrackLimit(15); err != nil || !ok || f.LastWrite() != ":Slmt15#" {
		t.Errorf("SetMeridianTrackLimit: ok=%v err=%v wrote %q", ok, err, f.LastWrite())
	}
	if ok, err := m.SetMeridianSlewLimit(10); err != nil || !ok || f.LastWrite() != ":Slms10#" {
		t.Errorf("SetMeridianSlewLimit: ok=%v err=%v wrote %q", ok, err, f.LastWrite())
	}
	if err := m.SetAutomatedSlewRate(20); err != nil || f.LastWrite() != ":RMs20#" {
		t.Errorf("SetAutomatedSlewRate: %v wrote %q", err, f.LastWrite())
	}
	if err := m.SetBaudRate(Baud115200); err != nil || f.LastWrite() != ":SB0#" {
		t.Errorf("SetBaudRate: %v wrote %q", err, f.LastWrite())
	}
	if err := m.SetDecBacklash(45); err != nil || f.LastWrite() != ":Bd00*00:45#" {
		t.Errorf("SetDecBacklash: %v wrote %q", err, f.LastWrite())
	}
	if err := m.SetRABacklash(90); err != nil || f.LastWrite() != ":Br00*01:30#" {
		t.Errorf("SetRABacklash: %v wrote %q", err, f.LastWrite())
	}
	if sl, err := m.DomeSlewingExternal(); err != nil || !sl {
		t.Errorf("DomeSlewingExternal = %v, %v; want true", sl, err)
	}
	// :RMs inverted: reply '1' means invalid → error
	m2, _ := newMount(map[string]string{":RMs99#": "1"})
	if err := m2.SetAutomatedSlewRate(99); err == nil {
		t.Errorf("SetAutomatedSlewRate(99, reply 1): want error")
	}
}

func TestIdentityAndParallactic(t *testing.T) {
	m, _ := newMount(map[string]string{
		":GETID#": "01234567890123456789#",
		":GPASZ#": "+012.34567#",
		":GPAZ#":  "+045.00000#",
		":GPA#":   "+044.90000#",
	})
	if id, err := m.HardwareID(); err != nil || id != "01234567890123456789" {
		t.Errorf("HardwareID = %q, %v", id, err)
	}
	if v, err := m.ParallacticSpeedZenith(); err != nil || math.Abs(v-12.34567) > 1e-6 {
		t.Errorf("ParallacticSpeedZenith = %v, %v; want 12.34567", v, err)
	}
	if v, err := m.ParallacticAngleZenith(); err != nil || math.Abs(v-45) > 1e-6 {
		t.Errorf("ParallacticAngleZenith = %v, %v", v, err)
	}
}

func TestDomeGeometryAndControl(t *testing.T) {
	m, f := newMount(map[string]string{
		":SDR2500#":   "",   // Blind
		":SDU05#":     "",   // Blind
		":SDXM+0100#": "",   // Blind
		":SDYM+0000#": "",   // Blind
		":SDZM-0050#": "",   // Blind
		":SDX+0200#":  "",   // Blind
		":SDY+0000#":  "",   // Blind
		":SDA1800#":   "1#", // 180.0° → 1800 tenths
		":SDAr#":      "",   // Blind
	})
	if err := m.SetDomeRadius(2500); err != nil || f.LastWrite() != ":SDR2500#" {
		t.Errorf("SetDomeRadius: %v wrote %q", err, f.LastWrite())
	}
	if err := m.SetDomeUpdateInterval(5); err != nil || f.LastWrite() != ":SDU05#" {
		t.Errorf("SetDomeUpdateInterval: %v wrote %q", err, f.LastWrite())
	}
	if err := m.SetDomeMountOffset(100, 0, -50); err != nil || f.LastWrite() != ":SDZM-0050#" {
		t.Errorf("SetDomeMountOffset: %v last write %q", err, f.LastWrite())
	}
	if err := m.SetDomeOpticalAxisOffset(200, 0); err != nil || f.LastWrite() != ":SDY+0000#" {
		t.Errorf("SetDomeOpticalAxisOffset: %v last write %q", err, f.LastWrite())
	}
	if ok, err := m.SlewDomeToAzimuth(180); err != nil || !ok || f.LastWrite() != ":SDA1800#" {
		t.Errorf("SlewDomeToAzimuth: ok=%v err=%v wrote %q", ok, err, f.LastWrite())
	}
	if _, err := m.SlewDomeToAzimuth(400); err == nil { // > 360°
		t.Errorf("SlewDomeToAzimuth(400): want range error")
	}
	if err := m.ReleaseDomeControl(); err != nil || f.LastWrite() != ":SDAr#" {
		t.Errorf("ReleaseDomeControl: %v wrote %q", err, f.LastWrite())
	}
}

func TestNetworkDiscovery(t *testing.T) {
	m, f := newMount(map[string]string{
		":NTGdisc#":        "1,GM1000#", // available + active, named
		":NTSdisc1,Scope#": "1#",
		":NTGweb#":         "1#",
		":NTSweb0#":        "1#",
	})
	if ds, err := m.DiscoveryService(); err != nil || !ds.Available || !ds.Active || ds.Name != "GM1000" {
		t.Errorf("DiscoveryService = %+v, %v", ds, err)
	}
	if err := m.ConfigureDiscoveryService(true, "Scope"); err != nil || f.LastWrite() != ":NTSdisc1,Scope#" {
		t.Errorf("ConfigureDiscoveryService: %v wrote %q", err, f.LastWrite())
	}
	if on, err := m.WebInterfaceActive(); err != nil || !on {
		t.Errorf("WebInterfaceActive = %v, %v; want true", on, err)
	}
	if err := m.SetWebInterface(false); err != nil || f.LastWrite() != ":NTSweb0#" {
		t.Errorf("SetWebInterface: %v wrote %q", err, f.LastWrite())
	}
	// "0#" alone → not available
	m2, _ := newMount(map[string]string{":NTGdisc#": "0#"})
	if ds, err := m2.DiscoveryService(); err != nil || ds.Available {
		t.Errorf("DiscoveryService(0#) = %+v, %v; want unavailable", ds, err)
	}
}

func TestMountClass(t *testing.T) {
	cases := []struct {
		product            string
		altaz, gm4000, dds bool
	}{
		{"10micron GM1000HPS", false, false, false},
		{"10micron GM4000QCI 48V", false, true, false},
		{"10micron AZ2000", true, false, false},
		{"10micron AZ5000DDS", true, false, true},
		{"10micron AZ2500DDS", true, false, true},
	}
	for _, c := range cases {
		mc := parseMountClass(c.product)
		if mc.AltAz != c.altaz || mc.GM4000 != c.gm4000 || mc.DDS != c.dds {
			t.Errorf("parseMountClass(%q) = %+v", c.product, mc)
		}
	}
	// MountClass() returns the stored classification verbatim.
	stored := MountClass{Product: "10micron GM1000HPS"}
	if got := (&Mount{mountClass: stored}).MountClass(); got != stored {
		t.Errorf("MountClass() = %+v, want %+v", got, stored)
	}
	if got := (&Mount{}).MountClass(); got != (MountClass{}) {
		t.Errorf("MountClass() on bare Mount = %+v, want zero", got)
	}
	if r := (&Mount{mountClass: MountClass{GM4000: true}}).RASlewRatio(); r != 0.75 {
		t.Errorf("RASlewRatio(GM4000) = %v, want 0.75", r)
	}
	if r := (&Mount{}).RASlewRatio(); r != 1.0 {
		t.Errorf("RASlewRatio(default) = %v, want 1.0", r)
	}
	m, _ := newMount(map[string]string{":GCFG#": "E,G,N,H#"})
	if cfg, err := m.MountConfiguration(); err != nil || cfg.AltAz || cfg.Fork || cfg.Southern || !cfg.Homed {
		t.Errorf("MountConfiguration = %+v, %v", cfg, err)
	}
}
