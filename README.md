# lx200

A transport-agnostic Go library for the Meade **LX200** command protocol family —
the `:CMD#`-framed serial/TCP dialect spoken (with vendor extensions) by 10Micron,
ZWO AM-series, Rainbow Astro RST, OnStep, and many other telescope mounts.

Module path: `github.com/mikefsq/lx200`

It is **dependency-light and ASCOM/Alpaca-agnostic**: import it to drive a mount
in-process. The Alpaca Telescope wrapper lives in a separate module.

## Layout

```
lx200/
├── lx200.go          Conn + the four framing primitives
├── commands.go       shared LX200 command set (coords, target, slew, sync, …)
├── sexagesimal.go    HH:MM:SS / sDD*MM:SS parsing & formatting
├── mount.go          Mount interface + optional capability interfaces
├── serial/           serial transport (isolates the go.bug.st/serial dependency)
├── tenmicron/        10Micron GM-series      (TCP)
├── am5/              ZWO AM3/AM5/AM5N/AM7     (USB-serial or WiFi/TCP)
├── rst/              Rainbow Astro RST-135/300 (USB-serial)
│   └── boot/         RST firmware flashing over its Microchip AN1388 bootloader
├── onstep/           OnStep / OnStepX        (USB-serial or WiFi/TCP)
└── bridge/           LX200 TCP *server* — the protocol inverse: serves a Mount
                      to Stellarium / SkySafari (see bridge/README.md)
```

The per-mount packages are LX200 **clients** (they speak `:CMD#` *to* a mount),
each embedding the core `*lx200.Conn` and adding only its vendor-specific status,
tracking, park, and site commands. `bridge/` is the **server** direction: it
*answers* `:CMD#` for an atlas, fronting any `lx200.Mount` — a sibling consumer of
the mount alongside the Alpaca Telescope wrapper.

## Design

- **Framing.** Every LX200 reply is one of four shapes, selected by the primitive
  you call: `Blind` (no reply — `:Q#`, `:Mn#`), `Ack` (one byte `0`/`1` — the
  `:Sr`/`:Sd`/`:St…` set commands), `Get` (read until `#` — the `:Gx#` queries),
  and `Slew` (`:MS#` → `0` started, else a `#`-terminated fault). Commands are
  serialized and bounded by a read deadline.
- **Capabilities.** `Mount` is the contract every per-mount type satisfies.
  Features not all mounts share — park/unpark, find-home, side-of-pier, alt/az,
  pulse-guide, per-axis move, track-rate select, site geometry, UTC clock — are
  small optional interfaces a consumer type-asserts for, so a driver advertises
  exactly what the hardware supports.
- **Transports.** TCP (`DialTCP`, e.g. 10Micron) and serial (`serial.Open`, for
  USB/RS-232). A TCP-only build never links the serial library.

## Usage

```go
import "github.com/mikefsq/lx200/tenmicron"

m, err := tenmicron.Connect("10.0.1.51:3492") // 10Micron over TCP
if err != nil { /* ... */ }
ra, _ := m.RA()
m.SetTargetRA(12.5)
m.SetTargetDec(45.0)
m.SlewToTarget()
```

```go
import "github.com/mikefsq/lx200/am5"

m, err := am5.Open("/dev/tty.usbserial-XXXX") // USB-serial …
// m, err := am5.Dial("192.168.4.1:4030")     // … or WiFi/TCP
```

`rst.Open` / `rst.Find` (auto-detect by USB id) and `onstep.Open` / `onstep.Dial`
follow the same pattern.

### Flashing RST firmware

`rst/boot` and `cmd/rstflash` replace the vendor's Windows-only
`HUBOi_Firmware_Downloader.exe` — which is Microchip's stock PIC32UBL (AN1388)
host, rebranded. The controller enters its bootloader only when powered on with
**PREV and NEXT held**; nothing in software puts it there.

```
rstflash -check RST-135E_260319.hex   # parse and summarise the file; opens no port
rstflash -info                        # report the bootloader version
rstflash RST-135E_260319.hex          # erase, program, verify (prompts first)
```

Flashing erases the application firmware before writing. An interrupted flash
leaves the mount unbootable until a flash completes; retry with PREV+NEXT held.
The protocol is reverse-engineered from the vendor tool and unit-tested against a
loopback bootloader, but has **not** been run against a mount.

## Status

| Mount | Transport | Validation |
|---|---|---|
| 10Micron | TCP | against hardware |
| Rainbow RST | serial | against hardware |
| ZWO AM5 | serial / TCP | from the INDI driver + vendor protocol; wip |
| OnStep | serial / TCP | from the INDI driver + vendor protocol; wip |

## License

MIT — see [LICENSE](LICENSE). 
