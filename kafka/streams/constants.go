package main

// Unit class name constants — mirrors config.Class* in option-b.
// Defined here to avoid a cross-module import; values must stay in sync with units.conf.
// No unit ID string literal (e.g. "witch-king", "gandalf") may appear in game logic.
const (
	classRingBearer      = "RingBearer"
	classFellowshipGuard = "FellowshipGuard"
	classGondorArmy      = "GondorArmy"
	classNazgul          = "Nazgul"
	classUrukHaiLegion   = "UrukHaiLegion"
	classMaia            = "Maia"
)

// Unit status constants — mirrors state.Status* in option-b.
const (
	statusActive     = "ACTIVE"
	statusDestroyed  = "DESTROYED"
	statusRespawning = "RESPAWNING"
)

// Side constants — mirrors config.Side* in option-b.
const (
	sideLight = "FREE_PEOPLES"
	sideDark  = "SHADOW"
)

// Path status constants — mirrors state.Path* in option-b.
const (
	pathOpen            = "OPEN"
	pathThreatened      = "THREATENED"
	pathBlocked         = "BLOCKED"
	pathTemporarilyOpen = "TEMPORARILY_OPEN"
)
