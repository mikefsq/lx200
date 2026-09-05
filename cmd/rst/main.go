// Command rst provides an interactive console for Rainbow Astro RST mounts.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/mikefsq/lx200"
	"github.com/mikefsq/lx200/rst"
)

func main() {
	serialPort := flag.String("serial", "", "serial port; empty = auto-detect the first RST (FTDI 0403:6001)")
	timeout := flag.Duration("timeout", 3*time.Second, "reply timeout for raw queries/awaits")
	flag.Parse()

	var (
		m   *rst.Mount
		err error
	)
	if *serialPort != "" {
		m, err = rst.Open(*serialPort)
	} else {
		m, err = rst.Find()
	}
	if err != nil {
		log.Fatalf("rst: open: %v", err)
	}
	defer m.Close()

	if v, err := m.Version(); err == nil {
		fmt.Printf("connected — firmware %q\n", v)
	} else {
		fmt.Printf("connected (version read failed: %v)\n", err)
	}

	// One-shot mode: everything after the flags is a single command line.
	if args := flag.Args(); len(args) > 0 {
		run(m, strings.Join(args, " "), *timeout)
		return
	}

	fmt.Println("type 'help' for commands, 'quit' to exit")
	sc := bufio.NewScanner(os.Stdin)
	fmt.Print("rst> ")
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch line {
		case "quit", "exit", "q":
			return
		case "":
		default:
			run(m, line, *timeout)
		}
		fmt.Print("rst> ")
	}
}

// run parses one console line and dispatches it. Raw verbs (g/a/b/w) send a
// literal frame and print the reply in the matching shape; named verbs call the
// typed rst.Mount API.
func run(m *rst.Mount, line string, timeout time.Duration) {
	fields := strings.Fields(line)
	verb := strings.ToLower(fields[0])
	args := fields[1:]

	switch verb {
	case "g", "get": // query: write, read until '#'
		if len(args) != 1 {
			fmt.Println("usage: g :CMD#")
			return
		}
		s, err := m.Get(args[0])
		report(s, err)
	case "a", "ack": // set: write, read one status byte
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
	case "b", "blind": // write, expect no reply
		if len(args) != 1 {
			fmt.Println("usage: b :CMD#")
			return
		}
		if err := m.Blind(args[0]); err != nil {
			fmt.Printf("err: %v\n", err)
			return
		}
		fmt.Println("sent (no reply expected)")
	case "w", "await": // read a pushed token (e.g. :MM0# / :CHO#)
		d := timeout
		if len(args) == 1 {
			if ms, err := strconv.Atoi(args[0]); err == nil {
				d = time.Duration(ms) * time.Millisecond
			}
		}
		s, err := m.Await(d)
		report(s, err)

	case "ra":
		reportF(m.RA())
	case "dec":
		reportF(m.Dec())
	case "alt":
		reportF(m.Altitude())
	case "az":
		reportF(m.Azimuth())
	case "version", "ver":
		report(m.Version())
	case "voltage", "volt":
		reportF(m.Voltage())
	case "slewing":
		reportB(m.Slewing())
	case "tracking":
		reportB(m.Tracking())
	case "trackmode":
		if tm, err := m.TrackMode(); err != nil {
			fmt.Printf("err: %v\n", err)
		} else {
			fmt.Printf("trackmode: %d\n", tm)
		}
	case "athome":
		reportB(m.AtHome())
	case "homefound":
		fmt.Printf("homeFound: %v\n", m.HomeFound())
	case "homepos":
		if ra, dec, az, alt, ok := m.HomePosition(); ok {
			fmt.Printf("home: RA=%.5f Dec=%.5f Az=%.5f Alt=%.5f\n", ra, dec, az, alt)
		} else {
			fmt.Println("home position not captured yet (run 'home')")
		}
	case "atpark":
		reportB(m.AtPark())
	case "pier":
		if ps, err := m.PierSide(); err != nil {
			fmt.Printf("err: %v\n", err)
		} else {
			fmt.Printf("pier: %v\n", ps)
		}
	case "status", "st":
		status(m)

	case "lst":
		reportF(m.SiderealTime())
	case "localtime":
		reportF(m.LocalTime())
	case "date":
		report(m.Date())
	case "utcoffset":
		reportF(m.UTCOffset())
	case "sitelon":
		reportF(m.SiteLongitude())
	case "sitename":
		if len(args) != 1 {
			fmt.Println("usage: sitename <1-3>  (:GP# is the precision mode, not a fourth name)")
			return
		}
		n, _ := strconv.Atoi(args[0])
		report(m.SiteName(n))
	case "sysstatus":
		if s, err := m.SystemStatus(); err != nil {
			fmt.Printf("err: %v\n", err)
		} else {
			fmt.Printf("tcs=%v decMotor=%v raMotor=%v (raw %q)\n", s.TCS, s.DecMotor, s.RAMotor, s.Raw)
		}
	case "motorload":
		if d, r, err := m.MotorLoad(); err != nil {
			fmt.Printf("err: %v\n", err)
		} else {
			fmt.Printf("motor load: dec=%.1f%% ra=%.1f%%\n", d, r)
		}
	case "autoresume":
		reportB(m.AutoResume())
	case "guiderate":
		reportF(m.GuideRate())
	case "setguiderate":
		if len(args) != 1 {
			fmt.Println("usage: setguiderate <xSidereal>")
			return
		}
		v, err := strconv.ParseFloat(args[0], 64)
		if err != nil {
			fmt.Printf("err: %v\n", err)
			return
		}
		reportErr(m.SetGuideRate(v))
	case "slewspeed":
		if len(args) != 1 {
			fmt.Println("usage: slewspeed <1-3>")
			return
		}
		n, _ := strconv.Atoi(args[0])
		if v, err := m.SlewSpeed(n); err != nil {
			fmt.Printf("err: %v\n", err)
		} else {
			fmt.Printf("slew speed %d: %d\n", n, v)
		}
	case "forceflip":
		if len(args) != 1 || (args[0] != "on" && args[0] != "off") {
			fmt.Println("usage: forceflip on|off")
			return
		}
		reportErr(m.SetForcePierFlip(args[0] == "on"))
	case "gps":
		gps(m)

	case "sr", "settargetra":
		if len(args) != 1 {
			fmt.Println("usage: sr <hours>")
			return
		}
		h, err := strconv.ParseFloat(args[0], 64)
		if err != nil {
			fmt.Printf("err: %v\n", err)
			return
		}
		ok, err := m.SetTargetRA(h)
		reportOK(ok, err)
	case "sd", "settargetdec":
		if len(args) != 1 {
			fmt.Println("usage: sd <deg>")
			return
		}
		d, err := strconv.ParseFloat(args[0], 64)
		if err != nil {
			fmt.Printf("err: %v\n", err)
			return
		}
		ok, err := m.SetTargetDec(d)
		reportOK(ok, err)
	case "slew":
		if err := m.SlewToTarget(); err != nil {
			fmt.Printf("err: %v\n", err)
			return
		}
		waitSlew(m)
	case "pole": // polar-alignment slew: point at the celestial pole (homed first)
		fmt.Println("slewing to celestial pole (Az 0, Alt=latitude)...")
		if err := m.SlewToPole(); err != nil {
			fmt.Printf("err: %v\n", err)
			return
		}
		waitSlew(m)
	case "altaz": // horizontal goto: altaz <az> <alt>
		if len(args) != 2 {
			fmt.Println("usage: altaz <az> <alt>")
			return
		}
		az, err1 := strconv.ParseFloat(args[0], 64)
		alt, err2 := strconv.ParseFloat(args[1], 64)
		if err1 != nil || err2 != nil {
			fmt.Println("err: az/alt must be numbers")
			return
		}
		if err := m.SlewToAltAz(az, alt); err != nil {
			fmt.Printf("err: %v\n", err)
			return
		}
		waitSlew(m)
	case "sitelat":
		reportF(m.SiteLatitude())
	case "fault":
		if f := m.Fault(); f == "" {
			fmt.Println("fault: none (last move completed cleanly)")
		} else {
			fmt.Printf("fault: %q\n", f)
		}
	case "sync":
		_, err := m.SyncToTarget()
		reportErr(err)
	case "synccurrent": // sync to the mount's own current RA/Dec — should be a no-op
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
	case "addpt":
		reportErr(m.AddAlignmentPoint())
	case "home":
		fmt.Println("homing (seeking mechanical West-horizon reference)...")
		if err := m.FindHome(); err != nil {
			fmt.Printf("err: %v\n", err)
			return
		}
		waitSlew(m)
	case "park":
		fmt.Println("parking (equatorial goto to HA +6h / Dec +89*59' — RA axis to 0, tube on top)...")
		if err := m.Park(); err != nil {
			fmt.Printf("err: %v\n", err)
			return
		}
		waitSlew(m)
		// The mechanical angles are the check that matters: the sky position is the same
		// whatever the RA axis does, so only :CY# can tell a top-up park from the old
		// tube-on-the-left one. RA axis near 0 = landed; near -80 = it did not.
		if dec, ra, err := m.AxisAngles(); err != nil {
			fmt.Printf("  axes: %v\n", err)
		} else {
			fmt.Printf("  axes: DEC %.2f°  RA %.2f°   (want DEC ~90, RA ~0)\n", dec, ra)
		}
	case "unpark":
		reportErr(m.Unpark())
	case "halt", "stop":
		reportErr(m.Halt())

	case "track":
		if len(args) != 1 || (args[0] != "on" && args[0] != "off") {
			fmt.Println("usage: track on|off")
			return
		}
		reportErr(m.SetTracking(args[0] == "on"))
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
	case "pulse":
		pulse(m, args)

	case "serial":
		report(m.SerialNumber())
	case "model":
		report(m.ModelName())
	case "gear":
		ra, dec, err := m.GearRatio()
		reportPair(ra, dec, err)
	case "worm":
		ra, dec, err := m.WormCount()
		reportPair(ra, dec, err)
	case "precision":
		if len(args) == 1 {
			reportErr(m.SetPrecision(strings.EqualFold(args[0], "high") || args[0] == "H"))
			return
		}
		report(m.Precision())
	case "echo":
		if len(args) != 1 {
			fmt.Println("usage: echo on|off   (:SPE#/:SPF# — off breaks prefix matching)")
			return
		}
		reportErr(m.EchoPrefix(args[0] == "on"))
	case "clockformat":
		if len(args) == 1 {
			n, err := strconv.Atoi(args[0])
			if err != nil {
				fmt.Println("usage: clockformat [12|24]")
				return
			}
			reportErr(m.SetClockFormat(n))
			return
		}
		v, err := m.ClockFormat()
		reportF(float64(v), err)

	case "target":
		ra, err1 := m.TargetRA()
		dec, err2 := m.TargetDec()
		az, alt, err3 := m.TargetAltAz()
		if err := firstErr(err1, err2, err3); err != nil {
			fmt.Println("err:", err)
			return
		}
		fmt.Printf("  RA %.4f  Dec %.4f  Az %.3f  Alt %.3f\n", ra, dec, az, alt)
	case "axes":
		dec, ra, err := m.AxisAngles()
		if err != nil {
			fmt.Println("err:", err)
			return
		}
		fmt.Printf("  DEC axis %.2f°   RA axis %.2f°   (:CY#)\n", dec, ra)
	case "limits":
		v, err := m.SlewLimits()
		if err != nil {
			fmt.Println("err:", err)
			return
		}
		fmt.Printf("  %v\n", v)
	case "homing":
		report(boolStr(m.Homing()))
	case "polepos":
		az, alt, err := m.PolePosition()
		if err != nil {
			fmt.Println("err:", err)
			return
		}
		fmt.Printf("  Az %.3f  Alt %.3f\n", az, alt)

	case "setutc":
		reportErr(m.SetUTC(time.Now().UTC()))
	case "setlocaltime":
		reportErr(m.SetLocalTime(time.Now()))
	case "setutcoffset":
		if len(args) != 1 {
			fmt.Println("usage: setutcoffset <whole hours, e.g. -7>")
			return
		}
		h, err := strconv.Atoi(args[0])
		if err != nil {
			fmt.Println("usage: setutcoffset <whole hours>")
			return
		}
		reportErr(m.SetUTCOffset(time.Duration(h) * time.Hour))

	case "axisrate":
		if len(args) != 1 {
			fmt.Printf("usage: axisrate <deg/s>   vendor rates: %v\n", rst.AxisRates())
			return
		}
		v, err := strconv.ParseFloat(args[0], 64)
		if err != nil {
			fmt.Println("usage: axisrate <deg/s>")
			return
		}
		reportErr(m.SetAxisRate(v))
	case "setslewspeed":
		if len(args) != 2 {
			fmt.Println("usage: setslewspeed <1-3> <xSidereal>   (slot 3 is what :RS# and a goto use)")
			return
		}
		n, err1 := strconv.Atoi(args[0])
		v, err2 := strconv.Atoi(args[1])
		if err := firstErr(err1, err2); err != nil {
			fmt.Println("usage: setslewspeed <1-3> <xSidereal>")
			return
		}
		reportErr(m.SetSlewSpeed(n, v))

	case "messier", "ngc", "star":
		if len(args) != 1 {
			fmt.Printf("usage: %s <number>   (loads the object into the goto target)\n", verb)
			return
		}
		n, err := strconv.Atoi(args[0])
		if err != nil {
			fmt.Printf("usage: %s <number>\n", verb)
			return
		}
		switch verb {
		case "messier":
			reportErr(m.SelectMessier(n))
		case "ngc":
			reportErr(m.SelectNGC(n))
		default:
			reportErr(m.SelectStar(n))
		}
	case "siteslot":
		if len(args) == 1 {
			n, err := strconv.Atoi(args[0])
			if err != nil {
				fmt.Println("usage: siteslot [n]")
				return
			}
			reportErr(m.SelectSiteSlot(n))
			return
		}
		report(m.SiteSlot())

	case "alpaca":
		alpacaStatus(m)

	case "help", "?", "h":
		help()
	default:
		// Bare frame convenience: a line that looks like ":XXX#" is a query.
		if strings.HasPrefix(verb, ":") {
			s, err := m.Get(fields[0])
			report(s, err)
			return
		}
		fmt.Printf("unknown command %q (try 'help')\n", verb)
	}
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

func moveAxis(m *rst.Mount, args []string) {
	if len(args) != 3 {
		fmt.Println("usage: moveaxis pri|sec +|- g|c|f|s")
		return
	}
	var a lx200.Axis
	switch strings.ToLower(args[0]) {
	case "pri", "p", "0":
		a = lx200.AxisPrimary
	case "sec", "s", "1":
		a = lx200.AxisSecondary
	default:
		fmt.Printf("bad axis %q (pri|sec)\n", args[0])
		return
	}
	positive := args[1] != "-"
	r, ok := rate(args[2:])
	if !ok {
		return
	}
	reportErr(m.MoveAxis(a, positive, r))
}

func pulse(m *rst.Mount, args []string) {
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

func status(m *rst.Mount) {
	type row struct {
		name string
		fn   func() (string, error)
	}
	f := func(v float64, err error) (string, error) { return fmt.Sprintf("%.5f", v), err }
	b := func(v bool, err error) (string, error) { return fmt.Sprintf("%v", v), err }
	rows := []row{
		{"ra", func() (string, error) { return f(m.RA()) }},
		{"dec", func() (string, error) { return f(m.Dec()) }},
		{"alt", func() (string, error) { return f(m.Altitude()) }},
		{"az", func() (string, error) { return f(m.Azimuth()) }},
		{"slewing", func() (string, error) { return b(m.Slewing()) }},
		{"tracking", func() (string, error) { return b(m.Tracking()) }},
		{"athome", func() (string, error) { return b(m.AtHome()) }},
		{"atpark", func() (string, error) { return b(m.AtPark()) }},
	}
	for _, r := range rows {
		v, err := r.fn()
		if err != nil {
			fmt.Printf("  %-9s err: %v\n", r.name, err)
			continue
		}
		fmt.Printf("  %-9s %s\n", r.name, v)
	}
}

// gps dumps the GPS-fed position/clock the mount reports, plus site name.
func gps(m *rst.Mount) {
	pf := func(name string, v float64, err error) {
		if err != nil {
			fmt.Printf("  %-11s err: %v\n", name, err)
		} else {
			fmt.Printf("  %-11s %.5f\n", name, v)
		}
	}
	lat, err := m.SiteLatitude()
	pf("latitude", lat, err)
	lon, err := m.SiteLongitude()
	pf("longitude", lon, err)
	lt, err := m.LocalTime()
	pf("localtime", lt, err)
	st, err := m.SiderealTime()
	pf("sidereal", st, err)
	off, err := m.UTCOffset()
	pf("utcoffset", off, err)
	if d, err := m.Date(); err != nil {
		fmt.Printf("  %-11s err: %v\n", "date", err)
	} else {
		fmt.Printf("  %-11s %s\n", "date", d)
	}
	if s, err := m.SiteName(1); err == nil {
		fmt.Printf("  %-11s %q\n", "site", s)
	}
}

// waitSlew polls until the mount stops slewing (or a 3-min cap), then reports
// whether it reached the target or aborted on a fault (e.g. a movement limit).
func waitSlew(m *rst.Mount) {
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		sl, err := m.Slewing()
		if err != nil {
			fmt.Printf("err: %v\n", err)
			return
		}
		if !sl {
			if f := m.Fault(); f != "" {
				fmt.Printf("ABORTED at fault %q (e.g. movement limit) — check position\n", f)
			} else {
				az, _ := m.Azimuth()
				alt, _ := m.Altitude()
				fmt.Printf("done: Az=%.3f Alt=%.3f\n", az, alt)
			}
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	fmt.Println("timeout waiting for slew to finish")
}

func report(s string, err error) {
	if err != nil {
		fmt.Printf("err: %v\n", err)
		return
	}
	fmt.Printf("reply: %q\n", s)
}

func reportF(v float64, err error) {
	if err != nil {
		fmt.Printf("err: %v\n", err)
		return
	}
	fmt.Printf("value: %.6f\n", v)
}

func reportB(v bool, err error) {
	if err != nil {
		fmt.Printf("err: %v\n", err)
		return
	}
	fmt.Printf("value: %v\n", v)
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
	fmt.Print(`raw protocol (type the full frame, e.g. :GR#):
  g :CMD#            query — write, read until '#', print reply
  a :CMD#            set   — write, read one status byte ('1'/'0')
  b :CMD#            blind — write, expect no reply
  w [ms]             await a pushed token (:MM0# / :CHO#)
  :CMD#              shorthand for 'g :CMD#'

typed reads:
  ra dec alt az  version voltage
  slewing tracking trackmode athome atpark pier  status

gps / clock / site / telemetry:
  gps                dump position+clock     lst  localtime  date  utcoffset
  sitelat sitelon    sitename <1-3>   siteslot
  sysstatus          motorload  autoresume
  guiderate  setguiderate <x>  slewspeed <1-3>  forceflip on|off
  serial model gear worm            identity and factory calibration
  precision [high|low]  echo on|off clockformat [12|24]
  setutc  setlocaltime  setutcoffset <hours>

rates:
  axisrate <deg/s>   program the speed slot AND select it (what MoveAxis needs)
  setslewspeed <1-3> <xSidereal>    slot 3 is what :RS# and a goto use

targets, limits, catalogue:
  target             read back the goto target (RA/Dec and Az/Alt)
  axes               mechanical axis angles (:CY# — DEC axis / RA axis)
  limits             the six slew-limit registers (:CA#..:CF#)
  polepos            where Park sends the mount
  messier <n>  ngc <n>  star <n>    load a catalogue object into the target
  siteslot [n]       which stored site :Sg/:St write into

state:
  homing             is a home seek RUNNING (:AH#) — not "at home"
  alpaca             the ASCOM home/park contract vs the mount's own answers

target + goto:
  sr <hours>         set target RA
  sd <deg>           set target Dec
  slew               goto target      sync   re-center on target
  addpt              save align point  home   find home
  pole               polar-align: slew to celestial pole (must be homed first)
  altaz <az> <alt>   horizontal goto   sitelat  read mount latitude
  fault              last motion-abort token (limit/error), or none
  park  unpark  halt

tracking:
  track on|off  sidereal  solar  lunar

manual motion:
  move n|s|e|w       rate g|c|f|s
  moveaxis pri|sec +|- g|c|f|s
  pulse n|s|e|w <ms>

  help   quit
`)
}

// reportPair prints a two-field reply like :AG#'s gear ratio or :AP#'s worm count.
func reportPair(a, b int, err error) {
	if err != nil {
		fmt.Println("err:", err)
		return
	}
	fmt.Printf("  %d / %d\n", a, b)
}

// firstErr returns the first non-nil error, so a multi-read verb can fail once.
func firstErr(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}

// boolStr adapts a bool-returning read to report's (string, error) shape.
func boolStr(v bool, err error) (string, error) { return fmt.Sprint(v), err }

// AtHome reads the MECHANICAL axis angles (:CY#), not Az/Alt — see homeDecAxis in the driver.
// 'axes' prints the same numbers raw when this disagrees with what you expect.
func alpacaStatus(m *rst.Mount) {
	ah, err1 := m.AlpacaAtHome()
	ap, err2 := m.AlpacaAtPark()
	rawHome, err3 := m.AtHome()
	rawPark, err4 := m.AtPark()
	if err := firstErr(err1, err2, err3, err4); err != nil {
		fmt.Println("err:", err)
		return
	}
	fmt.Printf("  AlpacaAtHome %-5v                        AtHome %-5v  (homed + :CY# axes at 0/0)\n", ah, rawHome)
	fmt.Printf("  AlpacaAtPark %-5v                        AtPark %-5v\n", ap, rawPark)
	fmt.Printf("  HomeFound    %-5v  (homed since power-on)\n", m.HomeFound())
	fmt.Printf("  can: findhome=%v park=%v unpark=%v setpark=%v\n",
		m.AlpacaCanFindHome(), m.AlpacaCanPark(), m.AlpacaCanUnpark(), m.AlpacaCanSetPark())
}
