# Adding a mount driver

Create a package for the mount dialect and embed `*lx200.Conn` in its mount
type. Open a TCP or serial transport, then pass it to `lx200.New`. Implement
the status and tracking methods required by `lx200.Mount`, overriding shared
commands where the dialect differs.

## Command replies

Choose the primitive from the command's reply format:

| Method | Reply |
|---|---|
| `Blind` | None |
| `Ack` | One byte, with `1` indicating success |
| `AckByte` | One byte interpreted by the caller |
| `Get` | Text terminated by `#`, returned without the terminator |
| `Slew` | `0` for success, otherwise a hash-terminated refusal |
| `SlewNack` | A hash-terminated refusal, or silence on acceptance |
| `Await` | An unsolicited hash-terminated message |
| `GetMatching` | A matching reply after skipping unsolicited frames |

Pass complete command frames to these methods. Use `Frame` when accepting
bare commands from a caller. Consume the entire reply, including status
bytes, to keep subsequent reads aligned. An acknowledgement may only confirm
receipt; test readback where the firmware can silently ignore a value.

For asynchronous completion tokens, route skipped frames into the mount's
motion state. `GetMatching` holds the command lock throughout the query and
matching reads; exhausting its skip budget returns `ErrNoMatch`.

## Interfaces and state

Use the units and sign conventions in each interface: RA is in hours,
declination in degrees, and site longitude is east-positive. Convert vendor
formats at the package boundary.

Implement optional interfaces in `mount.go` only for supported features.
Embedding `Conn` inherits some capabilities; override methods when their
wire commands differ. Embedded Go methods call their own receiver's methods,
so overriding `SetRate` alone does not change the embedded `MoveAxis`.

Protect cached state against concurrent calls and invalidate it after commands
that change it. Callers must share `OpLock` for multi-command target/slew/sync
operations; per-command locking alone cannot protect a target sequence.

## Transports and tests

`Transport` requires `io.ReadWriteCloser`. TCP connections support read
deadlines. Other transports must return periodically from `Read` so the
connection can enforce command timeouts; the `serial` package configures this.
On macOS, serial enumeration omits USB identifiers.

Use `internal/lx200test.Fake` for command/reply tests. Cover framing, rejection,
timeouts, units, and any firmware-specific state transitions. Compile-time
interface assertions document the capabilities a mount provides.

Keep hardware tests opt-in and state whether they move the mount or write
configuration. Scripted replies verify the implementation against fixtures;
they do not establish that a device accepts the commands.
