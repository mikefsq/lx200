package boot

// AN1388 framing bytes. SOH opens a frame and EOT closes it; both are written
// raw. Every byte in between equal to SOH, EOT or DLE is escaped by DLE.
const (
	soh = 0x01
	eot = 0x04
	dle = 0x10
)

// receive buffer maxFrame
const maxFrame = 0xFD

// crcTable is the nibble-wise CRC-16-CCITT table (polynomial 0x1021).
var crcTable = [16]uint16{
	0x0000, 0x1021, 0x2042, 0x3063, 0x4084, 0x50a5, 0x60c6, 0x70e7,
	0x8108, 0x9129, 0xa14a, 0xb16b, 0xc18c, 0xd1ad, 0xe1ce, 0xf1ef,
}

// CRC16 is the AN1388 frame checksum: CRC-16-CCITT (polynomial 0x1021) with an
// initial value of 0, consumed a nibble at a time, high nibble first. It is
// computed over the unescaped payload and appended little-endian.
func CRC16(data []byte) uint16 { return crc16From(0, data) }

// crc16From continues a CRC over another slice
func crc16From(crc uint16, data []byte) uint16 {
	for _, b := range data {
		crc = crc<<4 ^ crcTable[(crc>>12^uint16(b)>>4)&0x0f]
		crc = crc<<4 ^ crcTable[(crc>>12^uint16(b))&0x0f]
	}
	return crc
}

// encodeFrame wraps a payload as SOH | escape(payload ‖ crc_lo ‖ crc_hi) | EOT.
func encodeFrame(payload []byte) []byte {
	crc := CRC16(payload)
	body := make([]byte, 0, len(payload)+2)
	body = append(body, payload...)
	body = append(body, byte(crc), byte(crc>>8))

	out := make([]byte, 0, 2*len(body)+2)
	out = append(out, soh)
	for _, b := range body {
		if b == soh || b == eot || b == dle {
			out = append(out, dle)
		}
		out = append(out, b)
	}
	return append(out, eot)
}

// decoder reassembles frames from the byte stream. It is the mirror of
// encodeFrame: a byte-at-a-time state machine that unescapes, then validates the
// trailing CRC when EOT closes the frame.
type decoder struct {
	buf   []byte
	inDLE bool
}

func (d *decoder) reset() {
	d.buf = d.buf[:0]
	d.inDLE = false
}

// feed pushes one received byte and returns a validated payload if it succeeds.
// a frame that fails its CRC is dropped silently, so the sender retries on timeout.
func (d *decoder) feed(b byte) ([]byte, bool) {
	if len(d.buf) >= maxFrame {
		d.reset()
	}
	if d.inDLE {
		d.inDLE = false
		d.buf = append(d.buf, b)
		return nil, false
	}
	switch b {
	case soh:
		d.buf = d.buf[:0]
	case dle:
		d.inDLE = true
	case eot:
		frame := d.buf
		d.buf = d.buf[:0]
		if len(frame) <= 2 {
			return nil, false
		}
		payload := frame[:len(frame)-2]
		want := uint16(frame[len(frame)-2]) | uint16(frame[len(frame)-1])<<8
		if CRC16(payload) != want {
			return nil, false
		}
		return append([]byte(nil), payload...), true
	default:
		d.buf = append(d.buf, b)
	}
	return nil, false
}
