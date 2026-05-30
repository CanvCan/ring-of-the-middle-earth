package state

import "github.com/lotr/option-b/internal/config"

type UnitStatus string

const (
	StatusActive     UnitStatus = "ACTIVE"
	StatusDestroyed  UnitStatus = "DESTROYED"
	StatusRespawning UnitStatus = "RESPAWNING"
)

type PathStatus string

const (
	PathOpen            PathStatus = "OPEN"
	PathThreatened      PathStatus = "THREATENED"
	PathBlocked         PathStatus = "BLOCKED"
	PathTemporarilyOpen PathStatus = "TEMPORARILY_OPEN"
)

type ControlSide string

const (
	ControlFree    ControlSide = "FREE_PEOPLES"
	ControlShadow  ControlSide = "SHADOW"
	ControlNeutral ControlSide = "NEUTRAL"
)

// UnitSnapshot is the mutable runtime state of a unit.
type UnitSnapshot struct {
	ID                string
	Region            string     // always "" for ring-bearer in public state
	Strength          int
	Status            UnitStatus
	RespawnTurns      int
	Route             []string // ordered list of path IDs
	RouteIdx          int
	RemainingPathCost int      // turns left on the current path segment (cost support)
	Cooldown          int
	Disabled          bool // Saruman disabled when Isengard falls
}

// RegionState is the mutable runtime state of a region.
type RegionState struct {
	ID           string
	Control      ControlSide
	ThreatLevel  int
	Fortified    bool
	FortifyTurns int
	UnitsPresent []string // unit IDs
}

// PathState is the mutable runtime state of a path.
type PathState struct {
	ID                string
	Status            PathStatus
	PreviousStatus    PathStatus // status before becoming BLOCKED; used when reverting
	SurveillanceLevel int        // 0–3
	TempOpenTurns     int
	BlockedBy         string // unit ID blocking this path, "" if none
}

// RingBearerState is owned exclusively by the game engine — never exposed to shared topics.
type RingBearerState struct {
	TrueRegion         string
	Exposed            bool
	Route              []string
	RouteIdx           int
	RemainingPathCost  int
	LastDetectedTurn   int
	LastDetectedRegion string
}

// WorldStateCache is the in-memory view of the entire game world.
type WorldStateCache struct {
	Turn                  int
	MaxTurns              int
	HiddenUntilTurn       int
	RingDestructionSiteID string               // read-only: region with specialRole=RING_DESTRUCTION_SITE
	Units                 map[string]UnitSnapshot
	Regions               map[string]RegionState
	Paths                 map[string]PathState
	UnitConfigs           map[string]config.UnitConfig // read-only after startup
	Graph                 *config.Graph                // read-only after startup
	RingBearer            RingBearerState              // never sent outside engine
	GameOver              bool
	Winner                string
	LightView             LightSideView
	DarkView              DarkSideView
}

// LightSideView is what the Light Side player can see.
type LightSideView struct {
	RingBearerRegion string
	AssignedRoute    []string
	RouteIdx         int // current position of the Ring Bearer in AssignedRoute
}

// DarkSideView is what the Dark Side player can see.
// RingBearerRegion MUST always be "" — enforced by EventRouter and CacheManager.
type DarkSideView struct {
	RingBearerRegion   string // ALWAYS "" — no code path ever sets this
	LastDetectedRegion string
	LastDetectedTurn   int
}

// PublicUnitSnapshot strips ring-bearer's region for the dark side.
func PublicUnitSnapshot(u UnitSnapshot, forSide string, cfg config.UnitConfig) UnitSnapshot {
	if cfg.Class == config.ClassRingBearer && forSide == config.SideDark {
		u.Region = ""
	}
	return u
}
