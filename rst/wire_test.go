package rst

import (
	"testing"
	"time"
)

// Every command this package can send, checked byte for byte against the frame PROTOCOL.md
// records. Most of them are silent, so a wrong byte would never be reported by the mount.
func TestWireFormats(t *testing.T) {
	// Commands answering a '#'-terminated frame rather than a bare ack byte need a fixture
	// of the right shape, or the read times out instead of returning.
	framed := map[string]string{
		":CtA#": ":CTA#", ":CtL#": ":CTL#", ":CtS#": ":CTS#", ":CtM#": ":CTM#",
	}
	cases := []struct {
		name string
		want string
		call func(m *Mount) error
	}{
		// browse / find parameters
		{"SetFieldDiameter", ":SF600#", func(m *Mount) error { return m.SetFieldDiameter(600) }},
		{"SetBrightMagnitudeLimit", ":Sb+050#", func(m *Mount) error { return m.SetBrightMagnitudeLimit(5) }},
		{"SetFaintMagnitudeLimit", ":Sf-099#", func(m *Mount) error { return m.SetFaintMagnitudeLimit(-9.9) }},
		{"SetHigherAltitudeLimit", ":Sh00#", func(m *Mount) error { return m.SetHigherAltitudeLimit(0) }},
		// crossed in firmware: the "smaller" setter is :Sl, the "larger" one :Ss
		{"SetSmallerSizeLimit", ":Sl999#", func(m *Mount) error { return m.SetSmallerSizeLimit(999) }},
		{"SetLargerSizeLimit", ":Ss000#", func(m *Mount) error { return m.SetLargerSizeLimit(0) }},
		{"SetMinimumQuality", ":SqSU#", func(m *Mount) error { return m.SetMinimumQuality(QualitySuperb) }},
		{"SetSearchString", ":SyG#", func(m *Mount) error { return m.SetSearchString("G") }},

		// Catalogue. Zero padding is optional, so plain %d.
		{"SelectMessier", ":LM1#", func(m *Mount) error { return m.SelectMessier(1) }},
		{"SelectNGC", ":LC7000#", func(m *Mount) error { return m.SelectNGC(7000) }},
		{"SelectStar", ":LS100#", func(m *Mount) error { return m.SelectStar(100) }},
		{"Find", ":LF#", func(m *Mount) error { return m.Find() }},
		{"FindAlt", ":Lf#", func(m *Mount) error { return m.FindAlt() }},
		{"SelectLibrary", ":Lo0#", func(m *Mount) error { return m.SelectLibrary(0) }},
		{"SelectStarCatalog", ":Ls0#", func(m *Mount) error { return m.SelectStarCatalog(0) }},

		// site slots
		{"SelectSiteSlot", ":W9#", func(m *Mount) error { return m.SelectSiteSlot(9) }},
		{"SelectSiteSlotA", ":Wa#", func(m *Mount) error { return m.SelectSiteSlotA() }},
		{"SetSiteValueL upper", ":WL0100#", func(m *Mount) error { return m.SetSiteValueL(100, true) }},
		{"SetSiteValueL lower", ":Wl0100#", func(m *Mount) error { return m.SetSiteValueL(100, false) }},
		{"SetSiteValueM upper", ":WM0100#", func(m *Mount) error { return m.SetSiteValueM(100, true) }},
		{"SetSiteValueM lower", ":Wm0100#", func(m *Mount) error { return m.SetSiteValueM(100, false) }},

		// satellite
		{"SelectSatelliteSlot", ":Vn03#", func(m *Mount) error { return m.SelectSatelliteSlot(3) }},
		{"CommitSatellite", ":VA03#", func(m *Mount) error { return m.CommitSatellite(3) }},
		{"SatelliteValueB", ":VB07#", func(m *Mount) error { return m.SatelliteValueB(7) }},
		{"SatelliteValueF", ":VF1#", func(m *Mount) error { return m.SatelliteValueF(1) }},
		{"SatelliteValueLowerB", ":Vb00000042#", func(m *Mount) error { return m.SatelliteValueLowerB(42) }},
		{"SatelliteFlag", ":VT#", func(m *Mount) error { return m.SatelliteFlag('T') }},

		// misc / asymmetries closed
		{"SetAutoResume on", ":CrR#", func(m *Mount) error { return m.SetAutoResume(true) }},
		{"SetAutoResume off", ":CrX#", func(m *Mount) error { return m.SetAutoResume(false) }},
		{"ToggleClockFormat", ":H#", func(m *Mount) error { return m.ToggleClockFormat() }},
		{"StarAlign", ":CS#", func(m *Mount) error { return m.StarAlign() }},
		{"SetDecAxisOffset", ":Cg+00.0000#", func(m *Mount) error { return m.SetDecAxisOffset(0) }},
		{"SetSlewLimit 0", ":Ca85.000#", func(m *Mount) error { return m.SetSlewLimit(0, 85) }},
		{"SetSlewLimit 5", ":Cf-6.900#", func(m *Mount) error { return m.SetSlewLimit(5, -6.9) }},
		{"SetRateRegister B", ":MB2000#", func(m *Mount) error { return m.SetRateRegister(true, 2000) }},
		{"SetRateRegister C", ":MC0600#", func(m *Mount) error { return m.SetRateRegister(false, 600) }},
		{"SetEncoderRate", ":Mb1 1#", func(m *Mount) error { return m.SetEncoderRate('b', "1 1") }},
		{"AdjustDateTime", ":SM-01#", func(m *Mount) error { return m.AdjustDateTime(-1) }},

		// motion, tracking, rates
		{"Halt", ":Q#", func(m *Mount) error { return m.Halt() }},
		{"SetTracking on", ":CtA#", func(m *Mount) error { return m.SetTracking(true) }},
		{"SetTracking off", ":CtL#", func(m *Mount) error { return m.SetTracking(false) }},
		{"TrackSolar", ":CtS#", func(m *Mount) error { return m.TrackSolar() }},
		{"TrackLunar", ":CtM#", func(m *Mount) error { return m.TrackLunar() }},
		{"SetForcePierFlip on", ":Af1#", func(m *Mount) error { return m.SetForcePierFlip(true) }},
		{"SetForcePierFlip off", ":Af0#", func(m *Mount) error { return m.SetForcePierFlip(false) }},
		{"SetGuideRate", ":Cu0=0.5#", func(m *Mount) error { return m.SetGuideRate(0.5) }},
		{"SlewToTargetAlt", ":MD#", func(m *Mount) error { return m.SlewToTargetAlt() }},

		// Clock, site and precision, all blind and verified silent on hardware.
		{"SetUTCOffset", ":SG+07#", func(m *Mount) error { return m.SetUTCOffset(7 * time.Hour) }},
		{"SetUTCOffset negative", ":SG-05#", func(m *Mount) error { return m.SetUTCOffset(-5 * time.Hour) }},
		{"SetLocalTime", ":SL07:24:45#", func(m *Mount) error {
			return m.SetLocalTime(time.Date(2026, 8, 25, 7, 24, 45, 0, time.UTC))
		}},
		{"SetSiteLatitude", ":St+37*57'00#", func(m *Mount) error { return m.SetSiteLatitude(37.95) }},
		{"SetSiteLongitude", ":Sg+122*33'00#", func(m *Mount) error { return m.SetSiteLongitude(-122.55) }},
		{"SetPrecision high", ":SPH#", func(m *Mount) error { return m.SetPrecision(true) }},
		{"SetPrecision low", ":SPL#", func(m *Mount) error { return m.SetPrecision(false) }},
		{"EchoPrefix on", ":SPE#", func(m *Mount) error { return m.EchoPrefix(true) }},
		{"EchoPrefix off", ":SPF#", func(m *Mount) error { return m.EchoPrefix(false) }},

		// unresolved commands: the wire format is still pinned
		{"SetDebugZ", ":Cz1#", func(m *Mount) error { return m.SetDebugZ("1") }},
		{"SetInitFlags", ":Cw1#", func(m *Mount) error { return m.SetInitFlags("1") }},
		{"SetQ", ":CQ1#", func(m *Mount) error { return m.SetQ("1") }},
		{"SetTrackingRelatedO", ":Co1#", func(m *Mount) error { return m.SetTrackingRelatedO("1") }},
		{"SetTemperatureCal", ":Cj1#", func(m *Mount) error { return m.SetTemperatureCal("1") }},
		{"GetX", ":GX1#", func(m *Mount) error { return m.GetX("1") }},
		{"ResetDateTimeField", ":SNH#", func(m *Mount) error { return m.ResetDateTimeField("H") }},
		{"SetSS", ":SS1#", func(m *Mount) error { return m.SetSS("1") }},
		{"SetSm", ":Sm1#", func(m *Mount) error { return m.SetSm("1") }},
		{"SetSn", ":Sn1#", func(m *Mount) error { return m.SetSn("1") }},
		{"SatelliteValueC", ":VC07#", func(m *Mount) error { return m.SatelliteValueC(7) }},
		{"SatelliteValueD", ":VD07#", func(m *Mount) error { return m.SatelliteValueD(7) }},
		{"SelectSiteSlot 0", ":W0#", func(m *Mount) error { return m.SelectSiteSlot(0) }},

		// unsafe
		{"SetModelName", ":AmRST-135E#", func(m *Mount) error { return m.Unsafe().SetModelName("RST-135E") }},
		{"WriteFactoryBlockA", ":Aax,1,2#", func(m *Mount) error { return m.Unsafe().WriteFactoryBlockA("x,1,2") }},
		{"WriteFactoryBlockB", ":Ab1.0,2,3,4#", func(m *Mount) error { return m.Unsafe().WriteFactoryBlockB("1.0,2,3,4") }},
		{"SelectLX200Dialect", ":AL#", func(m *Mount) error { return m.Unsafe().SelectLX200Dialect() }},
		{"SelectWiFiTransport", ":AW#", func(m *Mount) error { return m.Unsafe().SelectWiFiTransport() }},
		{"SetTransmitFlag", ":Ba1#", func(m *Mount) error { return m.Unsafe().SetTransmitFlag(1) }},
		{"SPIWriteD", ":Fd001234?0056#", func(m *Mount) error { return m.Unsafe().SPIWriteD(1234, 56) }},
		{"SPIRaw", ":FB01#", func(m *Mount) error { return m.Unsafe().SPIRaw('B', "01") }},
		{"PEC lower", ":Pu#", func(m *Mount) error { return m.Unsafe().PEC('u') }},
		{"SetGearRatio", ":Ag017842176*008921088#", func(m *Mount) error {
			return m.Unsafe().SetGearRatio(17842176, 8921088)
		}},
		{"SetWormCount", ":Ap0100*0100#", func(m *Mount) error { return m.Unsafe().SetWormCount(100, 100) }},
		{"SetSerialNumber", ":As350021#", func(m *Mount) error { return m.Unsafe().SetSerialNumber(350021) }},
		{"SetDebugReport on", ":AX#", func(m *Mount) error { return m.Unsafe().SetDebugReport(true) }},
		{"SetDebugReport off", ":Ax#", func(m *Mount) error { return m.Unsafe().SetDebugReport(false) }},
		{"SPIWriteA", ":Fa001234?0056#", func(m *Mount) error { return m.Unsafe().SPIWriteA(1234, 56) }},
		{"Diagnostic", ":XR#", func(m *Mount) error { return m.Unsafe().Diagnostic('R', "") }},
		{"PEC", ":PA#", func(m *Mount) error { return m.Unsafe().PEC('A') }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rep, ok := framed[c.want]
			if !ok {
				rep = "1" // ack-taking commands; blind ones ignore it
			}
			m, f := newMount(map[string]string{c.want: rep})
			if err := c.call(m); err != nil {
				t.Fatalf("%s: %v", c.name, err)
			}
			if got := f.LastWrite(); got != c.want {
				t.Errorf("%s wrote %q, want %q", c.name, got, c.want)
			}
		})
	}
}

// Arguments outside the ranges the firmware parses must be refused here rather than sent as a
// malformed frame. A malformed frame is the worst case in this dialect: silent, and it can
// leave the mount in a state nothing reports, which is how the development mount's clock
// ended up at 00:26.
func TestRejectsOutOfRangeArguments(t *testing.T) {
	m, f := newMount(nil)
	bad := []struct {
		name string
		call func() error
	}{
		{"satellite slot high", func() error { return m.SelectSatelliteSlot(MaxSatellites) }},
		{"satellite slot negative", func() error { return m.SelectSatelliteSlot(-1) }},
		{"commit slot high", func() error { return m.CommitSatellite(99) }},
		{"slew-limit index", func() error { return m.SetSlewLimit(6, 0) }},
		{"clock format", func() error { return m.SetClockFormat(13) }},
		{"satellite flag", func() error { return m.SatelliteFlag('Z') }},
		{"spi command", func() error { return m.Unsafe().SPIRaw('Z', "") }},
		{"diagnostic", func() error { return m.Unsafe().Diagnostic('Z', "") }},
		{"pec", func() error { return m.Unsafe().PEC('Z') }},
		{"encoder rate", func() error { return m.SetEncoderRate('z', "") }},
	}
	for _, c := range bad {
		if err := c.call(); err == nil {
			t.Errorf("%s: want an error, got nil", c.name)
		}
		if got := f.LastWrite(); got != "" {
			t.Errorf("%s wrote %q; nothing should reach the mount", c.name, got)
		}
	}
}
