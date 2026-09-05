# lx200/bridge

An LX200 TCP server for clients such as Stellarium and SkySafari. It serves
an existing `lx200.Mount`, which owns the device connection and can also be
shared with an Alpaca wrapper.

## Use the bridge

```go
package main

import (
    "context"
    "log"
    "os"
    "os/signal"

    "github.com/mikefsq/lx200"
    "github.com/mikefsq/lx200/bridge"
    "github.com/mikefsq/lx200/tenmicron"
)

func run() error {
    mount, err := tenmicron.Connect("10.0.1.51:3492")
    if err != nil {
        return err
    }
    defer mount.Close()

    ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
    defer stop()
    server := bridge.New(":4030", func() (lx200.Mount, error) {
        return mount, nil
    }, bridge.WithMountType('G'), bridge.WithReadOnlySite())
    return server.Serve(ctx)
}

func main() {
    if err := run(); err != nil {
        log.Fatal(err)
    }
}
```

Configure the client for Meade LX200 over TCP, using the bridge machine's
address and port `4030`. `Serve` handles connections until the context is
cancelled. For an application that reconnects the device, supply a `MountFunc`
that returns its current mount or a connection error.

`WithMountType` sets the alignment reply (`P` polar by default, `G` German
equatorial, `A` alt-az). `WithIdent` sets the fallback product and version;
`WithLogger` enables diagnostics. `WithReadOnlySite` acknowledges site/time
writes without applying them, preserving the mount's configuration.

## Sharing a mount

The bridge buffers target RA/Dec per client and applies them together when
slewing or syncing. If the mount implements `lx200.OpLocker`, it locks that
sequence. Other callers must use the same lock to prevent interleaved targets.
The bridge does not coordinate motion with camera exposures.

Coordinates are read through the mount, whose driver may cache status. The
bridge caches product, site, and UTC offset after successful reads; site writes
update its cache. Date/time replies use the host clock and configured offset.
The bridge performs no background polling.

## Implemented commands

| Command            | Action                                              |
|--------------------|-----------------------------------------------------|
| `ACK` (0x06)       | alignment/mount-kind byte (`WithMountType`)         |
| `:GR# :GD#`        | live RA / Dec (high precision)                      |
| `:GA# :GZ#`        | live Alt / Az (if the mount is `lx200.Horizontal`)  |
| `:GVP# :GVN#`      | product name / firmware                             |
| `:Sr… :Sd…`        | buffer target RA / Dec → `1` ok, `0` rejected       |
| `:MS#`             | atomic goto buffered target → `0`, or `1<reason>#`  |
| `:CM#`             | atomic sync to buffered target → match string       |
| `:Q#`              | halt all motion                                     |
| `:Mn/s/e/w#`, `:Qn/s/e/w#`, `:RG/C/M/S#` | manual move/rate (if the mount supports them) |
| `:D#`              | slewing status (non-empty while moving)             |
| `:U#`              | precision toggle (no-op; always high precision)     |
| `:Gt# :Gg#` | site latitude / longitude in Meade format |
| `:GG# :GL# :GC#` | UTC offset / local time / date |
| `:St… :Sg… :SG… :SL… :SC…` | set site or time, unless `WithReadOnlySite` is enabled |
| `:GW#` | mount type, tracking state, and a fixed aligned flag |

Unsupported queries return an empty reply; other unsupported commands are ignored.
