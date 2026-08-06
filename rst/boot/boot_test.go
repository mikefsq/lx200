package boot

import (
	"bytes"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestCRC16 pins the checksum against the standard CRC-16/XMODEM vector —
// polynomial 0x1021, initial value 0, not reflected — which is what AN1388's
// nibble-wise table computes.
func TestCRC16(t *testing.T) {
	if got := CRC16([]byte("123456789")); got != 0x31C3 {
		t.Errorf("CRC16(123456789) = 0x%04X; want 0x31C3", got)
	}
	if got := CRC16([]byte{0x01}); got != 0x1021 {
		t.Errorf("CRC16(01) = 0x%04X; want 0x1021", got)
	}
	if got := CRC16(nil); got != 0 {
		t.Errorf("CRC16(nil) = 0x%04X; want 0", got)
	}
}

// TestEncodeFrameEscapes is the framing regression: SOH/EOT are written raw, but
// a payload or CRC byte equal to SOH, EOT or DLE must be preceded by DLE.
func TestEncodeFrameEscapes(t *testing.T) {
	// READ_BOOT_INFO exercises both escape sites at once: the payload byte 0x01
	// is itself SOH, and its CRC (0x1021) has a high byte equal to DLE.
	got := encodeFrame([]byte{0x01})
	want := []byte{soh, dle, 0x01, 0x21, dle, 0x10, eot}
	if !bytes.Equal(got, want) {
		t.Errorf("encodeFrame(01) = % X; want % X", got, want)
	}

	// Every escapable byte, in the payload.
	got = encodeFrame([]byte{0x02, soh, eot, dle, 0x7F})
	if bytes.Count(got[1:len(got)-1], []byte{dle}) < 3 {
		t.Errorf("encodeFrame did not escape SOH/EOT/DLE: % X", got)
	}
}

// TestRoundTrip: anything encodeFrame produces, the decoder must recover.
func TestRoundTrip(t *testing.T) {
	payloads := [][]byte{
		{0x01},
		{0x02},
		{0x03, 0x02, 0x00, 0x00, 0x04, 0x00, 0x00, 0xFA},
		{0x04, 0x00, 0x00, 0x00, 0x9D, 0x10, 0x01, 0x00, 0x00, 0x34, 0x12},
		{soh, eot, dle, dle, soh},
	}
	for _, p := range payloads {
		var d decoder
		var got []byte
		for _, b := range encodeFrame(p) {
			if out, ok := d.feed(b); ok {
				got = out
			}
		}
		if !bytes.Equal(got, p) {
			t.Errorf("round trip of % X = % X", p, got)
		}
	}
}

// TestDecoderRejectsBadCRC: a corrupted frame must not surface as a reply.
func TestDecoderRejectsBadCRC(t *testing.T) {
	frame := encodeFrame([]byte{0x02})
	frame[len(frame)-2] ^= 0xFF // corrupt the CRC high byte

	var d decoder
	for _, b := range frame {
		if _, ok := d.feed(b); ok {
			t.Fatal("decoder accepted a frame with a bad CRC")
		}
	}
	// It must also resynchronise on the next good frame.
	var got []byte
	for _, b := range encodeFrame([]byte{0x02}) {
		if out, ok := d.feed(b); ok {
			got = out
		}
	}
	if !bytes.Equal(got, []byte{0x02}) {
		t.Errorf("decoder did not resync after a bad frame: % X", got)
	}
}

// TestDecoderSkipsNoise: line noise before SOH must not corrupt the frame.
func TestDecoderSkipsNoise(t *testing.T) {
	var d decoder
	var got []byte
	stream := append([]byte{0xAA, 0xBB, 0xCC}, encodeFrame([]byte{0x01, 0x01, 0x00})...)
	for _, b := range stream {
		if out, ok := d.feed(b); ok {
			got = out
		}
	}
	if !bytes.Equal(got, []byte{0x01, 0x01, 0x00}) {
		t.Errorf("payload after noise = % X", got)
	}
}

// --- hex parsing ------------------------------------------------------------

// A minimal but realistic image: an extended-linear record selecting 0x9D00,
// two data records, and EOF. CRLF line endings, as the vendor files use.
const sampleHex = ":020000041D00DD\r\n" +
	":0400000012345678E8\r\n" +
	":040004009ABCDEF0D4\r\n" +
	":00000001FF\r\n"

func TestParseHex(t *testing.T) {
	img, err := ParseHex(strings.NewReader(sampleHex))
	if err != nil {
		t.Fatalf("ParseHex: %v", err)
	}
	if len(img.Records) != 4 {
		t.Errorf("got %d records; want 4 (the EOF record is streamed too)", len(img.Records))
	}
	if img.Start != 0x9D000000 {
		t.Errorf("Start = 0x%08X; want 0x9D000000", img.Start)
	}
	if img.Length != 8 {
		t.Errorf("Length = %d; want 8", img.Length)
	}
	if want := CRC16([]byte{0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC, 0xDE, 0xF0}); img.CRC != want {
		t.Errorf("CRC = 0x%04X; want 0x%04X", img.CRC, want)
	}
	if n := img.DataBytes(); n != 8 {
		t.Errorf("DataBytes = %d; want 8", n)
	}
	// Records must be the decoded binary, not the ASCII text.
	if !bytes.Equal(img.Records[1], []byte{0x04, 0x00, 0x00, 0x00, 0x12, 0x34, 0x56, 0x78, 0xE8}) {
		t.Errorf("record 1 = % X", img.Records[1])
	}
}

// TestParseHexGapsAreErased: unwritten bytes inside the range must checksum as
// 0xFF, matching erased flash — otherwise Verify fails against a good device.
func TestParseHexGapsAreErased(t *testing.T) {
	src := ":020000041D00DD\r\n" +
		":0100000011EE\r\n" + // one byte at 0x9D000000
		":0100080001F6\r\n" + // one byte at 0x9D000008, leaving a 7-byte gap
		":00000001FF\r\n"
	img, err := ParseHex(strings.NewReader(src))
	if err != nil {
		t.Fatalf("ParseHex: %v", err)
	}
	// Range is 0x9D000000..0x9D00000A: one written byte, a 7-byte gap, the
	// second written byte, then a pad byte from the 4-byte end alignment.
	want := CRC16([]byte{0x11, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0x01, 0xFF})
	if img.CRC != want {
		t.Errorf("CRC = 0x%04X; want 0x%04X (gaps must read as erased 0xFF)", img.CRC, want)
	}
}

// TestParseHexExcludesBootFlash: config-word records at 0x1FC00000 are streamed
// to the device but must not move the verified range.
func TestParseHexExcludesBootFlash(t *testing.T) {
	src := ":020000041D00DD\r\n" +
		":0400000012345678E8\r\n" +
		":020000041FC01B\r\n" +
		":043FC000FFFF3F07B9\r\n" +
		":00000001FF\r\n"
	img, err := ParseHex(strings.NewReader(src))
	if err != nil {
		t.Fatalf("ParseHex: %v", err)
	}
	if len(img.Records) != 5 {
		t.Errorf("got %d records; want 5 — boot-flash records are still sent", len(img.Records))
	}
	if img.Start != 0x9D000000 || img.Length != 4 {
		t.Errorf("range = 0x%08X+%d; want 0x9D000000+4 (boot flash excluded)", img.Start, img.Length)
	}
}

// TestParseHexLargeRecord: a record carrying more than 251 data bytes must not
// wrap the byte arithmetic that slices its payload.
func TestParseHexLargeRecord(t *testing.T) {
	data := make([]byte, 255)
	for i := range data {
		data[i] = byte(i)
	}
	rec := append([]byte{255, 0x00, 0x00, recData}, data...)
	src := ":020000041D00DD\r\n" + hexLine(rec) + ":00000001FF\r\n"

	img, err := ParseHex(strings.NewReader(src))
	if err != nil {
		t.Fatalf("ParseHex: %v", err)
	}
	if img.Length != 258 { // 255 extended by the vendor's end += end%4 alignment
		t.Errorf("Length = %d; want 258", img.Length)
	}
	if n := img.DataBytes(); n != 255 {
		t.Errorf("DataBytes = %d; want 255", n)
	}
}

func TestParseHexRejectsCorruption(t *testing.T) {
	cases := map[string]string{
		"bad checksum": ":0400000012345678E9\r\n:00000001FF\r\n",
		"bad count":    ":0500000012345678E8\r\n:00000001FF\r\n",
		"no colon":     "0400000012345678E8\r\n:00000001FF\r\n",
		"not hex":      ":04000000123456ZZE8\r\n:00000001FF\r\n",
		"no EOF":       ":020000041D00DD\r\n:0400000012345678E8\r\n",
		"no data":      ":00000001FF\r\n",
	}
	for name, src := range cases {
		if _, err := ParseHex(strings.NewReader(src)); err == nil {
			t.Errorf("%s: ParseHex succeeded; want an error before anything is erased", name)
		}
	}
}

// --- client -----------------------------------------------------------------

// fakeDevice is a loopback bootloader: it decodes command frames and replies
// with whatever the test's handler returns.
type fakeDevice struct {
	mu      sync.Mutex
	dec     decoder
	out     bytes.Buffer
	handler func(cmd Command, args []byte) []byte
	Got     []Command
	Args    [][]byte
}

func (f *fakeDevice) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, b := range p {
		payload, ok := f.dec.feed(b)
		if !ok {
			continue
		}
		cmd, args := Command(payload[0]), payload[1:]
		f.Got = append(f.Got, cmd)
		f.Args = append(f.Args, append([]byte(nil), args...))
		if reply := f.handler(cmd, args); reply != nil {
			f.out.Write(encodeFrame(reply))
		}
	}
	return len(p), nil
}

func (f *fakeDevice) Read(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.out.Len() == 0 {
		return 0, nil // idle, like a serial port read timeout
	}
	return f.out.Read(p)
}

func (f *fakeDevice) Close() error { return nil }

// echo replies with just the command byte — the bootloader's ack shape.
func echo(cmd Command, _ []byte) []byte { return []byte{byte(cmd)} }

// TestConnectRetries: the mount is only in its bootloader after the user
// power-cycles it, so Connect must keep polling through silence.
func TestConnectRetries(t *testing.T) {
	var n int
	dev := &fakeDevice{handler: func(cmd Command, _ []byte) []byte {
		if n++; n < 3 {
			return nil // still powered off
		}
		return []byte{byte(cmd), 0x01, 0x00}
	}}
	if _, err := New(dev).Connect(2 * time.Second); err != nil {
		t.Fatalf("Connect did not retry through silence: %v", err)
	}
}

func TestConnectTimeout(t *testing.T) {
	dev := &fakeDevice{handler: func(Command, []byte) []byte { return nil }}
	_, err := New(dev).Connect(400 * time.Millisecond)
	if err == nil {
		t.Fatal("Connect succeeded against a silent device")
	}
	if !errors.Is(err, ErrTimeout) {
		t.Errorf("err = %v; want ErrTimeout", err)
	}
}

// TestProgramChunking: records must be batched 11 to a frame, concatenated as
// raw binary behind the command byte.
func TestProgramChunking(t *testing.T) {
	img, err := ParseHex(strings.NewReader(bigHex(25)))
	if err != nil {
		t.Fatalf("ParseHex: %v", err)
	}
	dev := &fakeDevice{handler: echo}

	var lastDone, lastTotal int
	if err := New(dev).Program(img, func(done, total int) { lastDone, lastTotal = done, total }); err != nil {
		t.Fatalf("Program: %v", err)
	}

	// 27 records (ext-linear + 25 data + EOF) => ceil(27/11) = 3 frames.
	if len(dev.Got) != 3 {
		t.Fatalf("sent %d frames; want 3 (11 records each)", len(dev.Got))
	}
	if lastDone != 27 || lastTotal != 27 {
		t.Errorf("progress ended at %d/%d; want 27/27", lastDone, lastTotal)
	}
	// The first frame's arguments must be records 0..10, byte for byte.
	var want []byte
	for _, r := range img.Records[:11] {
		want = append(want, r...)
	}
	if !bytes.Equal(dev.Args[0], want) {
		t.Errorf("frame 0 args = % X;\nwant % X", dev.Args[0], want)
	}
}

// TestVerify pins the READ_CRC argument layout: addr and length little-endian
// u32, then the host's expected CRC little-endian u16.
func TestVerify(t *testing.T) {
	img, err := ParseHex(strings.NewReader(sampleHex))
	if err != nil {
		t.Fatalf("ParseHex: %v", err)
	}
	dev := &fakeDevice{handler: func(cmd Command, _ []byte) []byte {
		return []byte{byte(cmd), byte(img.CRC), byte(img.CRC >> 8)}
	}}
	if _, err := New(dev).Verify(img); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	want := []byte{
		0x00, 0x00, 0x00, 0x9D, // start 0x9D000000
		0x08, 0x00, 0x00, 0x00, // length 8
		byte(img.CRC), byte(img.CRC >> 8),
	}
	if !bytes.Equal(dev.Args[0], want) {
		t.Errorf("ReadCRC args = % X; want % X", dev.Args[0], want)
	}
}

// TestErasedCRC: the chunked erased-flash checksum must equal the direct one,
// since family detection compares against it.
func TestErasedCRC(t *testing.T) {
	for _, n := range []uint32{0, 1, 4096, 8192, 8193, 0x6100} {
		buf := bytes.Repeat([]byte{0xFF}, int(n))
		if got, want := ErasedCRC(n), CRC16(buf); got != want {
			t.Errorf("ErasedCRC(%d) = 0x%04X; want 0x%04X", n, got, want)
		}
	}
}

// TestDetectAppBase: a device with code below the 150H base is a 135-family
// controller; one that reads erased there is a 150H/400.
func TestDetectAppBase(t *testing.T) {
	const span = AppBase150 - AppBase135

	erased := &fakeDevice{handler: func(cmd Command, _ []byte) []byte {
		c := ErasedCRC(span)
		return []byte{byte(cmd), byte(c), byte(c >> 8)}
	}}
	if got, err := New(erased).DetectAppBase(); err != nil || got != AppBase150 {
		t.Errorf("erased region -> base 0x%08X, %v; want 0x%08X", got, err, uint32(AppBase150))
	}

	programmed := &fakeDevice{handler: func(cmd Command, _ []byte) []byte {
		c := ErasedCRC(span) ^ 0xFFFF // anything else
		return []byte{byte(cmd), byte(c), byte(c >> 8)}
	}}
	if got, err := New(programmed).DetectAppBase(); err != nil || got != AppBase135 {
		t.Errorf("programmed region -> base 0x%08X, %v; want 0x%08X", got, err, uint32(AppBase135))
	}
}

func TestVerifyMismatch(t *testing.T) {
	img, err := ParseHex(strings.NewReader(sampleHex))
	if err != nil {
		t.Fatalf("ParseHex: %v", err)
	}
	dev := &fakeDevice{handler: func(cmd Command, _ []byte) []byte {
		return []byte{byte(cmd), 0xAD, 0xDE}
	}}
	got, err := New(dev).Verify(img)
	if err == nil {
		t.Fatal("Verify accepted a mismatched CRC")
	}
	if got != 0xDEAD {
		t.Errorf("device CRC = 0x%04X; want 0xDEAD reported back for diagnostics", got)
	}
}

// TestWriteErrorNotRetried: a dead port must fail fast, not burn every retry.
func TestWriteErrorNotRetried(t *testing.T) {
	c := New(errPipe{})
	if err := c.Erase(); err == nil {
		t.Fatal("Erase succeeded on a broken port")
	} else if errors.Is(err, ErrTimeout) {
		t.Errorf("err = %v; want the underlying write error, not a timeout", err)
	}
}

type errPipe struct{}

func (errPipe) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
func (errPipe) Read([]byte) (int, error)  { return 0, io.ErrClosedPipe }
func (errPipe) Close() error              { return nil }

// hexLine renders a record (count, addr_hi, addr_lo, type, data...) as a hex
// file line, appending the checksum.
func hexLine(rec []byte) string {
	var sum byte
	for _, v := range rec {
		sum += v
	}
	return ":" + strings.ToUpper(hex.EncodeToString(append(rec, -sum))) + "\r\n"
}

// bigHex builds a hex file with n 4-byte data records at consecutive addresses.
func bigHex(n int) string {
	var b strings.Builder
	b.WriteString(":020000041D00DD\r\n")
	for i := range n {
		addr := i * 4
		b.WriteString(hexLine([]byte{0x04, byte(addr >> 8), byte(addr), recData, byte(i), 0x11, 0x22, 0x33}))
	}
	b.WriteString(":00000001FF\r\n")
	return b.String()
}
