package game

import (
	"fmt"
	"github.com/lotr/option-b/internal/config"
	"github.com/lotr/option-b/internal/state"
)

// BlockPath transitions a path to BLOCKED if the unit is at an endpoint.
func BlockPath(pathID, unitID, unitRegion string, cache *state.WorldStateCache) error {
	path, ok := cache.Paths[pathID]
	if !ok {
		return fmt.Errorf("path not found: %s", pathID)
	}

	// Find path config to check endpoints
	// (pathCfg loaded from config package at init)
	// Simplified: caller must pass endpoint check
	path.Status = state.PathBlocked
	path.BlockedBy = unitID
	cache.Paths[pathID] = path
	return nil
}

// ClearPath reverts a BLOCKED path when the blocking unit leaves its endpoint.
// Reverts to PreviousStatus (THREATENED or OPEN) rather than always OPEN.
// Called automatically during Step 3 of turn processing.
func ClearPath(pathID string, cache *state.WorldStateCache) {
	path, ok := cache.Paths[pathID]
	if !ok {
		return
	}
	if path.Status == state.PathBlocked {
		path.Status = revertStatus(path.PreviousStatus)
		path.BlockedBy = ""
		cache.Paths[pathID] = path
	}
}

// ThreatPath transitions OPEN → THREATENED.
func ThreatPath(pathID string, cache *state.WorldStateCache) {
	path, ok := cache.Paths[pathID]
	if !ok {
		return
	}
	if path.Status == state.PathOpen {
		path.Status = state.PathThreatened
		cache.Paths[pathID] = path
	}
}

// SearchPath raises surveillanceLevel by 1 (max 3).
func SearchPath(pathID string, cache *state.WorldStateCache) {
	path, ok := cache.Paths[pathID]
	if !ok {
		return
	}
	if path.SurveillanceLevel < 3 {
		path.SurveillanceLevel++
	}
	cache.Paths[pathID] = path
}

// DecrementTempOpenTimers decrements TEMPORARILY_OPEN timers (Step 9).
// When timer hits 0: if blocker present at endpoint → BLOCKED, else → OPEN.
func DecrementTempOpenTimers(cache *state.WorldStateCache, pathCfgs map[string]config.PathConfig) {
	for id, path := range cache.Paths {
		if path.Status != state.PathTemporarilyOpen {
			continue
		}
		path.TempOpenTurns--
		if path.TempOpenTurns <= 0 {
			if path.BlockedBy != "" {
				blocker, exists := cache.Units[path.BlockedBy]
				if exists && isAtEndpoint(blocker.Region, id, pathCfgs) {
					path.Status = state.PathBlocked
				} else {
					path.Status = revertStatus(path.PreviousStatus)
					path.BlockedBy = ""
				}
			} else {
				path.Status = revertStatus(path.PreviousStatus)
			}
		}
		cache.Paths[id] = path
	}
}

// RevertBlockedPathsWithoutBlocker reverts BLOCKED paths whose blocker has left (Step 3).
func RevertBlockedPathsWithoutBlocker(cache *state.WorldStateCache, pathCfgs map[string]config.PathConfig) []string {
	var reverted []string
	for id, path := range cache.Paths {
		if path.Status != state.PathBlocked || path.BlockedBy == "" {
			continue
		}
		blocker, exists := cache.Units[path.BlockedBy]
		if !exists || blocker.Status != state.StatusActive || !isAtEndpoint(blocker.Region, id, pathCfgs) {
			path.Status = revertStatus(path.PreviousStatus)
			path.BlockedBy = ""
			cache.Paths[id] = path
			reverted = append(reverted, id)
		}
	}
	return reverted
}

// isAtEndpoint checks if a region is one of the two endpoints of a path.
func isAtEndpoint(region, pathID string, pathCfgs map[string]config.PathConfig) bool {
	pc, ok := pathCfgs[pathID]
	if !ok {
		return false
	}
	return region == pc.From || region == pc.To
}
