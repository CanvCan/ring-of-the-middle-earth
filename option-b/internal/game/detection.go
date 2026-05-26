package game

import (
	"github.com/rotr/option-b/internal/config"
	"github.com/rotr/option-b/internal/state"
)

// DetectionEvent is emitted when a Nazgul detects the Ring Bearer.
type DetectionEvent struct {
	NazgulID string
	Region   string
	Turn     int
}

// RunDetection applies the detection formula from spec Section 3.6.
// Returns detection events. Suppressed for turns 1..hiddenUntilTurn.
// IMPORTANT: reads ring bearer's true region — never exposes it outside this function.
func RunDetection(
	cache state.WorldStateCache,
	turn int,
) ([]DetectionEvent, bool) {
	if turn <= cache.HiddenUntilTurn {
		return nil, false
	}

	rbRegion := cache.RingBearer.TrueRegion
	exposed := false
	var events []DetectionEvent

	// Check if Sauron is active in Mordor — passive Eye of Sauron effect
	sauronActive := isSauronActive(cache)

	for _, unit := range cache.Units {
		cfg := cache.UnitConfigs[unit.ID]
		// Only Nazgul detect
		if cfg.Class != config.ClassNazgul {
			continue
		}
		if unit.Status != state.StatusActive {
			continue
		}

		effectiveRange := cfg.DetectionRange
		if sauronActive {
			effectiveRange++ // Witch-King: 2→3, Nazgul 2/3: 1→2
		}

		dist := cache.Graph.Distance(unit.Region, rbRegion)
		if dist >= 0 && dist <= effectiveRange {
			exposed = true
			events = append(events, DetectionEvent{
				NazgulID: unit.ID,
				Region:   rbRegion,
				Turn:     turn,
			})
		}
	}

	return events, exposed
}

// CheckSurveillanceExposure checks if Ring Bearer is exposed by crossing a high-surveillance path.
func CheckSurveillanceExposure(pathID string, paths map[string]state.PathState, turn, hiddenUntilTurn int) bool {
	if turn <= hiddenUntilTurn {
		return false
	}
	p, ok := paths[pathID]
	if !ok {
		return false
	}
	return p.SurveillanceLevel >= 1
}

// isSauronActive checks if the passive dark Maia's amplifier is active.
// The passive dark Maia (Sauron) is identified entirely by config properties:
//   Maia=true, Side=SHADOW, Indestructible=true, MaiaAbilityPaths=[] (no ability list)
// His amplifier is active when he is in his configured StartRegion (e.g. mordor).
// No unit ID or region ID string literals are used.
func isSauronActive(cache state.WorldStateCache) bool {
	for _, unit := range cache.Units {
		cfg := cache.UnitConfigs[unit.ID]
		if cfg.Maia &&
			cfg.Side == config.SideDark &&
			cfg.Indestructible &&
			len(cfg.MaiaAbilityPaths) == 0 &&
			unit.Region == cfg.StartRegion && // at home base — config-driven, no literal
			unit.Status == state.StatusActive {
			return true
		}
	}
	return false
}
