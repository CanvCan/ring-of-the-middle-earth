package state

import (
	"context"
	"sync"
)

// CacheManager owns WorldStateCache and serves value copies to consumers.
// It NEVER sends pointers — all outbound snapshots are deep copies.
type CacheManager struct {
	mu    sync.RWMutex
	cache WorldStateCache
}

func NewCacheManager(initial WorldStateCache) *CacheManager {
	return &CacheManager{cache: initial}
}

// Get returns a deep copy of the current cache.
func (cm *CacheManager) Get() WorldStateCache {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.deepCopy()
}

// Update applies a mutation function under write lock.
func (cm *CacheManager) Update(fn func(*WorldStateCache)) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	fn(&cm.cache)
	// Invariant: DarkView.RingBearerRegion must always be ""
	cm.cache.DarkView.RingBearerRegion = ""
}

// Run starts the CacheManager goroutine, applying updates from updateCh
// and sending snapshots on snapshotCh when requested.
func (cm *CacheManager) Run(ctx context.Context, updateCh <-chan func(*WorldStateCache)) {
	for {
		select {
		case fn, ok := <-updateCh:
			if !ok {
				return
			}
			cm.Update(fn)
		case <-ctx.Done():
			return
		}
	}
}

// deepCopy creates a full value copy of the cache.
// Maps are copied to prevent data races.
func (cm *CacheManager) deepCopy() WorldStateCache {
	c := cm.cache

	// Copy Units
	units := make(map[string]UnitSnapshot, len(c.Units))
	for k, v := range c.Units {
		if v.Route != nil {
			r := make([]string, len(v.Route))
			copy(r, v.Route)
			v.Route = r
		}
		units[k] = v
	}
	c.Units = units

	// Copy Regions
	regions := make(map[string]RegionState, len(c.Regions))
	for k, v := range c.Regions {
		if v.UnitsPresent != nil {
			up := make([]string, len(v.UnitsPresent))
			copy(up, v.UnitsPresent)
			v.UnitsPresent = up
		}
		regions[k] = v
	}
	c.Regions = regions

	// Copy Paths
	paths := make(map[string]PathState, len(c.Paths))
	for k, v := range c.Paths {
		paths[k] = v
	}
	c.Paths = paths

	// RingBearer route copy
	if c.RingBearer.Route != nil {
		r := make([]string, len(c.RingBearer.Route))
		copy(r, c.RingBearer.Route)
		c.RingBearer.Route = r
	}

	// LightView route copy
	if c.LightView.AssignedRoute != nil {
		r := make([]string, len(c.LightView.AssignedRoute))
		copy(r, c.LightView.AssignedRoute)
		c.LightView.AssignedRoute = r
	}

	// DarkView — enforce invariant
	c.DarkView.RingBearerRegion = ""

	// UnitConfigs and Graph are read-only after startup — safe to share pointer
	return c
}
