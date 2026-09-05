package lx200

import "time"

// Mount provides pointing, target, motion, and tracking operations.
// Additional features are exposed through the optional interfaces below.
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

// Parker supports parking and unparking.
type Parker interface {
	Park() error
	Unpark() error
	AtPark() (bool, error)
}

// Homer supports homing and home-position queries.
type Homer interface {
	FindHome() error
	AtHome() (bool, error)
}

// PierSider reports the side of pier.
type PierSider interface {
	PierSide() (PierSide, error)
}

// Horizontal reports altitude and azimuth in degrees.
type Horizontal interface {
	Altitude() (float64, error)
	Azimuth() (float64, error)
}

// Productizer reports the mount product name.
type Productizer interface {
	Product() (string, error)
}

// Guider supports timed pulse guiding.
type Guider interface {
	PulseGuide(d Direction, ms int) error
}

// GuideRater reports the pulse-guide rate as a fraction of the sidereal rate.
type GuideRater interface {
	GuideRateSidereal() (float64, error)
}

// AxisMover supports continuous per-axis motion.
type AxisMover interface {
	MoveAxis(a Axis, positive bool, rate Rate) error
	StopAxis(a Axis) error
}

// TrackRater selects sidereal, lunar, or solar tracking.
type TrackRater interface {
	TrackSidereal() error
	TrackLunar() error
	TrackSolar() error
}

// DualAxisTracker controls tracking on both axes for refraction or pointing models.
type DualAxisTracker interface {
	DualAxisTracking() (bool, error)
	SetDualAxisTracking(on bool) error
}

// SiteSetter sets site latitude/longitude in degrees (east-positive) and elevation in meters.
type SiteSetter interface {
	SetSiteLatitude(deg float64) error
	SetSiteLongitude(deg float64) error
	SetSiteElevation(meters float64) error
}

// SiteReader reports site latitude and east-positive longitude in degrees.
type SiteReader interface {
	SiteLatitude() (float64, error)
	SiteLongitude() (float64, error)
}

// Clock sets the mount clock from UTC.
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

// OpLocker serializes multi-command operations on a shared mount.
// Call the returned function to release the lock.
type OpLocker interface {
	OpLock() func()
}

var (
	_ Horizontal = (*Conn)(nil)
	_ Guider     = (*Conn)(nil)
	_ AxisMover  = (*Conn)(nil)
	_ TrackRater = (*Conn)(nil)
	_ OpLocker   = (*Conn)(nil)
)
