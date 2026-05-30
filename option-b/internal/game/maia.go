package game

import (
	"fmt"

	"github.com/lotr/option-b/internal/config"
	"github.com/lotr/option-b/internal/state"
)

// MaiaResult holds the outcome of a Maia ability.
type MaiaResult struct {
	PathID    string
	EventType string // "PATH_OPENED" | "PATH_CORRUPTED"
}

// DispatchMaiaAbility handles a MaiaAbility order.
// Dispatch is determined by config properties — NO unit ID string literals.
//
// Gandalf (maiaAbilityPaths=[], maia=true, side=FREE_PEOPLES) → OpenPath
// Saruman (maiaAbilityPaths=[...], maia=true, side=SHADOW)    → CorruptPath
// Sauron  (passive — never receives MaiaAbility orders)
func DispatchMaiaAbility(
	order MaiaAbilityOrder,
	cache *state.WorldStateCache,
	pathCfgs map[string]config.PathConfig,
) (*MaiaResult, error) {
	unit, ok := cache.Units[order.UnitID]
	if !ok {
		return nil, fmt.Errorf("unit not found: %s", order.UnitID)
	}
	cfg, ok := cache.UnitConfigs[order.UnitID]
	if !ok || !cfg.Maia {
		return nil, &ValidationError{ErrorCode: ErrInvalidTarget, Message: "unit is not Maia"}
	}
	if unit.Disabled {
		return nil, &ValidationError{ErrorCode: ErrMaiaDisabled, Message: "Maia is disabled"}
	}
	if unit.Cooldown > 0 {
		return nil, &ValidationError{ErrorCode: ErrAbilityOnCooldown, Message: "ability on cooldown"}
	}

	// Dispatch by config — not by ID
	if len(cfg.MaiaAbilityPaths) > 0 {
		// Has a restricted path list → Saruman-type: CorruptPath
		return applyCorruptPath(order, unit, cfg, cache, pathCfgs)
	}
	// No restricted paths + FREE_PEOPLES → Gandalf-type: OpenPath
	if cfg.Side == config.SideLight {
		return applyOpenPath(order, unit, cfg, cache, pathCfgs)
	}
	// Passive Maia (e.g. Sauron) — should never receive this order
	return nil, &ValidationError{ErrorCode: ErrInvalidTarget, Message: "passive Maia cannot use ability"}
}

func applyOpenPath(
	order MaiaAbilityOrder,
	unit state.UnitSnapshot,
	cfg config.UnitConfig,
	cache *state.WorldStateCache,
	pathCfgs map[string]config.PathConfig,
) (*MaiaResult, error) {
	path, ok := cache.Paths[order.TargetPathID]
	if !ok {
		return nil, &ValidationError{ErrorCode: ErrInvalidPath, Message: "path not found"}
	}
	if path.Status != state.PathBlocked {
		return nil, &ValidationError{ErrorCode: ErrInvalidTarget, Message: "OpenPath requires BLOCKED path"}
	}
	// Gandalf must be at one of the path's endpoint regions
	pc, ok := pathCfgs[order.TargetPathID]
	if !ok {
		return nil, &ValidationError{ErrorCode: ErrInvalidPath, Message: "path config not found"}
	}
	if unit.Region != pc.From && unit.Region != pc.To {
		return nil, &ValidationError{ErrorCode: ErrUnitNotAdjacent, Message: "Gandalf must be at a path endpoint to open it"}
	}

	path.Status = state.PathTemporarilyOpen
	path.TempOpenTurns = 2
	cache.Paths[order.TargetPathID] = path

	// Set cooldown
	u := cache.Units[order.UnitID]
	u.Cooldown = cfg.Cooldown
	cache.Units[order.UnitID] = u

	return &MaiaResult{PathID: order.TargetPathID, EventType: "PATH_OPENED"}, nil
}

func applyCorruptPath(
	order MaiaAbilityOrder,
	unit state.UnitSnapshot,
	cfg config.UnitConfig,
	cache *state.WorldStateCache,
	pathCfgs map[string]config.PathConfig,
) (*MaiaResult, error) {
	// Check path is in Saruman's allowed list
	allowed := false
	for _, pid := range cfg.MaiaAbilityPaths {
		if pid == order.TargetPathID {
			allowed = true
			break
		}
	}
	if !allowed {
		return nil, &ValidationError{ErrorCode: ErrInvalidPath, Message: "path not in Saruman's ability list"}
	}

	path, ok := cache.Paths[order.TargetPathID]
	if !ok {
		return nil, &ValidationError{ErrorCode: ErrInvalidPath, Message: "path not found"}
	}
	// CorruptPath works on OPEN, THREATENED, or BLOCKED
	if path.Status == state.PathTemporarilyOpen {
		return nil, &ValidationError{ErrorCode: ErrInvalidTarget, Message: "cannot corrupt temporarily open path"}
	}

	// Saruman must be at one of the path's endpoint regions
	pc, ok := pathCfgs[order.TargetPathID]
	if !ok {
		return nil, &ValidationError{ErrorCode: ErrInvalidPath, Message: "path config not found"}
	}
	if unit.Region != pc.From && unit.Region != pc.To {
		return nil, &ValidationError{ErrorCode: ErrUnitNotAdjacent, Message: "Saruman must be at a path endpoint to corrupt it"}
	}

	// Permanent surveillance level 3
	path.SurveillanceLevel = 3
	cache.Paths[order.TargetPathID] = path

	// Set cooldown
	u := cache.Units[order.UnitID]
	u.Cooldown = cfg.Cooldown
	cache.Units[order.UnitID] = u

	return &MaiaResult{PathID: order.TargetPathID, EventType: "PATH_CORRUPTED"}, nil
}
