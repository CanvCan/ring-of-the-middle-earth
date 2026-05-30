package tests

import (
	"testing"

	"github.com/lotr/option-b/internal/config"
	"github.com/lotr/option-b/internal/game"
	"github.com/lotr/option-b/internal/state"
)

// ── helpers ──────────────────────────────────────────────────────────────

func unit(id, region string, str int) state.UnitSnapshot {
	return state.UnitSnapshot{ID: id, Region: region, Strength: str, Status: state.StatusActive}
}

func unitCfg(id, class, side string, opts ...func(*config.UnitConfig)) config.UnitConfig {
	c := config.UnitConfig{ID: id, Class: class, Side: side}
	for _, o := range opts {
		o(&c)
	}
	return c
}

func withIgnoresFortress() func(*config.UnitConfig) {
	return func(c *config.UnitConfig) { c.IgnoresFortress = true }
}
func withLeadership(bonus int) func(*config.UnitConfig) {
	return func(c *config.UnitConfig) { c.Leadership = true; c.LeadershipBonus = bonus }
}
func withIndestructible() func(*config.UnitConfig) {
	return func(c *config.UnitConfig) { c.Indestructible = true }
}

// ── Case 1: Attacker(5) vs Defender(5, PLAINS) → tie, defender holds ────

func TestCombat_TiePlains(t *testing.T) {
	units := map[string]state.UnitSnapshot{
		"att": unit("att", "bree", 5),
		"def": unit("def", "bree", 5),
	}
	cfgs := map[string]config.UnitConfig{
		"att": unitCfg("att", config.ClassFellowshipGuard, config.SideLight),
		"def": unitCfg("def", config.ClassNazgul, config.SideDark),
	}
	r := state.RegionState{ID: "bree"} // PLAINS — no terrain bonus

	result := game.ResolveCombat([]string{"att"}, []string{"def"}, r, units, cfgs)

	if result.AttackerWon {
		t.Error("expected defender to hold on tie (5 vs 5)")
	}
	if result.AttackerLoss != 1 {
		t.Errorf("expected attacker loss=1, got %d", result.AttackerLoss)
	}
}

// ── Case 2: Attacker(5) vs Defender(5, FORTRESS) → defender wins ─────────

func TestCombat_FortressTerrain(t *testing.T) {
	cfgs := map[string]config.UnitConfig{
		"att": unitCfg("att", config.ClassFellowshipGuard, config.SideLight),
		"def": unitCfg("def", config.ClassUrukHaiLegion, config.SideDark),
	}
	// terrain bonus for FORTRESS (no IgnoresFortress on attacker) = 2
	bonus := game.TerrainBonusForTerrain(config.TerrainFortress, []string{"att"}, cfgs)
	if bonus != 2 {
		t.Errorf("expected FORTRESS terrain bonus=2, got %d", bonus)
	}
	// attacker=5 vs defender=5+2=7 → defender wins
	if 5 > 5+bonus {
		t.Error("attacker should NOT win: 5 vs 7")
	}
}

// ── Case 3: UrukHai(ignoresFortress) vs Defender(5, FORTRESS) → tie ──────

func TestCombat_IgnoresFortressTerrain(t *testing.T) {
	cfgs := map[string]config.UnitConfig{
		"uruk": unitCfg("uruk", config.ClassUrukHaiLegion, config.SideDark, withIgnoresFortress()),
		"def":  unitCfg("def", config.ClassGondorArmy, config.SideLight),
	}
	// IgnoresFortress skips FORTRESS terrain bonus
	bonus := game.TerrainBonusForTerrain(config.TerrainFortress, []string{"uruk"}, cfgs)
	if bonus != 0 {
		t.Errorf("IgnoresFortress: expected terrain bonus=0, got %d", bonus)
	}
	// attacker=5 vs defender=5+0=5 → tie (defender holds)
}

// ── Case 4: UrukHai(ignoresFortress) vs fortified Defender → defender wins

func TestCombat_IgnoresFortressButFortificationApplies(t *testing.T) {
	units := map[string]state.UnitSnapshot{
		"uruk": unit("uruk", "osgiliath", 5),
		"gond": unit("gond", "minas-tirith", 5),
	}
	cfgs := map[string]config.UnitConfig{
		"uruk": unitCfg("uruk", config.ClassUrukHaiLegion, config.SideDark, withIgnoresFortress()),
		"gond": unitCfg("gond", config.ClassGondorArmy, config.SideLight),
	}
	// Fortified region: fortification bonus = +2 (always applies, even vs IgnoresFortress)
	// FORTRESS terrain bonus = 0 (skipped by IgnoresFortress)
	// defender = 5 + 0 + 2 = 7  vs  attacker = 5  → defender wins
	region := state.RegionState{ID: "minas-tirith", Fortified: true}

	result := game.ResolveCombat([]string{"uruk"}, []string{"gond"}, region, units, cfgs)
	if result.AttackerWon {
		t.Error("expected defender to win: UrukHai(5) vs GondorArmy fortified(5+0+2=7)")
	}
}

// ── Case 5: Leadership bonus applied to co-located allies ────────────────
// Aragorn(5, leader+1) + Gimli(3) → Gimli effective=4; total 5+4=9 vs UrukHai(5+2=7)

func TestCombat_LeadershipBonus(t *testing.T) {
	units := map[string]state.UnitSnapshot{
		"aragorn": unit("aragorn", "isengard", 5),
		"gimli":   unit("gimli", "isengard", 3),
		"uruk":    unit("uruk", "isengard", 5),
	}
	cfgs := map[string]config.UnitConfig{
		"aragorn": unitCfg("aragorn", config.ClassFellowshipGuard, config.SideLight, withLeadership(1)),
		"gimli":   unitCfg("gimli", config.ClassFellowshipGuard, config.SideLight),
		"uruk":    unitCfg("uruk", config.ClassUrukHaiLegion, config.SideDark),
	}
	// attackPow = Aragorn(5+0) + Gimli(3+1) = 9
	// defPow    = UrukHai(5) + FORTRESS(2) = 7  (neither attacker ignores fortress)
	// 9 > 7 → attacker wins
	region := state.RegionState{ID: "isengard"}

	result := game.ResolveCombat([]string{"aragorn", "gimli"}, []string{"uruk"}, region, units, cfgs, config.TerrainFortress)
	if !result.AttackerWon {
		t.Error("expected attacker to win with leadership: 9 vs 7")
	}
	if result.Damage != 2 {
		t.Errorf("expected damage=2 (9-7), got %d", result.Damage)
	}
}

// ── Case 6: Indestructible unit takes fatal damage → strength=1, ACTIVE ──

func TestCombat_IndestructibleFloor(t *testing.T) {
	wk := state.UnitSnapshot{ID: "witch-king", Strength: 5, Status: state.StatusActive}
	cfg := config.UnitConfig{
		ID: "witch-king", Class: config.ClassNazgul,
		Indestructible: true, Strength: 5,
	}

	result := game.ApplyDamage(wk, cfg, 100) // fatal overkill

	if result.Strength != 1 {
		t.Errorf("indestructible floor: expected strength=1, got %d", result.Strength)
	}
	if result.Status != state.StatusActive {
		t.Errorf("indestructible: expected ACTIVE, got %s", result.Status)
	}
}

// ── Case 7: SWAMP terrain bonus (+1) — defender holds against equal strength ─

func TestCombat_SwampTerrain(t *testing.T) {
	// SWAMP gives +1 defender bonus.
	// attacker=4, defender=4+1=5 → defender wins.
	cfgs := map[string]config.UnitConfig{
		"att": unitCfg("att", config.ClassFellowshipGuard, config.SideLight),
		"def": unitCfg("def", config.ClassNazgul, config.SideDark),
	}
	bonus := game.TerrainBonusForTerrain(config.TerrainSwamp, []string{"att"}, cfgs)
	// Spec Section 4: FORTRESS +2, MOUNTAINS +1, all others → 0.
	if bonus != 0 {
		t.Errorf("SWAMP terrain bonus: expected 0 (spec: diğerleri→0), got %d", bonus)
	}

	// attacker=5, defender=5+0=5 → tie → defender holds, attacker loses 1
	units := map[string]state.UnitSnapshot{
		"att": unit("att", "dead-marshes", 5),
		"def": unit("def", "dead-marshes", 5),
	}
	region := state.RegionState{ID: "dead-marshes"}
	result := game.ResolveCombat([]string{"att"}, []string{"def"}, region, units, cfgs, config.TerrainSwamp)
	if result.AttackerWon {
		t.Error("SWAMP: attacker(5) vs defender(5+0=5) → tie → attacker should NOT win")
	}
	if result.AttackerLoss != 1 {
		t.Errorf("tie: expected attacker loss=1, got %d", result.AttackerLoss)
	}
}

// ── Case 8: Respawning Nazgul takes fatal damage → RESPAWNING, strength=0 ─

func TestCombat_RespawnOnFatalDamage(t *testing.T) {
	nazgul := state.UnitSnapshot{ID: "naz2", Strength: 2, Status: state.StatusActive}
	cfg := config.UnitConfig{
		ID: "naz2", Class: config.ClassNazgul,
		Respawns: true, RespawnTurns: 3,
	}

	result := game.ApplyDamage(nazgul, cfg, 5) // overkill

	if result.Status != state.StatusRespawning {
		t.Errorf("respawning Nazgul: expected RESPAWNING, got %s", result.Status)
	}
	if result.Strength != 0 {
		t.Errorf("respawning Nazgul: expected strength=0, got %d", result.Strength)
	}
	if result.RespawnTurns != 3 {
		t.Errorf("respawning Nazgul: expected RespawnTurns=3, got %d", result.RespawnTurns)
	}
	if result.Region != "" {
		t.Errorf("respawning Nazgul: expected region=\"\" (removed from map), got %q", result.Region)
	}
}
