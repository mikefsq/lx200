package boot

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
)

// PIC32 memory map.
const (
	kseg0     = 0x80000000
	bootFlash = 0x9FC00000
	imageBase = 0x9D000000
	imageSize = 0x500000
)

// Intel HEX record types.
const (
	recData   = 0x00
	recEOF    = 0x01
	recExtSeg = 0x02
	recExtLin = 0x04
)

// Image is a parsed Intel HEX firmware file.
type Image struct {
	Records [][]byte
	Start   uint32
	Length  uint32
	CRC     uint16
}

// DataBytes is the number of payload bytes across all data records, for display.
func (img *Image) DataBytes() int {
	var n int
	for _, r := range img.Records {
		if r[3] == recData {
			n += int(r[0])
		}
	}
	return n
}

// LoadHexFile parses an Intel HEX firmware file from disk.
func LoadHexFile(path string) (*Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ParseHex(f)
}

// ParseHex reads an Intel HEX stream into an Image.
func ParseHex(r io.Reader) (*Image, error) {
	flat := make([]byte, imageSize)
	for i := range flat {
		flat[i] = 0xFF
	}

	var (
		img            Image
		extLin, extSeg uint32
		start          = uint32(0xFFFFFFFF)
		end            uint32
		sawData        bool
	)

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for line := 1; sc.Scan(); line++ {
		text := strings.TrimSpace(sc.Text())
		if text == "" {
			continue
		}
		rec, err := decodeRecord(text)
		if err != nil {
			return nil, fmt.Errorf("hex line %d: %w", line, err)
		}
		img.Records = append(img.Records, rec)

		count := uint32(rec[0])
		typ, data := rec[3], rec[4:4+count]
		offset := uint32(rec[1])<<8 | uint32(rec[2])

		if (typ == recExtSeg || typ == recExtLin) && count < 2 {
			return nil, fmt.Errorf("hex line %d: address record type 0x%02X carries %d bytes, want 2", line, typ, count)
		}

		switch typ {
		case recData:
			addr := (extLin | extSeg | offset) | kseg0
			if addr >= bootFlash {
				continue
			}
			if addr < imageBase || uint64(addr)+uint64(count) > imageBase+imageSize {
				return nil, fmt.Errorf("hex line %d: data address 0x%08X outside the program-flash window 0x%08X-0x%08X",
					line, addr, uint32(imageBase), uint32(imageBase+imageSize))
			}
			copy(flat[addr-imageBase:], data)
			if addr < start {
				start = addr
			}
			if addr+count > end {
				end = addr + count
			}
			sawData = true
		case recExtSeg:
			extSeg = (uint32(data[0])<<8 | uint32(data[1])) << 4
			extLin = 0
		case recExtLin:
			extLin = (uint32(data[0])<<8 | uint32(data[1])) << 16
			extSeg = 0
		default:
			extSeg, extLin = 0, 0
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if !sawData {
		return nil, fmt.Errorf("hex file has no program-flash data records")
	}
	if last := img.Records[len(img.Records)-1]; last[3] != recEOF {
		return nil, fmt.Errorf("hex file does not end with an EOF record (last type 0x%02X)", last[3])
	}

	start -= start % 4
	end += end % 4
	if end > imageBase+imageSize {
		end = imageBase + imageSize
	}

	img.Start = start
	img.Length = end - start
	img.CRC = CRC16(flat[start-imageBase : (start-imageBase)+img.Length])
	return &img, nil
}

// decodeRecord converts one ":..." line to its binary record, the exact bytes PROGRAM_FLASH
// carries: count, address, type, data and checksum.
func decodeRecord(text string) ([]byte, error) {
	if !strings.HasPrefix(text, ":") {
		return nil, fmt.Errorf("missing ':' start code")
	}
	rec, err := hex.DecodeString(text[1:])
	if err != nil {
		return nil, err
	}
	if len(rec) < 5 {
		return nil, fmt.Errorf("record too short (%d bytes)", len(rec))
	}
	if int(rec[0]) != len(rec)-5 {
		return nil, fmt.Errorf("byte count %d does not match %d data bytes", rec[0], len(rec)-5)
	}
	var sum byte
	for _, b := range rec {
		sum += b
	}
	if sum != 0 {
		return nil, fmt.Errorf("bad checksum")
	}
	return rec, nil
}
