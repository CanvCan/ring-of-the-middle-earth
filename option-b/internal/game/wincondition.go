package game

import (
	"github.com/rotr/option-b/internal/config"
	"github.com/rotr/option-b/internal/state"
)

type GameResult struct {
	Over   bool
	Winner string // "FREE_PEOPLES" | "SHADOW" | "DRAW"
	Cause  string
}

// EvaluateWinConditions checks win/draw conditions per spec Section 1.2.
// Called as Step 13 of turn processing.
// Uses cache.RingDestructionSiteID (set from config at startup) — no hardcoded region ID.
func EvaluateWinConditions(cache *state.WorldStateCache, turn int, destroyRingSubmitted bool) GameResult {
	rb := cache.RingBearer
	site := cache.RingDestructionSiteID

	// Light Side win: RB at destruction site + DestroyRing submitted + no Dark Side unit there
	if site != "" && rb.TrueRegion == site && destroyRingSubmitted {
		darkUnitAtSite := false
		for _, unit := range cache.Units {
			cfg := cache.UnitConfigs[unit.ID]
			if cfg.Side == config.SideDark &&
				unit.Region == site &&
				unit.Status == state.StatusActive {
				darkUnitAtSite = true
				break
			}
		}
		if !darkUnitAtSite {
			return GameResult{Over: true, Winner: config.SideLight, Cause: "RING_DESTROYED"}
		}
	}

	// Dark Side win: any Nazgul in same region as RB AND exposed==true
	if rb.Exposed {
		for _, unit := range cache.Units {
			cfg := cache.UnitConfigs[unit.ID]
			if cfg.Class == config.ClassNazgul &&
				unit.Region == rb.TrueRegion &&
				unit.Status == state.StatusActive {
				return GameResult{Over: true, Winner: config.SideDark, Cause: "RING_BEARER_CAPTURED"}
			}
		}
	}

	// Draw after maxTurns
	if turn >= cache.MaxTurns {
		return GameResult{Over: true, Winner: "DRAW", Cause: "MAX_TURNS_REACHED"}
	}

	return GameResult{Over: false}
}

// ValidateDestroyRing checks conditions for DestroyRing order.
// Uses cache.RingDestructionSiteID — no hardcoded region ID.
func ValidateDestroyRing(cache *state.WorldStateCache) error {
	rb := cache.RingBearer
	site := cache.RingDestructionSiteID
	if site == "" || rb.TrueRegion != site {
		return &ValidationError{
			ErrorCode: ErrDestroyConditionNotMet,
			Message:   "Ring Bearer is not at the Ring Destruction Site",
		}
	}
	// Check no Dark Side unit at the destruction site
	for _, unit := range cache.Units {
		cfg := cache.UnitConfigs[unit.ID]
		if cfg.Side == config.SideDark &&
			unit.Region == site &&
			unit.Status == state.StatusActive {
			return &ValidationError{
				ErrorCode: ErrDestroyConditionNotMet,
				Message:   "Dark Side unit present at the Ring Destruction Site",
			}
		}
	}
	return nil
}
