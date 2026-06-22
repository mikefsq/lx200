package lx200

import "time"

// Mount is the contract a per-mount library satisfies and the Alpaca Telescope
// wrapper consumes. The embedded *Conn already provides the coordinate, target,
// slew, sync, and halt methods; a per-mount type completes the interface by
// adding the status/tracking members, which diverge per mount (each vendor has a
// different status command and tracking-enable command), then overrides any core
// method whose dialect differs (e.g. a degrees-based sync).
//
// Capabilities that not every mount has are NOT in Mount — they are the small
// optional interfaces below. The wrapper type-asserts for them to advertise the
// matching Alpaca Can*/Has* flags (the same pattern as the server's Busyable).
type Mount interface {
	// Pointing (read)
	RA() (float64, error)
	Dec() (float64, error)

	// Target + goto/sync
	SetTargetRA(hours float64) (bool, error)
	SetTargetDec(deg float64) (bool, error)
	SlewToTarget() error
	SyncToTarget() (string, error)
	Halt() error

	// Status — implemented per mount (status/tracking commands diverge)
	Slewing() (bool, error)
	Tracking() (bool, error)
	SetTracking(on bool) error
}

// PierSide reports the mount's pointing state of the German-equatorial pier (or
// the harmonic-mount equivalent). Values match ASCOM PierSide.
type PierSide int

const (
	PierUnknown PierSide = -1
	PierEast    PierSide = 0
	PierWest    PierSide = 1
)

// --- Optional capability interfaces (type-asserted by the Alpaca wrapper) ---

// Parker is implemented by mounts that can park/unpark. (Alpaca CanPark/CanUnpark.)
type Parker interface {
	Park() error
	Unpark() error
	AtPark() (bool, error)
}

// Homer is implemented by mounts that can find/go home. (Alpaca CanFindHome.)
type Homer interface {
	FindHome() error
	AtHome() (bool, error)
}

// PierSider is implemented by mounts that report side of pier. (Alpaca SideOfPier.)
type PierSider interface {
	PierSide() (PierSide, error)
}

// Horizontal is implemented by mounts that report Alt/Az directly. (Alpaca
// Altitude/Azimuth — otherwise the wrapper derives them.)
type Horizontal interface {
	Altitude() (float64, error)
	Azimuth() (float64, error)
}

// Productizer is implemented by mounts that report a product-name string (LX200
// :GVP#). The LX200 bridge serves it on :GVP# so a connecting client sees the real
// connected mount rather than the bridge's generic identity.
type Productizer interface {
	Product() (string, error)
}

// Guider is implemented by mounts supporting timed pulse guiding. (Alpaca
// CanPulseGuide; *Conn satisfies this via PulseGuide.)
type Guider interface {
	PulseGuide(d Direction, ms int) error
}

// GuideRater is implemented by mounts that can report their pulse-guide rate as a
// fraction of the sidereal rate — the unit INDI's GUIDE_RATE and PHD2 expect.
// Mounts whose protocol gives the rate in arcsec/s convert it here (sidereal is
// ~15.041"/s); mounts that cannot report a rate simply do not implement it. The core
// *Conn does NOT satisfy this — it is per-mount, since the command and units differ.
type GuideRater interface {
	GuideRateSidereal() (float64, error)
}

// AxisMover is implemented by mounts supporting continuous per-axis slews.
// (Alpaca MoveAxis; *Conn satisfies this via MoveAxis/StopAxis.)
type AxisMover interface {
	MoveAxis(a Axis, positive bool, rate Rate) error
	StopAxis(a Axis) error
}

// TrackRater is implemented by mounts with selectable tracking rates. (Alpaca
// TrackingRate; *Conn satisfies this via TrackSidereal/Lunar/Solar.)
type TrackRater interface {
	TrackSidereal() error
	TrackLunar() error
	TrackSolar() error
}

// DualAxisTracker is implemented by mounts that can toggle dual-axis tracking — driving
// BOTH axes to follow a refraction/pointing model (10Micron :Sdat/:Gdat). There is no
// standard ASCOM/Alpaca member for it, so front-ends expose it as an INDI switch or an
// Alpaca Action. It is per-mount (only some protocols have it), not on the core *Conn.
type DualAxisTracker interface {
	DualAxisTracking() (bool, error)
	SetDualAxisTracking(on bool) error
}

// SiteSetter is implemented by mounts that accept observing-site geometry.
// (Alpaca SiteLatitude/SiteLongitude/SiteElevation — formats/signs are
// mount-specific, hence per-mount, not in the core.)
type SiteSetter interface {
	SetSiteLatitude(deg float64) error
	SetSiteLongitude(deg float64) error
	SetSiteElevation(meters float64) error
}

// SiteReader is implemented by mounts that report their configured observing site
// (degrees, longitude East-positive). The LX200 bridge reads it once and re-formats
// it into Meade :Gg#/:Gt# replies for clients that need site coordinates.
type SiteReader interface {
	SiteLatitude() (float64, error)
	SiteLongitude() (float64, error)
}

// Clock is implemented by mounts that accept the UTC date/time. (Alpaca UTCDate.)
type Clock interface {
	SetUTC(t time.Time) error
}

// UTCOffsetReader / UTCOffsetSetter read and set the offset added to local time to
// obtain UTC (LX200 :GG#/:SG#; positive west of Greenwich). The bridge needs the
// offset to render Meade local date/time and to apply a client's time set.
type UTCOffsetReader interface {
	UTCOffset() (time.Duration, error)
}
type UTCOffsetSetter interface {
	SetUTCOffset(offset time.Duration) error
}

// OpLocker is implemented by mounts that can serialize a multi-command logical
// operation (a goto or sync — set-target then act) against other such operations,
// so independent front-ends sharing one mount (the Alpaca wrapper and the LX200
// bridge) cannot interleave their :Sr/:Sd/:MS# sequences and corrupt the device's
// single target register. *Conn satisfies it via OpLock; a front-end type-asserts
// for it and skips the outer lock when absent.
type OpLocker interface {
	OpLock() func()
}

// The core *Conn already satisfies these optional capabilities directly, so a
// per-mount type that embeds it inherits them for free.
var (
	_ Horizontal = (*Conn)(nil)
	_ Guider     = (*Conn)(nil)
	_ AxisMover  = (*Conn)(nil)
	_ TrackRater = (*Conn)(nil)
	_ OpLocker   = (*Conn)(nil)
)
