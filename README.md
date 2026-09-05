# lx200

Go library for telescope mounts using the Meade LX200 command family.
It provides serial and TCP connections, mount-specific commands, and an
[LX200 TCP bridge](bridge/README.md) for clients such as Stellarium and SkySafari.

Requires Go 1.25 or later. Linux, macOS, and Windows builds do not require cgo.

| Package | Mounts | Connection |
|---|---|---|
| `tenmicron` | 10Micron GM-series | TCP |
| `rst` | Rainbow Astro RST | USB-serial |
| `am5` | ZWO AM3, AM5, AM5N, AM7 | USB-serial or TCP |
| `onstep` | OnStep and OnStepX | Serial or TCP |

10Micron and RST have been exercised against hardware. AM-series and OnStep
support is based on vendor protocols and INDI implementations and needs hardware
validation. Alpaca wrappers are maintained separately in
[goalpaca-devices](https://github.com/mikefsq/goalpaca-devices).

## Use the library

```go
package main

import (
    "fmt"
    "log"

    "github.com/mikefsq/lx200/tenmicron"
)

func run() error {
    mount, err := tenmicron.Connect("10.0.1.51:3492")
    if err != nil {
        return err
    }
    defer mount.Close()

    ra, err := mount.RA()
    if err != nil {
        return err
    }
    dec, err := mount.Dec()
    if err != nil {
        return err
    }
    fmt.Printf("RA %.6f hours, Dec %.6f degrees\n", ra, dec)
    return nil
}

func main() {
    if err := run(); err != nil {
        log.Fatal(err)
    }
}
```

Use `rst.Open(port)`, `am5.Open(port)`, or `onstep.Open(port)` for serial
connections. AM-series and OnStep also provide `Dial("host:port")`.
`rst.Find()` probes candidate serial ports; `rst.FindMatching` can restrict
that scan by USB serial number. On macOS, serial enumeration provides device
names but no USB identifiers, so use an explicit port when selecting a device.

Commands are serialized individually. When sharing a mount between callers,
hold `OpLock` across target-setting and slew/sync sequences. Check setter
acknowledgements and errors before starting motion.

RST slews require a completed home seek. Poll `Slewing` for completion and
check `Fault` afterward. Homing uses a fixed speed and blocks interfering
motion commands. `SetAxisRate` writes a speed slot before selecting it;
`SetRate` alone selects whatever speed is already stored.

## Command-line tools

```sh
go build -o tenmicron ./cmd/tenmicron
go build -o rst ./cmd/rst
./tenmicron -addr 10.0.1.51:3492 ra
./rst -serial /dev/ttyUSB0 ra
```

Omit the command to open an interactive console, then enter `help` to list
commands. The `tenmicron` console also provides `dump` for a read-only query
sweep. Both consoles expose motion, configuration, and raw protocol commands.

## RST firmware

Build the firmware tool with `go build -o rstflash ./cmd/rstflash`.
Power on the controller while holding **PREV and NEXT** to enter the bootloader.

```sh
./rstflash -check firmware.hex
./rstflash -serial /dev/ttyUSB0 -info
./rstflash -serial /dev/ttyUSB0 firmware.hex
```

`-check` only parses the file. Flashing prompts before erasing, programming,
and verifying the application firmware. Use firmware for the exact mount model.
An interrupted flash requires another completed flash before normal startup;
re-enter the bootloader with PREV and NEXT to retry.

The bootloader implementation has loopback tests; hardware flashing has not
been validated. Use `-help` for CRC and firmware-identification options.

## Development

```sh
go test -race ./...
go vet ./...
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build ./...
```

Tests use scripted transports. The RST hardware test is opt-in through `RST_HW`;
set `RST_PORT` to select its serial port and close other applications using it.
See [DRIVERS.md](DRIVERS.md) for adding a mount dialect or transport.

## License

[MIT](LICENSE).
