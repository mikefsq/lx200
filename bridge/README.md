# lx200/bridge

An LX200 TCP **server** that fronts any `lx200.Mount`, so sky atlases that speak
the Meade LX200 telescope protocol — **Stellarium**'s TelescopeControl ("Meade
LX200") and **SkySafari**'s LX200 mode — can connect to a mount that already has
a goalpaca driver connected.

It is the protocol inverse of the `lx200` client core: where the core sends
`:CMD#` frames *to* a mount, the bridge *answers* them. It is a **consumer** of a
`lx200.Mount`, exactly like the Alpaca Telescope wrapper — the two front-ends sit
side by side over the same mount, which stays the single source of truth.

```
            lx200.Mount   (the device; owns the connection)
           /          \
  Alpaca Telescope     bridge.Server
  (goalpaca-devices)    LX200 TCP for Stellarium / SkySafari
```

## State integrity across the two front-ends

The Alpaca-side and LX200-side can conflict with each other. There is no protection
from slewing the mount while mid exposure. 

- **No cached device state.** Every `:GR#`/`:GD#`/`:GA#`/`:GZ#` reads *live* from
  the `Mount`. A slew started over Alpaca is immediately visible to the atlas, and
  vice-versa. The mount's own per-command serialization makes each read consistent.
- **Atomic target writes.** The LX200 client sends `:Sr` (RA), `:Sd` (Dec) and
  `:MS#` (slew) as three separate messages. The bridge buffers RA/Dec *per
  connection* (the client's intent — the LX200 analogue of Alpaca's remembered
  `TargetRightAscension`) and writes the device's single target register only
  inside an **`OpLock`-guarded** `SetTarget → act` sequence at `:MS#`/`:CM#` time.
  Because the Alpaca wrapper takes the same `OpLock` for its slews, the two
  front-ends can never interleave and leave the mount aiming at one client's RA
  with another's Dec. (`OpLock` is provided by `*lx200.Conn`; the bridge discovers
  it via the `lx200.OpLocker` assertion and runs lock-free if a mount lacks it.)

## Usage

```go
b := bridge.New(":4030", tel.LiveMount,
    bridge.WithMountType('G'),                  // ACK reply: 'P' polar (default), 'G' GEM, 'A' alt-az
    bridge.WithIdent("10micron", "fw-1.0"),     // :GVP# / :GVN#
    bridge.WithLogger(log.Printf),
)
go b.Serve(ctx) // one goroutine per connection; returns nil on ctx cancel
```

`tel.LiveMount` is a `MountFunc` — `func() (lx200.Mount, error)` — returning the
mount that is connected *right now*. The bridge calls it per operation, so a
reconnect on the owning side is picked up transparently and no stale handle or
device state is ever cached.

## Wiring it into a driver

A driver opts in from its `cmd/main` (not from the wrapper type). The `tenmicron`
Alpaca driver does this behind `-lx200-port`:

```
tenmicron -addr 10.0.1.51:3492 -port 11200 -lx200-port 4030
```

## Connecting Stellarium

1. Enable the **Telescope Control** plugin (Configuration → Plugins), restart.
2. Configure a telescope → **External software or remote computer** is *not* it —
   choose a directly-connected device of type **Meade LX200 (compatible)**.
3. Set the connection to **TCP**, host = the bridge machine, port = `-lx200-port`
   (e.g. `4030`).
4. Connect. Current Stellarium will show the reticle at the mount's live position;
   "Current object" → **Slew** sends `:Sr/:Sd/:MS#`; the configured sync key sends
   `:CM#`.

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


