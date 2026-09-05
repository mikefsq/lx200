// Command tenmicron provides an interactive console for 10Micron mounts.
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/mikefsq/lx200"
	"github.com/mikefsq/lx200/tenmicron"
)

func main() {
	addr := flag.String("addr", "", "mount TCP address, e.g. 10.0.1.51:3492 (required)")
	timeout := flag.Duration("timeout", 3*time.Second, "reply timeout for raw queries/awaits")
	flag.Parse()

	if *addr == "" {
		log.Fatal("tenmicron: -addr is required (e.g. -addr 10.0.1.51:3492)")
	}
	m, err := tenmicron.Connect(*addr)
	if err != nil {
		log.Fatalf("tenmicron: connect: %v", err)
	}
	defer m.Close()

	if v, err := m.Firmware(); err == nil {
		fmt.Printf("connected %s — firmware %q (%s), %s\n", *addr, v, m.FirmwareVersion(), m.MountClass().Product)
	} else {
		fmt.Printf("connected %s (version read failed: %v)\n", *addr, err)
	}

	if args := flag.Args(); len(args) > 0 {
		run(m, strings.Join(args, " "), *timeout)
		return
	}

	fmt.Println("type 'help' for commands, 'quit' to exit")
	sc := bufio.NewScanner(os.Stdin)
	fmt.Print("10u> ")
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch line {
		case "quit", "exit", "q":
			return
		case "":
		default:
			run(m, line, *timeout)
		}
		fmt.Print("10u> ")
	}
}

func run(m *tenmicron.Mount, line string, timeout time.Duration) {
	fields := strings.Fields(line)
	verb := strings.ToLower(fields[0])
	args := fields[1:]

	switch verb {
	case "g", "get":
		if len(args) != 1 {
			fmt.Println("usage: g :CMD#")
			return
		}
		report(m.Get(args[0]))
	case "a", "ack":
		if len(args) != 1 {
			fmt.Println("usage: a :CMD#")
			return
		}
		b, err := m.AckByte(args[0])
		if err != nil {
			fmt.Printf("err: %v\n", err)
			return
		}
		fmt.Printf("byte: %q (0x%02x)\n", string(b), b)
	case "b", "blind":
		if len(args) != 1 {
			fmt.Println("usage: b :CMD#")
			return
		}
		if err := m.Blind(args[0]); err != nil {
			fmt.Printf("err: %v\n", err)
			return
		}
		fmt.Println("sent (no reply expected)")
	case "w", "await":
		d := timeout
		if len(args) == 1 {
			if ms, err := strconv.Atoi(args[0]); err == nil {
				d = time.Duration(ms) * time.Millisecond
			}
		}
		report(m.Await(d))

	case "ra":
		reportF(m.RA())
	case "dec":
		reportF(m.Dec())
	case "alt":
		reportF(m.Altitude())
	case "az":
		reportF(m.Azimuth())
	case "lst":
		reportF(m.SiderealTime())
	case "pier":
		reportS(pierStr(m.PierSide()))
	case "pointing":
		reportS(pierStr(m.PointingState()))
	case "destpier":
		reportS(pierStr(m.DestinationSideOfPier()))
	case "slewing":
		reportB(m.Slewing())
	case "tracking":
		reportB(m.Tracking())
	case "trackactive":
		reportB(m.TrackingActive())
	case "dualaxis":
		reportB(m.DualAxisTracking())
	case "athome":
		reportB(m.AtHome())
	case "atpark":
		reportB(m.AtPark())
	case "statuscode":
		reportI(m.StatusCode())
	case "slewprog":
		reportB(m.SlewInProgress())
	case "trackable":
		reportB(m.TargetTrackable())
	case "ginfo", "status", "st":
		ginfo(m)
	case "dump":
		dump(m)
	case "version", "ver":
		report(m.Firmware())
	case "fwdate":
		reportT(m.FirmwareDate())
	case "fwtime":
		report(m.FirmwareTime())
	case "product":
		report(m.Product())
	case "hwid":
		report(m.HardwareID())
	case "controlbox":
		report(m.ControlBoxVersion())
	case "mountcfg":
		mountCfg(m)
	case "alarms":
		alarms(m)
	case "alarmack":
		if len(args) != 1 {
			fmt.Println("usage: alarmack <alarm-id>   (e.g. 1001)")
			return
		}
		n, err := strconv.Atoi(args[0])
		if err != nil {
			fmt.Printf("err: bad alarm id %q\n", args[0])
			return
		}
		reportErr(m.AcknowledgeAlarm(tenmicron.Alarm(n)))

	case "clock", "site":
		runProbes(clockProbes(m))
	case "limits":
		runProbes(limitProbes(m))
	case "rates":
		runProbes(rateProbes(m))
	case "refraction":
		runProbes(refractionProbes(m))
	case "target", "tgt":
		runProbes(targetProbes(m))
	case "network", "net":
		runProbes(networkProbes(m))
	case "dome":
		domeCmd(m, args)
	case "focuser", "foc":
		focuserCmd(m, args)
	case "focuser1", "f1":
		focuser1Cmd(m, args)
	case "rotator", "rot":
		rotatorCmd(m, args)
	case "sat", "satellite":
		satCmd(m, args)
	case "guiderate":
		reportF(m.GuideRate())
	case "temp":
		if len(args) != 1 {
			fmt.Println("usage: temp <sensor#>  (1 RA drv,2 Dec drv,9 elec box,…)")
			return
		}
		n, _ := strconv.Atoi(args[0])
		if v, err := m.Temperature(tenmicron.TemperatureElement(n)); err != nil {
			fmt.Printf("err: %v\n", err)
		} else {
			fmt.Printf("value: %.2f °C\n", v)
		}
	case "model":
		model(m)

	case "sr", "settargetra":
		if h, ok := pF(args, "sr <hours>"); ok {
			reportOK(m.SetTargetRA(h))
		}
	case "sd", "settargetdec":
		if d, ok := pF(args, "sd <deg>"); ok {
			reportOK(m.SetTargetDec(d))
		}
	case "sa", "settargetalt":
		if d, ok := pF(args, "sa <deg>"); ok {
			reportOK(m.SetTargetAltitude(d))
		}
	case "sz", "settargetaz":
		if d, ok := pF(args, "sz <deg>"); ok {
			reportOK(m.SetTargetAzimuth(d))
		}

	case "setsitelat":
		if d, ok := pF(args, "setsitelat <deg>"); ok {
			reportErr(m.SetSiteLatitude(d))
		}
	case "setsitelon":
		if d, ok := pF(args, "setsitelon <deg, east+>"); ok {
			reportErr(m.SetSiteLongitude(d))
		}
	case "setsiteelev":
		if d, ok := pF(args, "setsiteelev <meters>"); ok {
			reportErr(m.SetSiteElevation(d))
		}
	case "setutcoffset":
		if h, ok := pF(args, "setutcoffset <hours>"); ok {
			reportErr(m.SetUTCOffset(time.Duration(h * float64(time.Hour))))
		}
	case "setutc":
		reportErr(m.SetUTC(parseWhen(args)))
	case "setclock": // set date+time together from UTC now (or a given RFC3339)
		t := parseWhen(args)
		if err := m.SetDate(t); err != nil {
			fmt.Printf("err (date): %v\n", err)
			return
		}
		reportErr(m.SetTime(t))
	case "setjd":
		if jd, ok := pF(args, "setjd <julianDate>"); ok {
			reportErr(m.SetJulianDate(jd))
		}
	case "nudgetime":
		if ms, ok := pI(args, "nudgetime <±ms>"); ok {
			reportErr(m.NudgeTime(ms))
		}
	case "updategps":
		reportOK(m.UpdateFromGPS())

	case "sethighalt":
		if d, ok := pI(args, "sethighalt <deg>"); ok {
			reportOK(m.SetHighAltitudeLimit(d))
		}
	case "setlowalt":
		if d, ok := pI(args, "setlowalt <deg, -5..45>"); ok {
			reportOK(m.SetLowAltitudeLimit(d))
		}
	case "setmertrack":
		if d, ok := pI(args, "setmertrack <deg>"); ok {
			reportOK(m.SetMeridianTrackLimit(d))
		}
	case "setmerslew":
		if d, ok := pI(args, "setmerslew <deg>"); ok {
			reportOK(m.SetMeridianSlewLimit(d))
		}
	case "setmerside":
		if s, ok := parseMerSide(args); ok {
			reportOK(m.SetMeridianSideBehaviour(s))
		}
	case "setsettle":
		if s, ok := pF(args, "setsettle <seconds>"); ok {
			reportErr(m.SetSlewSettleTime(time.Duration(s * float64(time.Second))))
		}
	case "setunattended":
		if on, ok := onOff(args); ok {
			reportErr(m.SetUnattendedFlip(on))
		}

	case "setmaxslew":
		if d, ok := pI(args, "setmaxslew <degPerSec>"); ok {
			reportOK(m.SetMaxSlewRate(d))
		}
	case "setautoslew":
		if d, ok := pI(args, "setautoslew <degPerSec>"); ok {
			reportErr(m.SetAutomatedSlewRate(d))
		}
	case "setguideidx":
		if n, ok := pI(args, "setguideidx <0-2> (0.25/0.5/1.0×)"); ok {
			reportErr(m.SetGuidingRateIndex(n))
		}
	case "setcenteridx":
		if n, ok := pI(args, "setcenteridx <0-3> (16/64/600/1200×)"); ok {
			reportErr(m.SetCenteringRateIndex(n))
		}
	case "setslewidx":
		if n, ok := pI(args, "setslewidx <0-2> (1200/900/600×)"); ok {
			reportErr(m.SetSlewRateIndex(n))
		}
	case "setcenter":
		if x, ok := pI(args, "setcenter <1-255 ×sidereal>"); ok {
			reportErr(m.SetCenteringRateSidereal(x))
		}
	case "setslew":
		if x, ok := pI(args, "setslew <1-1200 ×sidereal>"); ok {
			reportErr(m.SetSlewRateSidereal(x))
		}
	case "setguiderate":
		if v, ok := pF(args, "setguiderate <arcsec/s>"); ok {
			reportErr(m.SetGuideRate(v))
		}
	case "setguideport":
		if on, ok := onOff(args); ok {
			reportErr(m.SetGuiderPortEnabled(on))
		}

	case "precision":
		precision(m, args)

	case "setrefraction":
		if len(args) != 2 {
			fmt.Println("usage: setrefraction <hPa> <tempC>")
			return
		}
		p, e1 := strconv.ParseFloat(args[0], 64)
		t, e2 := strconv.ParseFloat(args[1], 64)
		if e1 != nil || e2 != nil {
			fmt.Println("err: hPa/tempC must be numbers")
			return
		}
		reportErr(m.SetRefraction(p, t))
	case "setpressure":
		if v, ok := pF(args, "setpressure <hPa>"); ok {
			reportErr(m.SetRefractionPressure(v))
		}
	case "settemp":
		if v, ok := pF(args, "settemp <°C>"); ok {
			reportErr(m.SetRefractionTemperature(v))
		}
	case "refcorr":
		if on, ok := onOff(args); ok {
			reportOK(m.SetRefractionCorrection(on))
		}
	case "speedcorr":
		if on, ok := onOff(args); ok {
			reportOK(m.SetSpeedCorrection(on))
		}

	case "setdecbacklash":
		if v, ok := pF(args, "setdecbacklash <arcsec>"); ok {
			reportErr(m.SetDecBacklash(v))
		}
	case "setrabacklash":
		if v, ok := pF(args, "setrabacklash <arcsec>"); ok {
			reportErr(m.SetRABacklash(v))
		}

	case "trackrate":
		if r, ok := parseTrackRate(args); ok {
			reportErr(m.SetTrackRate(r))
		}
	case "setdualaxis":
		if on, ok := onOff(args); ok {
			reportErr(m.SetDualAxisTracking(on))
		}
	case "setrarate":
		if v, ok := pF(args, "setrarate <×sidereal>"); ok {
			reportErr(m.SetCustomRARate(v))
		}
	case "setdecrate":
		if v, ok := pF(args, "setdecrate <×sidereal>"); ok {
			reportErr(m.SetCustomDecRate(v))
		}
	case "settrackarcsec":
		if v, ok := pF(args, "settrackarcsec <arcsec/s>"); ok {
			reportErr(m.SetTrackRateArcsec(v))
		}

	case "slew":
		startWait(m, m.SlewToTarget())
	case "slewfine":
		startWait(m, m.SlewToTargetFineLimit())
	case "altaz": // horizontal goto: altaz <az> <alt>
		if len(args) != 2 {
			fmt.Println("usage: altaz <az> <alt>")
			return
		}
		az, e1 := strconv.ParseFloat(args[0], 64)
		alt, e2 := strconv.ParseFloat(args[1], 64)
		if e1 != nil || e2 != nil {
			fmt.Println("err: az/alt must be numbers")
			return
		}
		startWait(m, m.SlewToAltAz(alt, az))
	case "sync":
		_, err := m.SyncToTarget()
		reportErr(err)
	case "synccurrent": // sync to the mount's own current RA/Dec — should be a no-op
		syncCurrent(m)
	case "flip":
		startWait(m, m.Flip())
	case "nudge":
		if len(args) != 2 {
			fmt.Println("usage: nudge <raAzArcsec> <decAltArcsec>")
			return
		}
		ra, e1 := strconv.Atoi(args[0])
		dec, e2 := strconv.Atoi(args[1])
		if e1 != nil || e2 != nil {
			fmt.Println("err: offsets must be integers (arcsec)")
			return
		}
		startWait(m, m.Nudge(ra, dec))
	case "home":
		startWait(m, m.FindHome())
	case "park":
		startWait(m, m.Park())
	case "raaxis": // no arg: go to the RA-axis reference (RA 90°, Dec 0°) and STOP.
		// with <deg>: rotate the RA axis to that exact mechanical angle (Dec unchanged).
		if len(args) == 0 {
			startWait(m, m.SlewToRAAxis())
		} else if d, ok := pF(args, "raaxis [<deg>]"); ok {
			startWait(m, m.RotateRAAxis(d))
		}
	case "unpark":
		reportErr(m.Unpark())
	case "stop": // :STOP# — stops tracking too
		reportErr(m.Stop())
	case "halt": // :Q# — stops slewing only
		reportErr(m.Halt())

	case "track":
		if on, ok := onOff(args); ok {
			reportErr(m.SetTracking(on))
		}
	case "sidereal":
		reportErr(m.TrackSidereal())
	case "solar":
		reportErr(m.TrackSolar())
	case "lunar":
		reportErr(m.TrackLunar())

	case "move":
		if d, ok := dir(args); ok {
			reportErr(m.Move(d))
		}
	case "rate":
		if r, ok := rate(args); ok {
			reportErr(m.SetRate(r))
		}
	case "moveaxis":
		moveAxis(m, args)
	case "stopaxis":
		if a, ok := axis(args); ok {
			reportErr(m.StopAxis(a))
		}
	case "pulse":
		pulse(m, args)

	case "help", "?", "h":
		help()
	default:
		if strings.HasPrefix(verb, ":") {
			report(m.Get(fields[0]))
			return
		}
		fmt.Printf("unknown command %q (try 'help')\n", verb)
	}
}

func pF(args []string, usage string) (float64, bool) {
	if len(args) != 1 {
		fmt.Println("usage: " + usage)
		return 0, false
	}
	v, err := strconv.ParseFloat(args[0], 64)
	if err != nil {
		fmt.Printf("err: %v\n", err)
		return 0, false
	}
	return v, true
}

func pI(args []string, usage string) (int, bool) {
	if len(args) != 1 {
		fmt.Println("usage: " + usage)
		return 0, false
	}
	v, err := strconv.Atoi(args[0])
	if err != nil {
		fmt.Printf("err: %v\n", err)
		return 0, false
	}
	return v, true
}

func onOff(args []string) (bool, bool) {
	if len(args) != 1 || (args[0] != "on" && args[0] != "off") {
		fmt.Println("usage: <verb> on|off")
		return false, false
	}
	return args[0] == "on", true
}

func openClose(args []string) (bool, bool) {
	if len(args) != 1 || (args[0] != "open" && args[0] != "close") {
		fmt.Println("usage: <verb> open|close")
		return false, false
	}
	return args[0] == "open", true
}

// subArgs splits a "<verb> <index> <sub> <rest…>" line: it returns the lowercased
// sub-command (args[1]) and the remaining args (args[2:]).
func subArgs(args []string) (sub string, rest []string) {
	if len(args) > 1 {
		sub = strings.ToLower(args[1])
	}
	if len(args) > 2 {
		rest = args[2:]
	}
	return sub, rest
}

// pI2 parses two ints from exactly two args.
func pI2(args []string, usage string) (int, int, bool) {
	if len(args) != 2 {
		fmt.Println("usage: " + usage)
		return 0, 0, false
	}
	a, e1 := strconv.Atoi(args[0])
	b, e2 := strconv.Atoi(args[1])
	if e1 != nil || e2 != nil {
		fmt.Println("err: both values must be integers")
		return 0, 0, false
	}
	return a, b, true
}

func rotFrame(s string) (tenmicron.RotatorFrame, bool) {
	switch strings.ToLower(s) {
	case "mech", "m":
		return tenmicron.RotatorMechanical, true
	case "equ", "eq", "r":
		return tenmicron.RotatorEquatorial, true
	case "opt", "o":
		return tenmicron.RotatorOptical, true
	}
	return 0, false
}

// parseWhen returns UTC now, or the RFC3339 timestamp in args[0].
func parseWhen(args []string) time.Time {
	if len(args) == 1 {
		if t, err := time.Parse(time.RFC3339, args[0]); err == nil {
			return t.UTC()
		}
		fmt.Println("warn: bad RFC3339 time, using now")
	}
	return time.Now().UTC()
}

func parseMerSide(args []string) (tenmicron.MeridianSide, bool) {
	if len(args) == 1 {
		switch strings.ToLower(args[0]) {
		case "both":
			return tenmicron.MeridianBothSides, true
		case "west":
			return tenmicron.MeridianWestOnly, true
		case "east":
			return tenmicron.MeridianEastOnly, true
		}
	}
	fmt.Println("usage: setmerside both|west|east")
	return 0, false
}

func parseTrackRate(args []string) (tenmicron.TrackRate, bool) {
	if len(args) == 1 {
		switch strings.ToLower(args[0]) {
		case "lunar":
			return tenmicron.TrackLunarRate, true
		case "solar":
			return tenmicron.TrackSolarRate, true
		case "sidereal":
			return tenmicron.TrackSiderealRate, true
		case "stop":
			return tenmicron.TrackStopped, true
		}
	}
	fmt.Println("usage: trackrate lunar|solar|sidereal|stop")
	return 0, false
}

func dir(args []string) (lx200.Direction, bool) {
	if len(args) != 1 {
		fmt.Println("usage: <verb> n|s|e|w")
		return 0, false
	}
	switch strings.ToLower(args[0]) {
	case "n":
		return lx200.North, true
	case "s":
		return lx200.South, true
	case "e":
		return lx200.East, true
	case "w":
		return lx200.West, true
	}
	fmt.Printf("bad direction %q (n|s|e|w)\n", args[0])
	return 0, false
}

func rate(args []string) (lx200.Rate, bool) {
	if len(args) != 1 {
		fmt.Println("usage: rate g|c|f|s   (guide|center|find|slew)")
		return 0, false
	}
	switch strings.ToLower(args[0]) {
	case "g":
		return lx200.RateGuide, true
	case "c":
		return lx200.RateCenter, true
	case "f", "m":
		return lx200.RateFind, true
	case "s":
		return lx200.RateMax, true
	}
	fmt.Printf("bad rate %q (g|c|f|s)\n", args[0])
	return 0, false
}

func axis(args []string) (lx200.Axis, bool) {
	if len(args) < 1 {
		fmt.Println("usage: <verb> pri|sec ...")
		return 0, false
	}
	switch strings.ToLower(args[0]) {
	case "pri", "p", "0", "ra", "az":
		return lx200.AxisPrimary, true
	case "sec", "s", "1", "dec", "alt":
		return lx200.AxisSecondary, true
	}
	fmt.Printf("bad axis %q (pri|sec)\n", args[0])
	return 0, false
}

// moveAxis drives an axis at an EXACT rate in deg/s (MoveAxisRate), unlike the
// coarse preset 'move'. usage: moveaxis pri|sec +|- <degPerSec>
func moveAxis(m *tenmicron.Mount, args []string) {
	if len(args) != 3 {
		fmt.Println("usage: moveaxis pri|sec +|- <degPerSec>")
		return
	}
	a, ok := axis(args)
	if !ok {
		return
	}
	positive := args[1] != "-"
	dps, err := strconv.ParseFloat(args[2], 64)
	if err != nil {
		fmt.Printf("err: rate must be a number (deg/s): %v\n", err)
		return
	}
	reportErr(m.MoveAxisRate(a, positive, dps))
}

func pulse(m *tenmicron.Mount, args []string) {
	if len(args) != 2 {
		fmt.Println("usage: pulse n|s|e|w <ms>")
		return
	}
	d, ok := dir(args[:1])
	if !ok {
		return
	}
	ms, err := strconv.Atoi(args[1])
	if err != nil {
		fmt.Printf("err: %v\n", err)
		return
	}
	reportErr(m.PulseGuide(d, ms))
}

func precision(m *tenmicron.Mount, args []string) {
	if len(args) != 1 {
		fmt.Println("usage: precision low|high|ultra|toggle")
		return
	}
	switch strings.ToLower(args[0]) {
	case "low":
		reportErr(m.SetPrecisionLow())
	case "high":
		reportErr(m.SetPrecisionHigh())
	case "ultra":
		reportErr(m.SetPrecisionUltra())
	case "toggle":
		reportErr(m.TogglePrecision())
	default:
		fmt.Println("usage: precision low|high|ultra|toggle")
	}
}

func syncCurrent(m *tenmicron.Mount) {
	ra, err := m.RA()
	if err != nil {
		fmt.Printf("err: %v\n", err)
		return
	}
	dec, err := m.Dec()
	if err != nil {
		fmt.Printf("err: %v\n", err)
		return
	}
	if _, err := m.SetTargetRA(ra); err != nil {
		fmt.Printf("err: %v\n", err)
		return
	}
	if _, err := m.SetTargetDec(dec); err != nil {
		fmt.Printf("err: %v\n", err)
		return
	}
	fmt.Printf("syncing to current RA=%.5f Dec=%.5f ...\n", ra, dec)
	_, err = m.SyncToTarget()
	reportErr(err)
}

// startWait reports a goto-launch error, or waits for the slew to finish.
func startWait(m *tenmicron.Mount, err error) {
	if err != nil {
		fmt.Printf("err: %v\n", err)
		return
	}
	waitSlew(m)
}

// waitSlew polls until the mount stops slewing (or a 3-min cap), then prints the
// final :Ginfo# so you can confirm it landed on target.
func waitSlew(m *tenmicron.Mount) {
	fmt.Println("slewing...")
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		sl, err := m.Slewing()
		if err != nil {
			fmt.Printf("err: %v\n", err)
			return
		}
		if !sl {
			ginfo(m)
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	fmt.Println("timeout waiting for slew to finish")
}

func ginfo(m *tenmicron.Mount) {
	s, err := m.Refresh()
	if err != nil {
		fmt.Printf("err: %v\n", err)
		return
	}
	fmt.Printf("  ra=%.5f dec=%.5f alt=%.5f az=%.5f pier=%v gstat=%d slew=%v\n"+
		"  tracking=%v slewing=%v parked=%v\n",
		s.RA, s.Dec, s.Alt, s.Az, s.Pier, s.Gstat, s.Slew,
		s.IsTracking(), s.IsSlewing(), s.IsParked())
}

func mountCfg(m *tenmicron.Mount) {
	c := m.MountClass()
	fmt.Printf("  class: product=%q altaz=%v gm4000=%v dds=%v raSlewRatio=%.2f\n",
		c.Product, c.AltAz, c.GM4000, c.DDS, m.RASlewRatio())
	if cfg, err := m.MountConfiguration(); err != nil {
		fmt.Printf("  :GCFG# err: %v\n", err)
	} else {
		fmt.Printf("  live:  altaz=%v fork=%v southern=%v homed=%v\n",
			cfg.AltAz, cfg.Fork, cfg.Southern, cfg.Homed)
	}
}

// alarms reports the DDS alarm lists (:alarmlistact# / :alarmlistunack#, firmware
// ≥ 3.4). Non-DDS mounts do not answer these, so the reads time out there.
func alarms(m *tenmicron.Mount) {
	for _, l := range []struct {
		title string
		fn    func() ([]tenmicron.Alarm, error)
	}{
		{"active", m.ActiveAlarms},
		{"unacknowledged", m.UnacknowledgedAlarms},
	} {
		list, err := l.fn()
		switch {
		case err != nil:
			fmt.Printf("  %-16s err: %v\n", l.title, err)
		case len(list) == 0:
			fmt.Printf("  %-16s none\n", l.title)
		default:
			for _, a := range list {
				fmt.Printf("  %-16s %d (%s)\n", l.title, int(a), a)
			}
		}
	}
}

func model(m *tenmicron.Mount) {
	if n, err := m.AlignmentStarCount(); err != nil {
		fmt.Printf("  starCount err: %v\n", err)
	} else {
		fmt.Printf("  alignment stars: %d\n", n)
	}
	info, err := m.AlignmentInfo()
	if err != nil {
		fmt.Printf("  :getain# err: %v\n", err)
		return
	}
	fmt.Printf("  az=%.4f alt=%.4f polarErr=%.4f posAngle=%.4f orthoErr=%.4f\n"+
		"  azTurns=%.2f altTurns=%.2f terms=%d rms=%.2f\"\n",
		info.Azimuth, info.Altitude, info.PolarError, info.PositionAngle, info.OrthoError,
		info.AzTurns, info.AltTurns, info.Terms, info.RMSArcsec)
}

// All guarded: the no-arg form probes presence first so a mount without the
// peripheral prints "not present" instead of a wall of timeouts.

func focuserCmd(m *tenmicron.Mount, args []string) {
	if len(args) == 0 {
		max, err := m.FocuserMaxIndex()
		if err != nil {
			fmt.Printf("err: %v\n", err)
			return
		}
		fmt.Printf("focuser max index: %d\n", max)
		for n := 1; n <= max; n++ {
			f := m.Focuser(n)
			if av, _ := f.Available(); !av {
				fmt.Printf("  [%d] not present\n", n)
				continue
			}
			info, _ := f.Info()
			pos, _ := f.Position()
			st, _ := f.Status()
			fmt.Printf("  [%d] %q pos=%dµm status=%d\n", n, info.Name, pos, int(st))
		}
		return
	}
	n, err := strconv.Atoi(args[0])
	if err != nil {
		fmt.Println("usage: focuser <n> <status|info|goto|move|stop|home|maxspeed|range> ...")
		return
	}
	f := m.Focuser(n)
	sub, rest := subArgs(args)
	switch sub {
	case "", "status":
		fmt.Printf("── focuser %d ──\n", n)
		runProbes([]probe{
			{"available", fB(f.Available)},
			{"positionValid", fB(f.PositionValid)},
			{"position", fI(f.Position)},
			{"destination", fI(f.Destination)},
			{"moving", fB(f.Moving)},
			{"status", fI2(func() (int, error) { v, e := f.Status(); return int(v), e })},
			{"temperature", fF(f.Temperature)},
			{"maxSpeed", fI(f.MaxSpeed)},
			{"maxSpeedRange", fPair(f.MaxSpeedRange)},
			{"range", fPair(f.Range)},
			{"homingStatus", fI2(func() (int, error) { v, e := f.HomingStatus(); return int(v), e })},
			{"info", func() (string, error) {
				i, e := f.Info()
				return fmt.Sprintf("%q / %q / %q", i.Name, i.Type, i.Serial), e
			}},
		})
	case "info":
		i, err := f.Info()
		if err != nil {
			fmt.Printf("err: %v\n", err)
			return
		}
		fmt.Printf("name=%q type=%q serial=%q\n", i.Name, i.Type, i.Serial)
	case "goto":
		if v, ok := pI(rest, "focuser <n> goto <microns>"); ok {
			ok2, err := f.SetDestination(v)
			if err != nil {
				fmt.Printf("err: %v\n", err)
				return
			}
			if !ok2 {
				fmt.Println("rejected: destination out of range")
				return
			}
			reportOK(f.StartMove())
		}
	case "move":
		if v, ok := pI(rest, "focuser <n> move <±µm/s>"); ok {
			reportOK(f.MoveAtSpeed(v))
		}
	case "stop":
		reportErr(f.Stop())
	case "home":
		reportOK(f.StartHoming())
	case "maxspeed":
		if v, ok := pI(rest, "focuser <n> maxspeed <µm/s>"); ok {
			reportOK(f.SetMaxSpeed(v))
		}
	case "range":
		if lo, hi, ok := pI2(rest, "focuser <n> range <min> <max>"); ok {
			reportOK(f.SetRange(lo, hi))
		}
	default:
		fmt.Println("usage: focuser <n> <status|info|goto|move|stop|home|maxspeed|range> ...")
	}
}

// focuser1Cmd drives the legacy focuser-1 motion commands (:F…#).
func focuser1Cmd(m *tenmicron.Mount, args []string) {
	if len(args) == 0 {
		fmt.Println("usage: focuser1 in|out|fast|slow|halt|speed <1-4>")
		return
	}
	switch strings.ToLower(args[0]) {
	case "in":
		reportErr(m.Focuser1In())
	case "out":
		reportErr(m.Focuser1Out())
	case "fast":
		reportErr(m.Focuser1SpeedFast())
	case "slow":
		reportErr(m.Focuser1SpeedSlow())
	case "halt", "stop":
		reportErr(m.Focuser1Halt())
	case "speed":
		if n, ok := pI(args[1:], "focuser1 speed <1-4>"); ok {
			reportErr(m.Focuser1Speed(n))
		}
	default:
		fmt.Println("usage: focuser1 in|out|fast|slow|halt|speed <1-4>")
	}
}

func rotatorCmd(m *tenmicron.Mount, args []string) {
	if len(args) == 0 {
		max, err := m.RotatorMaxIndex()
		if err != nil {
			fmt.Printf("err: %v\n", err)
			return
		}
		fmt.Printf("rotator max index: %d\n", max)
		for n := 1; n <= max; n++ {
			r := m.Rotator(n)
			if av, _ := r.Available(); !av {
				fmt.Printf("  [%d] not present\n", n)
				continue
			}
			info, _ := r.Info()
			ang, _ := r.Angle(tenmicron.RotatorEquatorial)
			st, _ := r.Status()
			fmt.Printf("  [%d] %q equAngle=%.4f° status=%d\n", n, info.Name, ang, st)
		}
		return
	}
	n, err := strconv.Atoi(args[0])
	if err != nil {
		fmt.Println("usage: rotator <n> <status|goto|stop|home|zero|offset|maxspeed> ...")
		return
	}
	r := m.Rotator(n)
	sub, rest := subArgs(args)
	switch sub {
	case "", "status":
		fmt.Printf("── rotator %d ──\n", n)
		runProbes([]probe{
			{"available", fB(r.Available)},
			{"positionValid", fB(r.PositionValid)},
			{"angleMech", fF(func() (float64, error) { return r.Angle(tenmicron.RotatorMechanical) })},
			{"angleEqu", fF(func() (float64, error) { return r.Angle(tenmicron.RotatorEquatorial) })},
			{"angleOpt", fF(func() (float64, error) { return r.Angle(tenmicron.RotatorOptical) })},
			{"destEqu", fF(func() (float64, error) { return r.Destination(tenmicron.RotatorEquatorial) })},
			{"moving", fB(r.Moving)},
			{"status", fI(r.Status)},
			{"offset", fF(r.Offset)},
			{"maxSpeed", fI(r.MaxSpeed)},
			{"maxSpeedRange", fPair(r.MaxSpeedRange)},
			{"homingStatus", fI2(func() (int, error) { v, e := r.HomingStatus(); return int(v), e })},
			{"info", func() (string, error) {
				i, e := r.Info()
				return fmt.Sprintf("%q / %q / %q", i.Name, i.Type, i.Serial), e
			}},
		})
	case "goto":
		if len(rest) != 2 {
			fmt.Println("usage: rotator <n> goto mech|equ|opt <deg>")
			return
		}
		fr, ok := rotFrame(rest[0])
		if !ok {
			fmt.Println("bad frame (mech|equ|opt)")
			return
		}
		deg, err := strconv.ParseFloat(rest[1], 64)
		if err != nil {
			fmt.Printf("err: %v\n", err)
			return
		}
		reportOK(r.SetDestination(fr, deg))
	case "stop":
		reportErr(r.Stop())
	case "home":
		reportOK(r.StartHoming())
	case "zero":
		if len(rest) != 1 {
			fmt.Println("usage: rotator <n> zero mech|equ")
			return
		}
		switch strings.ToLower(rest[0]) {
		case "mech", "m":
			reportOK(r.ZeroMechanical())
		case "equ", "eq", "r":
			reportOK(r.ZeroEquatorial())
		default:
			fmt.Println("usage: rotator <n> zero mech|equ")
		}
	case "offset":
		if v, ok := pF(rest, "rotator <n> offset <deg>"); ok {
			reportOK(r.SetOffset(v))
		}
	case "maxspeed":
		if v, ok := pI(rest, "rotator <n> maxspeed <deg/s>"); ok {
			reportOK(r.SetMaxSpeed(v))
		}
	default:
		fmt.Println("usage: rotator <n> <status|goto|stop|home|zero|offset|maxspeed> ...")
	}
}

func domeCmd(m *tenmicron.Mount, args []string) {
	if len(args) == 0 {
		runProbes(domeProbes(m))
		return
	}
	sub, rest := args[0], args[1:]
	switch strings.ToLower(sub) {
	case "flap":
		if open, ok := openClose(rest); ok {
			reportOK(m.CommandDomeFlap(open))
		}
	case "shutter":
		if open, ok := openClose(rest); ok {
			reportOK(m.CommandDomeShutter(open))
		}
	case "slew":
		if v, ok := pF(rest, "dome slew <az>"); ok {
			reportOK(m.SlewDomeToAzimuth(v))
		}
	case "release":
		reportErr(m.ReleaseDomeControl())
	case "home":
		reportOK(m.StartDomeHoming())
	case "control":
		if len(rest) != 1 {
			fmt.Println("usage: dome control disconnect|rs232|gps")
			return
		}
		var mode tenmicron.DomeControl
		switch strings.ToLower(rest[0]) {
		case "disconnect", "off":
			mode = tenmicron.DomeDisconnect
		case "rs232":
			mode = tenmicron.DomeOnRS232
		case "gps":
			mode = tenmicron.DomeOnGPS
		default:
			fmt.Println("usage: dome control disconnect|rs232|gps")
			return
		}
		reportOK(m.SetDomeControl(mode))
	case "settle":
		if s, ok := pF(rest, "dome settle <sec>"); ok {
			reportErr(m.SetDomeSettleTime(time.Duration(s * float64(time.Second))))
		}
	case "radius":
		if v, ok := pI(rest, "dome radius <mm>"); ok {
			reportErr(m.SetDomeRadius(v))
		}
	case "update":
		if v, ok := pI(rest, "dome update <sec>"); ok {
			reportErr(m.SetDomeUpdateInterval(v))
		}
	case "mounttype":
		if v, ok := pI(rest, "dome mounttype <1|2>"); ok {
			reportErr(m.SetDomeMountType(v))
		}
	default:
		fmt.Println("usage: dome [flap|shutter open|close | slew <az> | release | home | control … | settle/radius/update/mounttype …]")
	}
}

func satCmd(m *tenmicron.Mount, args []string) {
	if len(args) == 0 {
		runProbes([]probe{
			{"loadedTLE", fS(m.LoadedTLE)},
			{"dbCount", fI(m.DatabaseTLECount)},
			{"transitSlew", fI2(func() (int, error) { v, e := m.TransitSlewStatus(); return int(v), e })},
		})
		return
	}
	sub, rest := args[0], args[1:]
	switch strings.ToLower(sub) {
	case "load": // sat load <two-line elements, verbatim incl. $0A newline escapes>
		if len(rest) == 0 {
			fmt.Println("usage: sat load <tle text>")
			return
		}
		reportErr(m.LoadTLE(strings.Join(rest, " ")))
	case "dbcount":
		reportI(m.DatabaseTLECount())
	case "dbload":
		if n, ok := pI(rest, "sat dbload <n>"); ok {
			report(m.LoadDatabaseTLE(n))
		}
	case "eq":
		if jd, ok := pF(rest, "sat eq <julianDate>"); ok {
			ra, dec, err := m.SatelliteEquatorial(jd)
			if err != nil {
				fmt.Printf("err: %v\n", err)
				return
			}
			fmt.Printf("RA=%.5f h  Dec=%.5f°\n", ra, dec)
		}
	case "az":
		if jd, ok := pF(rest, "sat az <julianDate>"); ok {
			alt, az, err := m.SatelliteHorizontal(jd)
			if err != nil {
				fmt.Printf("err: %v\n", err)
				return
			}
			fmt.Printf("Alt=%.5f°  Az=%.5f°\n", alt, az)
		}
	case "precalc":
		if len(rest) != 2 {
			fmt.Println("usage: sat precalc <jd> <minutes>")
			return
		}
		jd, e1 := strconv.ParseFloat(rest[0], 64)
		min, e2 := strconv.Atoi(rest[1])
		if e1 != nil || e2 != nil {
			fmt.Println("err: jd float, minutes int")
			return
		}
		if tr, err := m.PrecalcTransit(jd, min); err != nil {
			fmt.Printf("err: %v\n", err)
		} else {
			fmt.Printf("transit: JD %.6f → %.6f  flip=%v\n", tr.JDStart, tr.JDEnd, tr.Flip)
		}
	case "replay":
		if tr, err := m.ReplayTrajectory(); err != nil {
			fmt.Printf("err: %v\n", err)
		} else {
			fmt.Printf("trajectory: JD %.6f → %.6f  flip=%v\n", tr.JDStart, tr.JDEnd, tr.Flip)
		}
	case "slew":
		reportErr(m.SlewToTransit())
	case "status":
		v, err := m.TransitSlewStatus()
		reportI(int(v), err)
	case "offget":
		if id, ok := pI(rest, "sat offget <id 1-4>"); ok {
			reportF(m.TrajectoryOffsetValue(tenmicron.TrajectoryOffset(id)))
		}
	case "offset", "offadd":
		if len(rest) != 2 {
			fmt.Printf("usage: sat %s <id 1-4> <value>\n", sub)
			return
		}
		id, e1 := strconv.Atoi(rest[0])
		val, e2 := strconv.ParseFloat(rest[1], 64)
		if e1 != nil || e2 != nil {
			fmt.Println("err: id int, value float")
			return
		}
		if strings.ToLower(sub) == "offadd" {
			reportErr(m.AddTrajectoryOffset(tenmicron.TrajectoryOffset(id), val))
		} else {
			reportErr(m.SetTrajectoryOffset(tenmicron.TrajectoryOffset(id), val))
		}
	case "offclear":
		reportErr(m.ClearTrajectoryOffsets())
	default:
		fmt.Println("usage: sat [load <tle> | dbcount | dbload <n> | eq/az <jd> | precalc <jd> <min> | replay | slew | status | offget/offset/offadd/offclear …]")
	}
}

func clockProbes(m *tenmicron.Mount) []probe {
	return []probe{
		{"siteLat", fF(m.SiteLatitude)},
		{"siteLon", fF(m.SiteLongitude)},
		{"siteElev", fF(m.SiteElevation)},
		{"utcOffset", fD(m.UTCOffset)},
		{"localTime", fD(m.LocalTime)},
		{"localDate", fT(m.LocalDate)},
		{"utcDateTime", fT(m.UTCDateTime)},
		{"localDateTime", fT(m.LocalDateTime)},
		{"julianDate", fF(m.JulianDate)},
		{"ut1MinusUTC", fF(m.UT1MinusUTC)},
		{"gpsSynced", fB(m.GPSSynced)},
		{"gpsSyncState", fI2(func() (int, error) { v, e := m.GPSSyncState(); return int(v), e })},
		{"gpsNMEA", fS(m.GPSNMEA)},
		{"gpsMinusUTC", fI(m.GPSMinusUTC)},
		{"gpsWeekRollover", fI(m.GPSWeekRollover)},
		{"nextLeapSecond", func() (string, error) {
			d, ok, e := m.NextLeapSecond()
			if !ok || e != nil {
				return "none scheduled", e
			}
			return d.Format("2006-01-02"), nil
		}},
		{"deltaTStatus", func() (string, error) {
			v, exp, e := m.DeltaTStatus()
			return fmt.Sprintf("valid=%v expiry=%s", v, exp.Format("2006-01-02")), e
		}},
	}
}

func limitProbes(m *tenmicron.Mount) []probe {
	return []probe{
		{"highAltLimit", fF(m.HighAltitudeLimit)},
		{"lowAltLimit", fF(m.LowAltitudeLimit)},
		{"meridianTrackLim", fI(m.MeridianTrackLimit)},
		{"meridianSlewLim", fI(m.MeridianSlewLimit)},
		{"meridianSide", fI2(func() (int, error) { v, e := m.MeridianSideBehaviour(); return int(v), e })},
		{"unattendedFlip", fB(m.UnattendedFlip)},
		{"slewSettleTime", fD(m.SlewSettleTime)},
		{"timeToTrackEnd", fD(m.TimeToTrackingEnd)},
		{"targetTrackable", fB(m.TargetTrackable)},
	}
}

func rateProbes(m *tenmicron.Mount) []probe {
	return []probe{
		{"slewRate", fF(m.SlewRate)},
		{"minSlewRate", fF(m.MinSlewRate)},
		{"maxSlewRate", fF(m.MaxSlewRate)},
		{"guideRate\"/s", fF(m.GuideRate)},
		{"guideRate×sid", fF(m.GuideRateSidereal)},
		{"guiderPortOn", fB(m.GuiderPortEnabled)},
		{"guidingState", fI(m.GuidingState)},
		{"trackingRateHz", fF(m.TrackingRateHz)},
	}
}

func refractionProbes(m *tenmicron.Mount) []probe {
	return []probe{
		{"refraction", func() (string, error) {
			p, t, e := m.Refraction()
			return fmt.Sprintf("%.1f hPa / %.1f °C", p, t), e
		}},
		{"refractionCorr", fB(m.RefractionCorrection)},
		{"speedCorrection", fB(m.SpeedCorrection)},
	}
}

func targetProbes(m *tenmicron.Mount) []probe {
	return []probe{
		{"targetRA", fF(m.TargetRA)},
		{"targetDec", fF(m.TargetDec)},
		{"targetAlt", fF(m.TargetAltitude)},
		{"targetAz", fF(m.TargetAzimuth)},
		{"targetAxisPri", fF(m.TargetAxisAnglePrimary)},
		{"targetAxisSec", fF(m.TargetAxisAngleSecondary)},
	}
}

func networkProbes(m *tenmicron.Mount) []probe {
	return []probe{
		{"connectionType", fI(m.ConnectionType)},
		{"wiredNetwork", fNet(m.WiredNetwork)},
		{"wirelessNetwork", fNet(m.WirelessNetwork)},
		{"wirelessAvail", fB(m.WirelessAvailable)},
		{"wirelessESSID", fS(m.WirelessESSID)},
		{"wirelessStatus", fS(m.WirelessStatus)},
		{"ethernetMAC", fS(m.EthernetMAC)},
		{"wirelessMAC", fS(m.WirelessMAC)},
		{"webInterface", fB(m.WebInterfaceActive)},
		{"wakeOnLAN", fI2(func() (int, error) { v, e := m.WakeOnLANState(); return int(v), e })},
		{"discoveryService", func() (string, error) {
			ds, e := m.DiscoveryService()
			return fmt.Sprintf("available=%v active=%v name=%q", ds.Available, ds.Active, ds.Name), e
		}},
		{"wirelessAPs", func() (string, error) {
			aps, e := m.WirelessAccessPoints()
			return strings.Join(aps, ", "), e
		}},
	}
}

func domeProbes(m *tenmicron.Mount) []probe {
	return []probe{
		{"domeAzimuth", fF(m.DomeAzimuth)},
		{"domeFlap", fI2(func() (int, error) { v, e := m.DomeFlap(); return int(v), e })},
		{"domeShutter", fI2(func() (int, error) { v, e := m.DomeShutter(); return int(v), e })},
		{"domeHoming", fB(m.DomeHoming)},
		{"domeSlewing", fB(m.DomeSlewing)},
		{"domeSlewingExt", fB(m.DomeSlewingExternal)},
		{"domeSettleTime", fD(m.DomeSettleTime)},
	}
}

// dump prints read-only, no-argument queries and their results.
func dump(m *tenmicron.Mount) {
	groups := []struct {
		title  string
		probes []probe
	}{
		{"identity", []probe{
			{"firmware", fS(m.Firmware)},
			{"firmwareDate", fT(m.FirmwareDate)},
			{"firmwareTime", fS(m.FirmwareTime)},
			{"product", fS(m.Product)},
			{"hardwareID", fS(m.HardwareID)},
			{"controlBox", fS(m.ControlBoxVersion)},
			{"emulatedRev", fS(m.EmulatedFirmwareRev)},
			{"connectionType", fI(m.ConnectionType)},
			{"autoPowerOn", fI2(func() (int, error) { v, e := m.AutoPowerOnState(); return int(v), e })},
		}},
		{"position", []probe{
			{"ra", fF(m.RA)},
			{"dec", fF(m.Dec)},
			{"alt", fF(m.Altitude)},
			{"az", fF(m.Azimuth)},
			{"siderealTime", fF(m.SiderealTime)},
			{"pierSide", fS(func() (string, error) { return pierStr(m.PierSide()) })},
			{"pointingState", fS(func() (string, error) { return pierStr(m.PointingState()) })},
			{"destSideOfPier", fS(func() (string, error) { return pierStr(m.DestinationSideOfPier()) })},
			{"axisAnglePri", fF(m.AxisAnglePrimary)},
			{"axisAngleSec", fF(m.AxisAngleSecondary)},
		}},
		{"status flags", []probe{
			{"statusCode", fI(m.StatusCode)},
			{"slewing", fB(m.Slewing)},
			{"slewInProgress", fB(m.SlewInProgress)},
			{"tracking", fB(m.Tracking)},
			{"trackingActive", fB(m.TrackingActive)},
			{"dualAxisTracking", fB(m.DualAxisTracking)},
			{"targetTrackable", fB(m.TargetTrackable)},
			{"atPark", fB(m.AtPark)},
			{"atHome", fB(m.AtHome)},
			{"homeStatus", fI2(func() (int, error) { v, e := m.HomeStatus(); return int(v), e })},
		}},
		{"target register", targetProbes(m)},
		{"site / clock", clockProbes(m)},
		{"limits", limitProbes(m)},
		{"rates / guiding", rateProbes(m)},
		{"refraction", refractionProbes(m)},
		{"parallactic", []probe{
			{"parallacticAngle", fF(m.ParallacticAngle)},
			{"parallacticSpeed", fF(m.ParallacticSpeed)},
			{"parallacticAngZ", fF(m.ParallacticAngleZenith)},
			{"parallacticSpdZ", fF(m.ParallacticSpeedZenith)},
		}},
		{"model", []probe{
			{"alignmentStars", fI(m.AlignmentStarCount)},
			{"maxAlignStars", fI(m.MaxAlignmentStars)},
			{"savedModelCount", fI(m.SavedModelCount)},
		}},
		{"final approach", []probe{
			{"faMode", fI2(func() (int, error) { v, e := m.FinalApproachMode(); return int(v), e })},
			{"faTimeConstant", fF(m.FinalApproachTimeConstant)},
			{"faDistanceLimit", fF(m.FinalApproachDistanceLimit)},
		}},
		{"dome", domeProbes(m)},
		{"peripherals", []probe{
			{"focuserMaxIndex", fI(m.FocuserMaxIndex)},
			{"rotatorMaxIndex", fI(m.RotatorMaxIndex)},
			{"lowTempLimited", fB(m.LowTemperatureLimited)},
			{"elecBoxTemp°C", func() (string, error) {
				v, e := m.Temperature(tenmicron.TempElectronicsBox)
				if errors.Is(e, tenmicron.ErrTemperatureUnavailable) {
					return "unavailable", nil
				}
				return fmt.Sprintf("%.1f", v), e
			}},
		}},
		{"weather station", []probe{
			{"pressure", fWeather(m.WeatherPressure)},
			{"temperature", fWeather(m.WeatherTemperature)},
			{"humidity", fWeather(m.WeatherHumidity)},
			{"dewPoint", fWeather(m.WeatherDewPoint)},
			{"autoUpdateMode", fI2(func() (int, error) { v, e := m.WeatherAutoUpdateMode(); return int(v), e })},
		}},
		{"network", networkProbes(m)},
		{"satellite / dither", []probe{
			{"tleDatabaseCount", fI(m.DatabaseTLECount)},
			{"loadedTLE", fS(m.LoadedTLE)},
			{"transitSlewState", fI2(func() (int, error) { v, e := m.TransitSlewStatus(); return int(v), e })},
			{"ditheringActive", fB(m.DitheringActive)},
			{"ditherParams", func() (string, error) {
				p, e := m.DitherParameters()
				return fmt.Sprintf("ra=%.1f\" dec=%.1f\" delay=%.0fs exp=%.0fs interval=%.0fs",
					p.RAArcsec, p.DecArcsec, p.DelaySec, p.ExposureSec, p.IntervalSec), e
			}},
		}},
	}
	if m.MountClass().DDS { // DDS-only; a GM/AZ mount just times out on these
		groups = append(groups, struct {
			title  string
			probes []probe
		}{"alarms", []probe{
			{"activeAlarms", fAlarms(m.ActiveAlarms)},
			{"unackedAlarms", fAlarms(m.UnacknowledgedAlarms)},
		}})
	}
	for _, g := range groups {
		fmt.Printf("── %s ──\n", g.title)
		runProbes(g.probes)
	}
}

type probe struct {
	name string
	fn   func() (string, error)
}

func runProbes(probes []probe) {
	for _, p := range probes {
		v, err := p.fn()
		if err != nil {
			fmt.Printf("  %-18s err: %v\n", p.name, err)
			continue
		}
		fmt.Printf("  %-18s %s\n", p.name, v)
	}
}

// f* adapt a typed getter to a probe's string-returning func.
func fF(fn func() (float64, error)) func() (string, error) {
	return func() (string, error) { v, err := fn(); return fmt.Sprintf("%.6f", v), err }
}
func fI(fn func() (int, error)) func() (string, error) {
	return func() (string, error) { v, err := fn(); return strconv.Itoa(v), err }
}
func fI2(fn func() (int, error)) func() (string, error) { return fI(fn) } // enum → int via wrapper
func fB(fn func() (bool, error)) func() (string, error) {
	return func() (string, error) { v, err := fn(); return fmt.Sprintf("%v", v), err }
}
func fD(fn func() (time.Duration, error)) func() (string, error) {
	return func() (string, error) { v, err := fn(); return v.String(), err }
}
func fT(fn func() (time.Time, error)) func() (string, error) {
	return func() (string, error) { v, err := fn(); return v.Format(time.RFC3339), err }
}
func fS(fn func() (string, error)) func() (string, error) {
	return func() (string, error) { v, err := fn(); return fmt.Sprintf("%q", v), err }
}
func fAlarms(fn func() ([]tenmicron.Alarm, error)) func() (string, error) {
	return func() (string, error) {
		list, err := fn()
		if len(list) == 0 {
			return "none", err
		}
		parts := make([]string, len(list))
		for i, a := range list {
			parts[i] = fmt.Sprintf("%d (%s)", int(a), a)
		}
		return strings.Join(parts, ", "), err
	}
}
func fWeather(fn func() (float64, time.Duration, error)) func() (string, error) {
	return func() (string, error) {
		v, age, err := fn()
		return fmt.Sprintf("%.1f (age %s)", v, age), err
	}
}
func fPair(fn func() (int, int, error)) func() (string, error) {
	return func() (string, error) {
		lo, hi, err := fn()
		return fmt.Sprintf("%d..%d", lo, hi), err
	}
}
func fNet(fn func() (tenmicron.NetworkConfig, error)) func() (string, error) {
	return func() (string, error) {
		nc, err := fn()
		return fmt.Sprintf("ip=%s mask=%s gw=%s flag=%s", nc.IP, nc.Netmask, nc.Gateway, nc.Flag), err
	}
}

func pierStr(p lx200.PierSide, err error) (string, error) { return fmt.Sprintf("%v", p), err }

func report(s string, err error) {
	if err != nil {
		fmt.Printf("err: %v\n", err)
		return
	}
	fmt.Printf("reply: %q\n", s)
}

func reportS(s string, err error) {
	if err != nil {
		fmt.Printf("err: %v\n", err)
		return
	}
	fmt.Printf("value: %s\n", s)
}

func reportF(v float64, err error) {
	if err != nil {
		fmt.Printf("err: %v\n", err)
		return
	}
	fmt.Printf("value: %.6f\n", v)
}

func reportI(v int, err error) {
	if err != nil {
		fmt.Printf("err: %v\n", err)
		return
	}
	fmt.Printf("value: %d\n", v)
}

func reportB(v bool, err error) {
	if err != nil {
		fmt.Printf("err: %v\n", err)
		return
	}
	fmt.Printf("value: %v\n", v)
}

func reportT(v time.Time, err error) {
	if err != nil {
		fmt.Printf("err: %v\n", err)
		return
	}
	fmt.Printf("value: %s\n", v.Format(time.RFC3339))
}

func reportOK(ok bool, err error) {
	if err != nil {
		fmt.Printf("err: %v\n", err)
		return
	}
	fmt.Printf("accepted: %v\n", ok)
}

func reportErr(err error) {
	if err != nil {
		fmt.Printf("err: %v\n", err)
		return
	}
	fmt.Println("ok")
}

func help() {
	fmt.Print(`raw protocol (type the full frame, e.g. :GVN#):
  g :CMD#   a :CMD#   b :CMD#   w [ms]        query / ack-byte / blind / await
  :CMD#     shorthand for 'g :CMD#'

reads — position / status:
  ra dec alt az lst  pier pointing destpier
  slewing tracking trackactive dualaxis athome atpark statuscode slewprog trackable
  ginfo                decode a fresh :Ginfo#
  dump                 read-only sweep of EVERY no-arg getter (the validation pass)

reads — grouped:
  clock  limits  rates  refraction  target  network  dome  model  mountcfg
  alarms               DDS alarm lists (fw ≥ 3.4); alarmack <id> to acknowledge
  version fwdate fwtime product hwid controlbox  guiderate  temp <sensor#>

target setters (round-trip the coordinate encoders):
  sr <hours>  sd <deg>  sa <alt>  sz <az>

site / time setters:
  setsitelat <deg>  setsitelon <deg,E+>  setsiteelev <m>
  setutcoffset <hours>  setutc [RFC3339]  setclock [RFC3339]  setjd <jd>
  nudgetime <±ms>  updategps

limit setters:
  sethighalt <deg>  setlowalt <deg>  setmertrack <deg>  setmerslew <deg>
  setmerside both|west|east  setsettle <sec>  setunattended on|off

rate setters:
  setmaxslew <deg/s>  setautoslew <deg/s>  setguideidx <0-2>  setcenteridx <0-3>
  setslewidx <0-2>  setcenter <1-255>  setslew <1-1200>
  setguiderate <arcsec/s>  setguideport on|off

precision:   precision low|high|ultra|toggle
refraction:  setrefraction <hPa> <tempC>  setpressure <hPa>  settemp <°C>
             refcorr on|off  speedcorr on|off
backlash:    setdecbacklash <arcsec>  setrabacklash <arcsec>
tracking:    track on|off  sidereal solar lunar  trackrate lunar|solar|sidereal|stop
             setdualaxis on|off  setrarate <×sid>  setdecrate <×sid>  settrackarcsec <"/s>

goto / sync (motion commands wait for the slew to finish):
  slew  slewfine  sync  synccurrent  altaz <az> <alt>  flip  nudge <raAz"> <decAlt">
  home  park  unpark  stop  halt
  raaxis                   go to the RA-axis reference (RA axis 90°, Dec axis 0°) and stop
  raaxis <deg>             rotate the RA axis to an exact mechanical angle (Dec unchanged)

manual motion:
  move n|s|e|w  rate g|c|f|s  moveaxis pri|sec +|- <deg/s>  stopaxis pri|sec  pulse n|s|e|w <ms>

peripherals (no-arg form probes presence first):
  focuser [<n> status|info|goto <µm>|move <±µm/s>|stop|home|maxspeed <µm/s>|range <lo> <hi>]
  focuser1 in|out|fast|slow|halt|speed <1-4>
  rotator [<n> status|goto mech|equ|opt <deg>|stop|home|zero mech|equ|offset <deg>|maxspeed <deg/s>]
  dome  [flap|shutter open|close | slew <az> | release | home | control disconnect|rs232|gps
         | settle <sec> | radius <mm> | update <sec> | mounttype <1|2>]
  sat   [load <tle> | dbcount | dbload <n> | eq|az <jd> | precalc <jd> <min>
         | replay | slew | status | offget <id> | offset|offadd <id> <val> | offclear]

  help   quit
`)
}
