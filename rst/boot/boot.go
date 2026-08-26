// Package boot flashes firmware to Rainbow Astro RST mounts over the Microchip
// AN1388 PIC32 bootloader their controllers run.
//
// Power the hand controller on while holding PREV and NEXT, then Connect
// polls READ_BOOT_INFO until it answers.
package boot

import (
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/mikefsq/lx200/serial"
)

// Baud is the controller's bootloader line rate.
const Baud = 115200

// Command is an AN1388 bootloader opcode.
type Command byte

const (
	ReadBootInfo Command = 0x01 // no arguments; replies with the version
	EraseFlash   Command = 0x02 // no arguments; the device knows its app range
	ProgramFlash Command = 0x03 // followed by decoded Intel HEX records
	ReadCRC      Command = 0x04 // followed by addr, length and expected CRC
	JumpToApp    Command = 0x05 // no arguments, no reply
)

// Per-command retry counts and reply timeouts, taken from the vendor downloader.
const (
	connectRetryEvery     = 200 * time.Millisecond
	eraseTimeout          = 5 * time.Second
	programTimeout        = 500 * time.Millisecond
	verifyTimeout         = 5 * time.Second
	commandRetries        = 3
	DefaultConnectTimeout = 6 * time.Second //user might have to power-cycle the controller.
)

// recordsPerFrame is how many Intel HEX records one PROGRAM_FLASH frame carries.
const recordsPerFrame = 11

// ErrTimeout is returned when the bootloader does not answer a command.
var ErrTimeout = errors.New("rst/boot: no response from the device")

// Info is the bootloader version reported by READ_BOOT_INFO.
type Info struct {
	Major, Minor byte
}

func (i Info) String() string { return fmt.Sprintf("v%d.%d", i.Major, i.Minor) }

// IsHuboI reports whether the version matches the controller the vendor downloader recognises.
// Other versions are not refused.
func (i Info) IsHuboI() bool { return i.Major == 1 && i.Minor == 0 }

// Client is a connection to a mount sitting in its bootloader.
type Client struct {
	rw  io.ReadWriteCloser
	dec decoder
	in  []byte
}

// Open dials the controller's serial port at the given baud (pass Baud for the
// default). The mount must already be in its bootloader.
func Open(portName string, baud int) (*Client, error) {
	tr, err := serial.Open(portName, baud)
	if err != nil {
		return nil, err
	}
	return New(tr), nil
}

// New wraps an already-open byte pipe, for tests and alternative transports.
func New(rw io.ReadWriteCloser) *Client {
	return &Client{rw: rw, in: make([]byte, 256)}
}

func (c *Client) Close() error { return c.rw.Close() }

// Connect polls READ_BOOT_INFO until the bootloader answers or timeout elapses.
// The controller enters its bootloader when powered on with PREV and NEXT held.
func (c *Client) Connect(timeout time.Duration) (Info, error) {
	attempts := int(timeout / connectRetryEvery)
	if attempts < 1 {
		attempts = 1
	}
	reply, err := c.do(ReadBootInfo, nil, attempts, connectRetryEvery)
	if err != nil {
		return Info{}, err
	}
	if len(reply) < 2 {
		return Info{}, fmt.Errorf("rst/boot: short boot-info reply (%d bytes)", len(reply))
	}
	return Info{Major: reply[0], Minor: reply[1]}, nil
}

// Erase wipes the application flash. The mount will not boot again until Program has run.
func (c *Client) Erase() error {
	_, err := c.do(EraseFlash, nil, commandRetries, eraseTimeout)
	return err
}

// Program streams the image's records to the device, recordsPerFrame at a time.
// progress, if non-nil, is called after each acknowledged frame with the number
// of records sent so far and the total.
func (c *Client) Program(img *Image, progress func(done, total int)) error {
	total := len(img.Records)
	for i := 0; i < total; i += recordsPerFrame {
		end := min(i+recordsPerFrame, total)

		var args []byte
		for _, rec := range img.Records[i:end] {
			args = append(args, rec...)
		}
		if _, err := c.do(ProgramFlash, args, commandRetries, programTimeout); err != nil {
			return fmt.Errorf("programming records %d-%d: %w", i, end-1, err)
		}
		if progress != nil {
			progress(end, total)
		}
	}
	return nil
}

// ReadCRC returns the device's checksum of a range of flash.
func (c *Client) ReadCRC(start, length uint32, expected uint16) (uint16, error) {
	args := []byte{
		byte(start), byte(start >> 8), byte(start >> 16), byte(start >> 24),
		byte(length), byte(length >> 8), byte(length >> 16), byte(length >> 24),
		byte(expected), byte(expected >> 8),
	}
	reply, err := c.do(ReadCRC, args, commandRetries, verifyTimeout)
	if err != nil {
		return 0, err
	}
	if len(reply) < 2 {
		return 0, fmt.Errorf("rst/boot: short CRC reply (%d bytes)", len(reply))
	}
	return uint16(reply[0]) | uint16(reply[1])<<8, nil
}

// ErasedCRC returns the checksum for a run of erased flash of the given length. Comparing a
// ReadCRC result against it identifies an unprogrammed region without needing a firmware
// image.
func ErasedCRC(length uint32) uint16 {
	const chunk = 8192
	buf := make([]byte, chunk)
	for i := range buf {
		buf[i] = 0xFF
	}
	var crc uint16
	for n := length; n > 0; {
		step := uint32(chunk)
		if n < step {
			step = n
		}
		crc = crc16From(crc, buf[:step])
		n -= step
	}
	return crc
}

// AppBase is a candidate application base address.
const (
	AppBase135 = 0x9D000000
	AppBase150 = 0x9D006100
)

// DetectAppBase identifies the controller family by asking whether the flash
// below AppBase150 is erased. It writes nothing and needs no firmware file.
func (c *Client) DetectAppBase() (uint32, error) {
	const span = AppBase150 - AppBase135
	got, err := c.ReadCRC(AppBase135, span, 0)
	if err != nil {
		return 0, err
	}
	if got == ErasedCRC(span) {
		return AppBase150, nil // nothing programmed below the 150H/400 base
	}
	return AppBase135, nil
}

// Verify checksums the programmed range and compares it with the image. It
// returns the device's CRC even on mismatch, for diagnostics.
func (c *Client) Verify(img *Image) (uint16, error) {
	got, err := c.ReadCRC(img.Start, img.Length, img.CRC)
	if err != nil {
		return 0, err
	}
	if got != img.CRC {
		return got, fmt.Errorf("rst/boot: verification failed: device CRC 0x%04X, image CRC 0x%04X", got, img.CRC)
	}
	return got, nil
}

// Run issues JUMP_TO_APP. The bootloader hands control to the freshly programmed
// firmware without replying, so this only reports a write error.
func (c *Client) Run() error { return c.write(JumpToApp, nil) }

// do sends one command and waits for the matching reply, retrying the whole
// exchange up to retries times. It returns the reply payload with the echoed
// command byte stripped.
func (c *Client) do(cmd Command, args []byte, retries int, timeout time.Duration) ([]byte, error) {
	if retries < 1 {
		retries = 1
	}
	var err error
	for attempt := 0; attempt < retries; attempt++ {
		c.dec.reset()
		if err = c.write(cmd, args); err != nil {
			return nil, err
		}
		var reply []byte
		reply, err = c.await(cmd, timeout)
		if err == nil {
			return reply, nil
		}
		if !errors.Is(err, ErrTimeout) {
			return nil, err
		}
	}
	return nil, fmt.Errorf("rst/boot: command 0x%02X failed after %d attempts: %w", byte(cmd), retries, err)
}

func (c *Client) write(cmd Command, args []byte) error {
	payload := make([]byte, 0, len(args)+1)
	payload = append(payload, byte(cmd))
	payload = append(payload, args...)
	if _, err := c.rw.Write(encodeFrame(payload)); err != nil {
		return fmt.Errorf("rst/boot: write command 0x%02X: %w", byte(cmd), err)
	}
	return nil
}

// await reads until a well-formed frame echoing cmd arrives, or the deadline
// passes. Frames for other commands are stale replies from an earlier retry and
// are skipped.
func (c *Client) await(cmd Command, timeout time.Duration) ([]byte, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		n, err := c.rw.Read(c.in)
		if err != nil {
			return nil, fmt.Errorf("rst/boot: read: %w", err)
		}
		if n == 0 {
			// A serial port blocks for its read timeout, but a transport that returns
			// immediately when idle would spin here.
			time.Sleep(2 * time.Millisecond)
			continue
		}
		for _, b := range c.in[:n] {
			payload, ok := c.dec.feed(b)
			if !ok || len(payload) == 0 || payload[0] != byte(cmd) {
				continue
			}
			return payload[1:], nil
		}
	}
	return nil, ErrTimeout
}
