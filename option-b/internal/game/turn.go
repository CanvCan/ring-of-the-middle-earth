package game

import (
	"fmt"

	"github.com/rotr/option-b/internal/config"
	"github.com/rotr/option-b/internal/state"
)

// TurnEvents holds all events emitted during a single turn.
type TurnEvents struct {
	UnitMoved              []UnitMovedEvent
	PathStatusChanged      []PathStatusChangedEvent
	PathCorrupted          []PathCorruptedEvent
	RegionControlChanged   []RegionControlChangedEvent
	BattleResolved         []BattleResolvedEvent
	RingBearerMoved        *RingBearerMovedEvent
	RingBearerDetected     []RingBearerDetectedEvent
	RingBearerSpotted      []RingBearerSpottedEvent
	RouteBlocked           []RouteBlockedEvent
	RouteCompromised       []RouteCompromisedEvent
	RouteComplete          []RouteCompleteEvent
	GameOver               *GameOverEvent
	WorldSnapshot          *WorldSnapshotEvent
}

type UnitMovedEvent           struct{ UnitID, From, To string; Turn int }
type PathStatusChangedEvent   struct{ PathID string; NewStatus state.PathStatus; SurveillanceLevel, TempOpenTurns, Turn int }
type PathCorruptedEvent       struct{ PathID string; Turn int }
type RegionControlChangedEvent struct{ RegionID, NewController string; Turn int }
type BattleResolvedEvent      struct{ RegionID string; AttackerWon bool; Turn int }
type RingBearerMovedEvent     struct{ TrueRegion string; Turn int }
type RingBearerDetectedEvent  struct{ RegionID string; Turn int }
type RingBearerSpottedEvent   struct{ PathID string; Turn int }
type RouteBlockedEvent        struct{ UnitID, PathID string; Turn int }
type RouteCompromisedEvent    struct{ UnitID string; Turn int }
type RouteCompleteEvent       struct{ UnitID string; Turn int }
type GameOverEvent            struct{ Winner, Cause string; Turn int }
type WorldSnapshotEvent       struct{ Turn int }

// OrderBatch is all validated orders for a single turn.
type OrderBatch struct {
	Turn           int
	AssignRoutes   []AssignRouteOrder
	Redirects      []RedirectUnitOrder
	BlockPaths     []BlockPathOrder
	SearchPaths    []SearchPathOrder
	Reinforces     []ReinforceRegionOrder
	DeployNazguls  []DeployNazgulOrder
	Fortifies      []FortifyRegionOrder
	MaiaAbilities  []MaiaAbilityOrder
	Attacks        []AttackRegionOrder
	DestroyRing    *DestroyRingOrder
}

// TurnProcessor executes the 13-step turn sequence.
type TurnProcessor struct {
	pathConfigs   map[string]config.PathConfig
	regionConfigs map[string]config.RegionConfig
}

func NewTurnProcessor(pathConfigs map[string]config.PathConfig, regionConfigs map[string]config.RegionConfig) *TurnProcessor {
	return &TurnProcessor{pathConfigs: pathConfigs, regionConfigs: regionConfigs}
}

// ProcessTurn executes all 13 steps and returns emitted events.
func (tp *TurnProcessor) ProcessTurn(cache *state.WorldStateCache, batch OrderBatch) (*TurnEvents, error) {
	events := &TurnEvents{}
	turn := batch.Turn

	// Step 1: Orders already collected in batch.

	// Step 2: AssignRoute and RedirectUnit
	for _, o := range batch.AssignRoutes {
		tp.applyAssignRoute(o, cache)
	}
	for _, o := range batch.Redirects {
		tp.applyRedirectUnit(o, cache)
	}

	// Step 3: BlockPath and SearchPath
	tp.revertBlockedPaths(cache)
	for _, o := range batch.BlockPaths {
		tp.applyBlockPath(o, cache, events, turn)
	}
	for _, o := range batch.SearchPaths {
		// SEARCH_PATH is exclusive to Nazgul class.
		if cache.UnitConfigs[o.UnitID].Class != config.ClassNazgul {
			continue
		}
		p := cache.Paths[o.PathID]
		if p.SurveillanceLevel < 3 {
			p.SurveillanceLevel++
		}
		cache.Paths[o.PathID] = p
		events.PathStatusChanged = append(events.PathStatusChanged, PathStatusChangedEvent{
			PathID: o.PathID, NewStatus: p.Status,
			SurveillanceLevel: p.SurveillanceLevel, Turn: turn,
		})
	}

	// Step 4: ReinforceRegion and DeployNazgul
	for _, o := range batch.Reinforces {
		tp.applyReinforce(o, cache)
	}
	for _, o := range batch.DeployNazguls {
		tp.applyDeployNazgul(o, cache, events, turn)
	}

	// Step 5: FortifyRegion
	for _, o := range batch.Fortifies {
		tp.applyFortify(o, cache)
	}

	// Step 6: MaiaAbility
	for _, o := range batch.MaiaAbilities {
		result, err := DispatchMaiaAbility(o, cache, tp.pathConfigs)
		if err != nil {
			continue
		}
		switch result.EventType {
		case "PATH_OPENED":
			p := cache.Paths[result.PathID]
			events.PathStatusChanged = append(events.PathStatusChanged, PathStatusChangedEvent{
				PathID: result.PathID, NewStatus: p.Status,
				TempOpenTurns: p.TempOpenTurns, Turn: turn,
			})
		case "PATH_CORRUPTED":
			events.PathCorrupted = append(events.PathCorrupted, PathCorruptedEvent{
				PathID: result.PathID, Turn: turn,
			})
		}
	}

	// Step 7: Auto-advance
	tp.autoAdvance(cache, events, turn)

	// Step 8: AttackRegion — deduplicate by (attacker region, target region)
	// Multiple units at the same region may all submit ATTACK_REGION against the same target,
	// but combat must only resolve once per (from, to) pair.
	seenAttacks := map[string]bool{}
	for _, o := range batch.Attacks {
		attacker := cache.Units[o.UnitID]
		attackKey := attacker.Region + "->" + o.TargetRegionID
		if seenAttacks[attackKey] {
			continue
		}
		seenAttacks[attackKey] = true
		tp.applyAttack(o, cache, events, turn)
	}

	// Step 9: Decrement TEMPORARILY_OPEN timers
	tp.decrementTempOpenTimers(cache, events, turn)

	// Step 10: Decrement fortification timers
	tp.decrementFortifyTimers(cache)

	// Step 11: Decrement respawn and cooldown counters
	tp.decrementCounters(cache, events, turn)

	// Step 12: Detection check
	// OR with current Exposed: surveillance exposure set in Step 7 must not be overwritten.
	detectionEvents, nazgulExposed := RunDetection(*cache, turn)
	cache.RingBearer.Exposed = cache.RingBearer.Exposed || nazgulExposed
	for _, de := range detectionEvents {
		events.RingBearerDetected = append(events.RingBearerDetected,
			RingBearerDetectedEvent{RegionID: de.Region, Turn: de.Turn})
	}

	// Step 13: Win conditions
	destroyRingSubmitted := batch.DestroyRing != nil
	result := EvaluateWinConditions(cache, turn, destroyRingSubmitted)
	if result.Over {
		cache.GameOver = true
		cache.Winner = result.Winner
		events.GameOver = &GameOverEvent{Winner: result.Winner, Cause: result.Cause, Turn: turn}
	}

	events.WorldSnapshot = &WorldSnapshotEvent{Turn: turn}

	// Reset exposed flag
	cache.RingBearer.Exposed = false
	cache.Turn++

	return events, nil
}

// ── Step helpers ─────────────────────────────────────────────────────────

func (tp *TurnProcessor) applyAssignRoute(o AssignRouteOrder, cache *state.WorldStateCache) {
	// Sauron never moves — reject at engine level.
	if isSauronUnit(o.UnitID, cache) {
		return
	}
	if isRingBearer(o.UnitID, cache) {
		cache.RingBearer.Route = o.PathIDs
		cache.RingBearer.RouteIdx = 0
		lv := cache.LightView
		lv.AssignedRoute = o.PathIDs
		lv.RouteIdx = 0
		cache.LightView = lv
		return
	}
	u := cache.Units[o.UnitID]
	u.Route = o.PathIDs
	u.RouteIdx = 0
	cache.Units[o.UnitID] = u
}

func (tp *TurnProcessor) applyRedirectUnit(o RedirectUnitOrder, cache *state.WorldStateCache) {
	// Sauron never moves — reject at engine level.
	if isSauronUnit(o.UnitID, cache) {
		return
	}
	if isRingBearer(o.UnitID, cache) {
		cache.RingBearer.Route = o.NewPathIDs
		cache.RingBearer.RouteIdx = 0
		lv := cache.LightView
		lv.AssignedRoute = o.NewPathIDs
		lv.RouteIdx = 0
		cache.LightView = lv
		return
	}
	u := cache.Units[o.UnitID]
	u.Route = o.NewPathIDs
	u.RouteIdx = 0
	cache.Units[o.UnitID] = u
}

func (tp *TurnProcessor) revertBlockedPaths(cache *state.WorldStateCache) {
	for id, path := range cache.Paths {
		if path.Status != state.PathBlocked || path.BlockedBy == "" {
			continue
		}
		blocker, exists := cache.Units[path.BlockedBy]
		if !exists || blocker.Status != state.StatusActive {
			path.Status = revertStatus(path.PreviousStatus)
			path.BlockedBy = ""
			cache.Paths[id] = path
			continue
		}
		pc, ok := tp.pathConfigs[id]
		if !ok {
			continue
		}
		if blocker.Region != pc.From && blocker.Region != pc.To {
			path.Status = revertStatus(path.PreviousStatus)
			path.BlockedBy = ""
			cache.Paths[id] = path
		}
	}
}

// revertStatus returns the status a path should revert to after its blocker leaves.
// Preserves THREATENED if the path was threatened before being blocked; otherwise OPEN.
func revertStatus(previous state.PathStatus) state.PathStatus {
	if previous == state.PathThreatened {
		return state.PathThreatened
	}
	return state.PathOpen
}

func (tp *TurnProcessor) applyBlockPath(o BlockPathOrder, cache *state.WorldStateCache, events *TurnEvents, turn int) {
	// Only FellowshipGuard, Nazgul, and UrukHaiLegion may block paths.
	cfg := cache.UnitConfigs[o.UnitID]
	if !canBlockPath(cfg.Class) {
		return
	}
	unit := cache.Units[o.UnitID]
	pc, ok := tp.pathConfigs[o.PathID]
	if !ok {
		return
	}
	if unit.Region != pc.From && unit.Region != pc.To {
		return
	}

	// Dark units (Nazgul, UrukHaiLegion) cannot block a path while an opposing
	// FellowshipGuard holds either endpoint — the guard physically denies access.
	// Identified by class and side from config; no unit-ID literals.
	if cfg.Side == config.SideDark {
		for _, u := range cache.Units {
			uc := cache.UnitConfigs[u.ID]
			if uc.Class == config.ClassFellowshipGuard &&
				u.Status == state.StatusActive &&
				(u.Region == pc.From || u.Region == pc.To) {
				return // block fails — FellowshipGuard defends this path endpoint
			}
		}
	}

	path := cache.Paths[o.PathID]
	path.PreviousStatus = path.Status // save for revert (OPEN or THREATENED)
	path.Status = state.PathBlocked
	path.BlockedBy = o.UnitID
	cache.Paths[o.PathID] = path

	events.PathStatusChanged = append(events.PathStatusChanged, PathStatusChangedEvent{
		PathID: o.PathID, NewStatus: state.PathBlocked,
		SurveillanceLevel: path.SurveillanceLevel, Turn: turn,
	})

	for uid, u := range cache.Units {
		if routeContains(u.Route, o.PathID) {
			events.RouteCompromised = append(events.RouteCompromised, RouteCompromisedEvent{UnitID: uid, Turn: turn})
		}
	}
	if routeContains(cache.RingBearer.Route, o.PathID) {
		// Look up ring bearer unit ID by class — no hardcoded string.
		if rbID := findRingBearerID(cache); rbID != "" {
			events.RouteCompromised = append(events.RouteCompromised, RouteCompromisedEvent{UnitID: rbID, Turn: turn})
		}
	}
}

func (tp *TurnProcessor) applyReinforce(o ReinforceRegionOrder, cache *state.WorldStateCache) {
	cfg := cache.UnitConfigs[o.UnitID]
	// RingBearer and Sauron cannot reinforce regions.
	if cfg.Class == config.ClassRingBearer || isSauronUnit(o.UnitID, cache) {
		return
	}
	region := cache.Regions[o.TargetRegionID]
	if cfg.Side == config.SideLight {
		region.Control = state.ControlFree
	} else {
		region.Control = state.ControlShadow
	}
	cache.Regions[o.TargetRegionID] = region
}

func (tp *TurnProcessor) applyDeployNazgul(o DeployNazgulOrder, cache *state.WorldStateCache, events *TurnEvents, turn int) {
	cfg := cache.UnitConfigs[o.UnitID]
	// Only Nazgul may be deployed — identified by config class, no hardcoded unit ID.
	if cfg.Class != config.ClassNazgul {
		return
	}
	unit := cache.Units[o.UnitID]
	from := unit.Region
	tp.removeUnitFromRegion(from, o.UnitID, cache)
	unit.Region = o.TargetRegionID
	cache.Units[o.UnitID] = unit
	tp.addUnitToRegion(o.TargetRegionID, o.UnitID, cache)

	// Emit UnitMoved event so game.events.unit KTable stays current (spec §28, §34).
	events.UnitMoved = append(events.UnitMoved, UnitMovedEvent{
		UnitID: o.UnitID,
		From:   from,
		To:     o.TargetRegionID,
		Turn:   turn,
	})
}

func (tp *TurnProcessor) applyFortify(o FortifyRegionOrder, cache *state.WorldStateCache) {
	cfg := cache.UnitConfigs[o.UnitID]
	if !cfg.CanFortify {
		return
	}
	unit := cache.Units[o.UnitID]
	region := cache.Regions[unit.Region]
	region.Fortified = true
	region.FortifyTurns = 2
	cache.Regions[unit.Region] = region
}

func (tp *TurnProcessor) autoAdvance(cache *state.WorldStateCache, events *TurnEvents, turn int) {
	// No hardcoded unit IDs — detect ring bearer by class from config.
	for uid := range cache.Units {
		isRB := isRingBearer(uid, cache)
		tp.advanceUnit(uid, isRB, cache, events, turn)
	}
}

func (tp *TurnProcessor) advanceUnit(uid string, isRB bool, cache *state.WorldStateCache, events *TurnEvents, turn int) {
	var route []string
	var routeIdx int
	var currentRegion string
	var remainingCost int

	if isRB {
		route = cache.RingBearer.Route
		routeIdx = cache.RingBearer.RouteIdx
		currentRegion = cache.RingBearer.TrueRegion
		remainingCost = cache.RingBearer.RemainingPathCost
	} else {
		u := cache.Units[uid]
		if u.Status != state.StatusActive {
			return
		}
		route = u.Route
		routeIdx = u.RouteIdx
		currentRegion = u.Region
		remainingCost = u.RemainingPathCost
	}

	if len(route) == 0 || routeIdx >= len(route) {
		return
	}

	nextPathID := route[routeIdx]
	path := cache.Paths[nextPathID]

	if path.Status == state.PathBlocked {
		events.RouteBlocked = append(events.RouteBlocked,
			RouteBlockedEvent{UnitID: uid, PathID: nextPathID, Turn: turn})
		return
	}

	pc := tp.pathConfigs[nextPathID]

	// Cost-based movement:
	// If remainingCost is 0 it means we just arrived at this path — load its cost.
	// If remainingCost > 1 the unit is still traversing — decrement and wait.
	// If remainingCost == 1 the unit crosses the path this turn.
	if remainingCost == 0 {
		// First turn on this path — load cost from config
		edgeCost := pc.Cost
		if edgeCost <= 0 {
			edgeCost = 1
		}
		remainingCost = edgeCost
	}

	if remainingCost > 1 {
		// Still traversing — burn one turn, stay put
		remainingCost--
		if isRB {
			rb := cache.RingBearer
			rb.RemainingPathCost = remainingCost
			cache.RingBearer = rb
		} else {
			u := cache.Units[uid]
			u.RemainingPathCost = remainingCost
			cache.Units[uid] = u
		}
		return
	}

	// remainingCost == 1: cross the path this turn
	dest := pc.To
	if currentRegion == pc.To {
		dest = pc.From
	}

	if isRB {
		rb := cache.RingBearer
		rb.TrueRegion = dest
		rb.RouteIdx++
		rb.RemainingPathCost = 0 // reset for next path
		cache.RingBearer = rb
		cache.LightView.RingBearerRegion = dest
		cache.LightView.RouteIdx = rb.RouteIdx

		if path.SurveillanceLevel >= 1 && turn > cache.HiddenUntilTurn {
			cache.RingBearer.Exposed = true
			events.RingBearerSpotted = append(events.RingBearerSpotted,
				RingBearerSpottedEvent{PathID: nextPathID, Turn: turn})
		}
		events.RingBearerMoved = &RingBearerMovedEvent{TrueRegion: dest, Turn: turn}
		if rb.RouteIdx >= len(route) {
			events.RouteComplete = append(events.RouteComplete,
				RouteCompleteEvent{UnitID: uid, Turn: turn})
		}
	} else {
		u := cache.Units[uid]
		from := u.Region
		tp.removeUnitFromRegion(from, uid, cache)
		u.Region = dest
		u.RouteIdx++
		u.RemainingPathCost = 0 // reset for next path
		cache.Units[uid] = u
		tp.addUnitToRegion(dest, uid, cache)

		events.UnitMoved = append(events.UnitMoved,
			UnitMovedEvent{UnitID: uid, From: from, To: dest, Turn: turn})
		if u.RouteIdx >= len(route) {
			events.RouteComplete = append(events.RouteComplete,
				RouteCompleteEvent{UnitID: uid, Turn: turn})
		}
	}
}

func (tp *TurnProcessor) applyAttack(o AttackRegionOrder, cache *state.WorldStateCache, events *TurnEvents, turn int) {
	attackerCfg := cache.UnitConfigs[o.UnitID]
	attackerUnit := cache.Units[o.UnitID]

	// RingBearer and Sauron cannot attack — enforce at engine level regardless of order source.
	if attackerCfg.Class == config.ClassRingBearer || isSauronUnit(o.UnitID, cache) {
		return
	}

	// Validate adjacency: target must be directly connected to attacker's region.
	// Topology1 also enforces this; engine validates defensively in case of Kafka bypass.
	if cache.Graph.Distance(attackerUnit.Region, o.TargetRegionID) != 1 {
		return
	}

	var attackerIDs, defenderIDs []string
	for uid, u := range cache.Units {
		cfg := cache.UnitConfigs[uid]
		if u.Status != state.StatusActive {
			continue
		}
		if cfg.Side == attackerCfg.Side && u.Region == attackerUnit.Region {
			attackerIDs = append(attackerIDs, uid)
		}
		if cfg.Side != attackerCfg.Side && u.Region == o.TargetRegionID {
			defenderIDs = append(defenderIDs, uid)
		}
	}

	if len(defenderIDs) == 0 {
		tp.removeUnitFromRegion(attackerUnit.Region, o.UnitID, cache)
		u := cache.Units[o.UnitID]
		u.Region = o.TargetRegionID
		cache.Units[o.UnitID] = u
		tp.addUnitToRegion(o.TargetRegionID, o.UnitID, cache)
		return
	}

	targetRegion := cache.Regions[o.TargetRegionID]
	terrain := ""
	if rc, ok := tp.regionConfigs[o.TargetRegionID]; ok {
		terrain = rc.Terrain
	}

	attackPow := tp.calcGroupPower(attackerIDs, cache)
	defPow := tp.calcGroupPower(defenderIDs, cache)
	defPow += TerrainBonusForTerrain(terrain, attackerIDs, cache.UnitConfigs)
	defPow += fortificationBonus(targetRegion)

	attackerWon := attackPow > defPow
	events.BattleResolved = append(events.BattleResolved,
		BattleResolvedEvent{RegionID: o.TargetRegionID, AttackerWon: attackerWon, Turn: turn})

	if attackerWon {
		damage := attackPow - defPow
		for _, did := range defenderIDs {
			u := cache.Units[did]
			cfg := cache.UnitConfigs[did]
			u = ApplyDamage(u, cfg, damage)
			cache.Units[did] = u
		}
		region := cache.Regions[o.TargetRegionID]
		if attackerCfg.Side == config.SideLight {
			region.Control = state.ControlFree
		} else {
			region.Control = state.ControlShadow
		}
		region.Fortified = false
		cache.Regions[o.TargetRegionID] = region

		events.RegionControlChanged = append(events.RegionControlChanged,
			RegionControlChangedEvent{RegionID: o.TargetRegionID, NewController: string(region.Control), Turn: turn})

		// If a Dark Maia with ability paths has its start region taken by Light Side,
		// disable that Maia (Saruman-type). Config-driven — no region ID hardcoded.
		if attackerCfg.Side == config.SideLight {
			for _, ucfg := range cache.UnitConfigs {
				if ucfg.Maia && ucfg.Side == config.SideDark &&
					len(ucfg.MaiaAbilityPaths) > 0 &&
					ucfg.StartRegion == o.TargetRegionID {
					tp.disableSaruman(cache)
					break
				}
			}
		}
	} else {
		for _, aid := range attackerIDs {
			u := cache.Units[aid]
			cfg := cache.UnitConfigs[aid]
			u = ApplyDamage(u, cfg, 1)
			cache.Units[aid] = u
		}
	}
}

func (tp *TurnProcessor) disableSaruman(cache *state.WorldStateCache) {
	for uid, cfg := range cache.UnitConfigs {
		if cfg.Maia && cfg.Side == config.SideDark && len(cfg.MaiaAbilityPaths) > 0 {
			u := cache.Units[uid]
			u.Disabled = true
			cache.Units[uid] = u
		}
	}
}

func (tp *TurnProcessor) decrementTempOpenTimers(cache *state.WorldStateCache, events *TurnEvents, turn int) {
	for id, path := range cache.Paths {
		if path.Status != state.PathTemporarilyOpen {
			continue
		}
		path.TempOpenTurns--
		if path.TempOpenTurns <= 0 {
			pc := tp.pathConfigs[id]
			blockerThere := false
			if path.BlockedBy != "" {
				if b, ok := cache.Units[path.BlockedBy]; ok {
					blockerThere = (b.Region == pc.From || b.Region == pc.To) && b.Status == state.StatusActive
				}
			}
			if blockerThere {
				path.Status = state.PathBlocked
			} else {
				path.Status = state.PathOpen
				path.BlockedBy = ""
			}
			events.PathStatusChanged = append(events.PathStatusChanged, PathStatusChangedEvent{
				PathID: id, NewStatus: path.Status,
				SurveillanceLevel: path.SurveillanceLevel, Turn: turn,
			})
		}
		cache.Paths[id] = path
	}
}

func (tp *TurnProcessor) decrementFortifyTimers(cache *state.WorldStateCache) {
	for id, region := range cache.Regions {
		if !region.Fortified {
			continue
		}
		region.FortifyTurns--
		if region.FortifyTurns <= 0 {
			region.Fortified = false
		}
		cache.Regions[id] = region
	}
}

func (tp *TurnProcessor) decrementCounters(cache *state.WorldStateCache, events *TurnEvents, turn int) {
	for uid, u := range cache.Units {
		cfg := cache.UnitConfigs[uid]
		changed := false
		if u.Cooldown > 0 {
			u.Cooldown--
			changed = true
		}
		if u.Status == state.StatusRespawning {
			u.RespawnTurns--
			if u.RespawnTurns <= 0 {
				u.Status = state.StatusActive
				u.Strength = cfg.Strength
				u.Region = cfg.StartRegion
				u.RespawnTurns = 0
				tp.addUnitToRegion(cfg.StartRegion, uid, cache)
				events.UnitMoved = append(events.UnitMoved,
					UnitMovedEvent{UnitID: uid, From: "", To: cfg.StartRegion, Turn: turn})
			}
			changed = true
		}
		if changed {
			cache.Units[uid] = u
		}
	}
}

func (tp *TurnProcessor) calcGroupPower(unitIDs []string, cache *state.WorldStateCache) int {
	total := 0
	for _, id := range unitIDs {
		u := cache.Units[id]
		total += u.Strength
		total += leadershipBonus(id, unitIDs, cache.UnitConfigs)
	}
	return total
}

func (tp *TurnProcessor) addUnitToRegion(regionID, unitID string, cache *state.WorldStateCache) {
	region := cache.Regions[regionID]
	for _, id := range region.UnitsPresent {
		if id == unitID {
			return
		}
	}
	region.UnitsPresent = append(region.UnitsPresent, unitID)
	cache.Regions[regionID] = region
}

func (tp *TurnProcessor) removeUnitFromRegion(regionID, unitID string, cache *state.WorldStateCache) {
	region := cache.Regions[regionID]
	updated := region.UnitsPresent[:0]
	for _, id := range region.UnitsPresent {
		if id != unitID {
			updated = append(updated, id)
		}
	}
	region.UnitsPresent = updated
	cache.Regions[regionID] = region
}

func routeContains(route []string, pathID string) bool {
	for _, p := range route {
		if p == pathID {
			return true
		}
	}
	return false
}

func isRingBearer(unitID string, cache *state.WorldStateCache) bool {
	cfg, ok := cache.UnitConfigs[unitID]
	if !ok {
		return false
	}
	return cfg.Class == config.ClassRingBearer
}

// isSauronUnit detects the passive Maia by config properties — no unit ID hardcoding.
// Sauron = Maia + SHADOW + Indestructible + no MaiaAbilityPaths.
func isSauronUnit(unitID string, cache *state.WorldStateCache) bool {
	cfg, ok := cache.UnitConfigs[unitID]
	if !ok {
		return false
	}
	return cfg.Maia && cfg.Side == config.SideDark &&
		cfg.Indestructible && len(cfg.MaiaAbilityPaths) == 0
}

// canBlockPath returns true for classes allowed to issue BLOCK_PATH orders.
// FellowshipGuard, Nazgul, UrukHaiLegion — per rulebook.
func canBlockPath(class string) bool {
	return class == config.ClassFellowshipGuard ||
		class == config.ClassNazgul ||
		class == config.ClassUrukHaiLegion
}

// findRingBearerID returns the unit ID of the RingBearer from config.
// Never hardcodes a string — detects by class.
func findRingBearerID(cache *state.WorldStateCache) string {
	for uid, cfg := range cache.UnitConfigs {
		if cfg.Class == config.ClassRingBearer {
			return uid
		}
	}
	return ""
}

// InitCache builds the initial WorldStateCache from config.
func InitCache(cfg *config.Config) state.WorldStateCache {
	units := make(map[string]state.UnitSnapshot, len(cfg.Game.Units))
	regions := make(map[string]state.RegionState, len(cfg.Map.Regions))
	paths := make(map[string]state.PathState, len(cfg.Map.Paths))
	unitConfigs := make(map[string]config.UnitConfig, len(cfg.Game.Units))

	var rbState state.RingBearerState

	for _, u := range cfg.Game.Units {
		unitConfigs[u.ID] = u
		snap := state.UnitSnapshot{
			ID: u.ID, Region: u.StartRegion,
			Strength: u.Strength, Status: state.StatusActive,
		}
		if u.Class == config.ClassRingBearer {
			rbState.TrueRegion = u.StartRegion
			snap.Region = ""
		}
		units[u.ID] = snap
	}

	for _, r := range cfg.Map.Regions {
		ctrl := state.ControlNeutral
		switch r.StartControl {
		case "FREE_PEOPLES":
			ctrl = state.ControlFree
		case "SHADOW":
			ctrl = state.ControlShadow
		}
		regions[r.ID] = state.RegionState{
			ID: r.ID, Control: ctrl, ThreatLevel: r.StartThreat,
		}
	}

	for _, p := range cfg.Map.Paths {
		paths[p.ID] = state.PathState{ID: p.ID, Status: state.PathOpen}
	}

	for uid, u := range units {
		ucfg := unitConfigs[uid]
		// Ring Bearer's true region is tracked via RingBearerState, not UnitsPresent.
		// Adding it to UnitsPresent would leak its location to the Dark Side via region data.
		if ucfg.Class == config.ClassRingBearer {
			_ = u
			continue
		}
		startRegion := ucfg.StartRegion
		r := regions[startRegion]
		r.UnitsPresent = append(r.UnitsPresent, uid)
		regions[startRegion] = r
		_ = u
	}

	// Discover ring destruction site from region config — no hardcoded region ID.
	ringDestructionSiteID := ""
	for _, r := range cfg.Map.Regions {
		if r.SpecialRole == config.RoleRingDestructionSite {
			ringDestructionSiteID = r.ID
			break
		}
	}

	return state.WorldStateCache{
		Turn:                  1,
		MaxTurns:              cfg.Game.MaxTurns,
		HiddenUntilTurn:       cfg.Game.HiddenUntilTurn,
		RingDestructionSiteID: ringDestructionSiteID,
		Units:                 units,
		Regions:               regions,
		Paths:                 paths,
		UnitConfigs:           unitConfigs,
		Graph:                 config.BuildGraph(cfg.Map.Paths),
		RingBearer:            rbState,
		DarkView:              state.DarkSideView{RingBearerRegion: ""},
	}
}

// ValidateBatch checks duplicate unit orders.
func ValidateBatch(batch OrderBatch, currentTurn int) []ValidationError {
	var errs []ValidationError
	seen := map[string]bool{}
	check := func(unitID string) {
		if seen[unitID] {
			errs = append(errs, ValidationError{ErrorCode: ErrDuplicateUnitOrder,
				Message: fmt.Sprintf("unit %s has multiple orders this turn", unitID)})
		}
		seen[unitID] = true
	}
	for _, o := range batch.AssignRoutes {
		check(o.UnitID)
	}
	for _, o := range batch.Redirects {
		check(o.UnitID)
	}
	for _, o := range batch.BlockPaths {
		check(o.UnitID)
	}
	for _, o := range batch.SearchPaths {
		check(o.UnitID)
	}
	for _, o := range batch.Attacks {
		check(o.UnitID)
	}
	for _, o := range batch.Fortifies {
		check(o.UnitID)
	}
	for _, o := range batch.MaiaAbilities {
		check(o.UnitID)
	}
	for _, o := range batch.DeployNazguls {
		check(o.UnitID)
	}
	for _, o := range batch.Reinforces {
		check(o.UnitID)
	}
	return errs
}
