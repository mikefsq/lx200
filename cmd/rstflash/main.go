// Command rstflash writes firmware to a Rainbow Astro RST mount over the
// Microchip AN1388 PIC32 bootloader.
//
// The controller enters its bootloader when it is powered on with the PREV
// and NEXT buttons held. rstflash polls for the bootloader on connect.
//
// Usage:
//
//	rstflash -check RST-135E_260319.hex     # parse the file, touch no hardware
//	rstflash -info                          # connect and report the bootloader version
//	rstflash RST-135E_260319.hex            # flash, with a confirmation prompt
//	rstflash -y -serial /dev/cu.usbserial-X RST-135E_260319.hex
//
package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mikefsq/lx200/rst/boot"
	"github.com/mikefsq/lx200/serial"
)

func main() {
	log.SetFlags(0)
	log.SetPrefix("rstflash: ")

	var (
		portName = flag.String("serial", "", "serial port; empty = auto-detect the first FTDI adapter")
		baud     = flag.Int("baud", boot.Baud, "bootloader baud rate")
		wait     = flag.Duration("wait", 60*time.Second, "how long to wait for the bootloader to answer")
		check    = flag.Bool("check", false, "parse and summarise the hex file, then exit; opens no port")
		info     = flag.Bool("info", false, "connect and report the bootloader version, then exit; writes nothing")
		crc      = flag.Bool("crc", false, "checksum the firmware already on the device and exit; writes nothing")
		identify = flag.Bool("identify", false, "identify the firmware on the device against the given hex files, then exit; writes nothing")
		rng      = flag.String("range", "", "with -crc, checksum ADDR:LEN (e.g. 0x9D000000:4096) instead of the hex file's range")
		run      = flag.Bool("run", false, "send JUMP_TO_APP after a successful verify instead of asking for a power cycle")
		yes      = flag.Bool("y", false, "skip the confirmation prompt")
	)
	flag.Parse()

	args := flag.Args()
	if *info {
		doInfo(*portName, *baud, *wait)
		return
	}
	if *identify {
		if err := doIdentify(*portName, *baud, *wait, args); err != nil {
			log.Fatalf("%v", err)
		}
		return
	}

	// -crc with an explicit range needs no hex file.
	var img *boot.Image
	var err error
	if len(args) == 1 {
		if img, err = boot.LoadHexFile(args[0]); err != nil {
			log.Fatalf("%v", err)
		}
		fmt.Printf("%s\n", args[0])
		fmt.Printf("  %d records, %d bytes of firmware\n", len(img.Records), img.DataBytes())
		fmt.Printf("  program flash 0x%08X + %d bytes, CRC 0x%04X\n", img.Start, img.Length, img.CRC)
	} else if !(*crc && *rng != "") {
		flag.Usage()
		os.Exit(2)
	}
	if *check {
		return
	}
	if *crc {
		if err := doCRC(*portName, *baud, *wait, img, *rng); err != nil {
			log.Fatalf("%v", err)
		}
		return
	}

	port := *portName
	if port == "" {
		if port, err = findPort(); err != nil {
			log.Fatalf("%v", err)
		}
		fmt.Printf("  port %s (auto-detected)\n", port)
	}

	if !*yes && !confirm(port) {
		fmt.Println("aborted")
		return
	}

	c, err := boot.Open(port, *baud)
	if err != nil {
		log.Fatalf("%v", err)
	}
	defer c.Close()

	if err := flash(c, img, *wait, *run); err != nil {
		log.Fatalf("%v", err)
	}
}

// flash runs the full session. Once Erase returns, the mount has no firmware
// until Verify succeeds, so everything from there on reports plainly.
func flash(c *boot.Client, img *boot.Image, wait time.Duration, run bool) error {
	if err := connect(c, wait); err != nil {
		return err
	}

	// Prevent loading images that have differing application bases
	base, err := c.DetectAppBase()
	if err != nil {
		return fmt.Errorf("detecting application base: %w", err)
	}
	want := uint32(boot.AppBase135)
	if img.Start >= boot.AppBase150 {
		want = boot.AppBase150
	}
	if base != want {
		return fmt.Errorf("family mismatch: the device runs from 0x%08X but this image targets 0x%08X — "+
			"flashing it would aim config-word writes into boot flash; pass the correct model's file", base, want)
	}
	fmt.Printf("device application base 0x%08X matches the image\n", base)

	fmt.Print("erasing... ")
	if err := c.Erase(); err != nil {
		return fmt.Errorf("erase failed (the mount still has its old firmware): %w", err)
	}
	fmt.Println("done")

	fmt.Println("programming — DO NOT POWER OFF OR DISCONNECT")
	start := time.Now()
	err = c.Program(img, func(done, total int) {
		fmt.Printf("\r  %d/%d records (%d%%)", done, total, 100*done/total)
	})
	fmt.Println()
	if err != nil {
		return fmt.Errorf("%w\nthe mount has no working firmware — power-cycle with PREV+NEXT held and run rstflash again", err)
	}
	fmt.Printf("  programmed in %s\n", time.Since(start).Round(time.Second))

	fmt.Print("verifying... ")
	if _, err := c.Verify(img); err != nil {
		return fmt.Errorf("%w\nthe mount has no working firmware — power-cycle with PREV+NEXT held and run rstflash again", err)
	}
	fmt.Printf("ok (CRC 0x%04X)\n", img.CRC)

	if run {
		if err := c.Run(); err != nil {
			return err
		}
		fmt.Println("sent JUMP_TO_APP — the mount should be running the new firmware")
		return nil
	}
	fmt.Println("firmware written. Turn the controller OFF and ON to start it.")
	return nil
}

func connect(c *boot.Client, wait time.Duration) error {
	fmt.Println("waiting for the bootloader — turn the controller ON while holding PREV and NEXT")
	nfo, err := c.Connect(wait)
	if err != nil {
		return fmt.Errorf("%w\n(the controller only enters its bootloader when powered on with PREV+NEXT held)", err)
	}
	if nfo.IsHuboI() {
		fmt.Printf("connected — HUBO-I/ASTRO V001 bootloader %s\n", nfo)
	} else {
		fmt.Printf("connected — bootloader %s (unrecognised version; the vendor tool expects v1.0)\n", nfo)
	}
	return nil
}

// doIdentify works out what the mount actually is, using only the bootloader's
// one read primitive.
//
// The controller family can be determined based on which application base address
// has code below it. The exact installed build is found by checksumming each candidate
// image's range and comparing to local .hex files.
func doIdentify(portName string, baud int, wait time.Duration, files []string) error {
	type cand struct {
		path string
		img  *boot.Image
	}
	var cands []cand
	for _, f := range files {
		img, err := boot.LoadHexFile(f)
		if err != nil {
			return err
		}
		cands = append(cands, cand{f, img})
	}

	port := portName
	if port == "" {
		var err error
		if port, err = findPort(); err != nil {
			return err
		}
		fmt.Printf("port %s (auto-detected)\n", port)
	}
	c, err := boot.Open(port, baud)
	if err != nil {
		return err
	}
	defer c.Close()
	if err := connect(c, wait); err != nil {
		return err
	}

	base, err := c.DetectAppBase()
	if err != nil {
		return fmt.Errorf("detecting application base: %w", err)
	}
	family := "RST-135 / 135E / 300"
	if base == boot.AppBase150 {
		family = "RST-150H / RST400"
	}
	fmt.Printf("application base 0x%08X -> %s family\n", base, family)

	if len(cands) == 0 {
		fmt.Println("(pass hex files to also identify the exact installed build)")
		return nil
	}

	// One ReadCRC per distinct range; several candidates may share one.
	fmt.Println("\nmatching installed firmware against candidates:")
	seen := map[[2]uint32]uint16{}
	var matched []string
	for _, cd := range cands {
		key := [2]uint32{cd.img.Start, cd.img.Length}
		got, ok := seen[key]
		if !ok {
			if got, err = c.ReadCRC(cd.img.Start, cd.img.Length, cd.img.CRC); err != nil {
				return fmt.Errorf("%s: %w", cd.path, err)
			}
			seen[key] = got
		}
		mark := " "
		if got == cd.img.CRC {
			mark = "*"
			matched = append(matched, cd.path)
		}
		fmt.Printf("  %s %-32s 0x%08X+%-7d file 0x%04X  device 0x%04X\n",
			mark, filepath.Base(cd.path), cd.img.Start, cd.img.Length, cd.img.CRC, got)
	}

	fmt.Println()
	switch len(matched) {
	case 0:
		fmt.Println("no candidate matches — the installed build is not among these files")
	case 1:
		fmt.Printf("INSTALLED: %s\n", filepath.Base(matched[0]))
	default:
		fmt.Printf("ambiguous: %v\n", matched)
	}
	return nil
}

// doCRC asks the device to checksum flash it already holds and reports the
// result. Nothing is erased or written, so this is safe to run against firmware
// you want to keep — and it exercises READ_CRC without committing to a flash.
func doCRC(portName string, baud int, wait time.Duration, img *boot.Image, rng string) error {
	start, length, expected := uint32(0), uint32(0), uint16(0)
	switch {
	case rng != "":
		var err error
		if start, length, err = parseRange(rng); err != nil {
			return err
		}
	case img != nil:
		start, length, expected = img.Start, img.Length, img.CRC
	default:
		return fmt.Errorf("-crc needs either a hex file or -range ADDR:LEN")
	}

	port := portName
	if port == "" {
		var err error
		if port, err = findPort(); err != nil {
			return err
		}
		fmt.Printf("  port %s (auto-detected)\n", port)
	}
	c, err := boot.Open(port, baud)
	if err != nil {
		return err
	}
	defer c.Close()
	if err := connect(c, wait); err != nil {
		return err
	}

	fmt.Printf("reading device CRC over 0x%08X + %d bytes... ", start, length)
	got, err := c.ReadCRC(start, length, expected)
	if err != nil {
		fmt.Println()
		return err
	}
	fmt.Printf("0x%04X\n", got)

	if img != nil && rng == "" {
		if got == img.CRC {
			fmt.Println("MATCHES the hex file — this firmware is already installed")
		} else {
			fmt.Printf("differs from the hex file (0x%04X) — the device holds other firmware\n", img.CRC)
		}
	}
	return nil
}

// parseRange parses "ADDR:LEN", each decimal or 0x-prefixed hex.
func parseRange(s string) (start, length uint32, err error) {
	a, b, ok := strings.Cut(s, ":")
	if !ok {
		return 0, 0, fmt.Errorf("bad -range %q, want ADDR:LEN", s)
	}
	av, err := strconv.ParseUint(strings.TrimSpace(a), 0, 32)
	if err != nil {
		return 0, 0, fmt.Errorf("bad -range address %q: %w", a, err)
	}
	bv, err := strconv.ParseUint(strings.TrimSpace(b), 0, 32)
	if err != nil {
		return 0, 0, fmt.Errorf("bad -range length %q: %w", b, err)
	}
	return uint32(av), uint32(bv), nil
}

func doInfo(portName string, baud int, wait time.Duration) {
	port := portName
	if port == "" {
		var err error
		if port, err = findPort(); err != nil {
			log.Fatalf("%v", err)
		}
		fmt.Printf("port %s (auto-detected)\n", port)
	}
	c, err := boot.Open(port, baud)
	if err != nil {
		log.Fatalf("%v", err)
	}
	defer c.Close()
	if err := connect(c, wait); err != nil {
		log.Fatalf("%v", err)
	}
}

// findPort picks the mount's USB-serial adapter. The bootloader answers on the
// same FTDI adapter the normal LX200 protocol uses, but it reports no identity
// of its own, so this is the same VID/PID-then-name match rst.Find applies.
func findPort() (string, error) {
	ports, err := serial.List()
	if err != nil {
		return "", err
	}
	for _, p := range ports {
		if p.IsUSB && p.VID == "0403" && p.PID == "6001" {
			return p.Name, nil
		}
	}
	for _, p := range ports {
		if p.VID == "" && p.IsUSB && strings.Contains(p.Name, "usbserial") {
			return p.Name, nil
		}
	}
	return "", fmt.Errorf("no RST serial port found (FTDI 0403:6001); pass -serial")
}

func confirm(port string) bool {
	fmt.Printf("\nThis will ERASE the firmware on the mount at %s and write the file above.\n", port)
	fmt.Print("An interrupted flash leaves the mount unbootable until you retry. Continue? [y/N] ")
	sc := bufio.NewScanner(os.Stdin)
	if !sc.Scan() {
		return false
	}
	answer := strings.ToLower(strings.TrimSpace(sc.Text()))
	return answer == "y" || answer == "yes"
}
