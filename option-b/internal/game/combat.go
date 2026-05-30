package game

import "github.com/lotr/option-b/internal/config"
import "github.com/lotr/option-b/internal/state"

// CombatResult holds the outcome of a battle.
type CombatResult struct {
	AttackerWon  bool
	Damage       int // damage dealt to defenders when attacker wins
	AttackerLoss int // strength loss per attacker when defender holds
}

// ResolveCombat applies the combat formula from spec Section 4.
// terrain is the terrain type of the defending region (e.g. config.TerrainFortress).
func ResolveCombat(
	attackerIDs, defenderIDs []string,
	region state.RegionState,
	units map[string]state.UnitSnapshot,
	unitConfigs map[string]config.UnitConfig,
	terrain ...string, // optional: defaults to "" (no terrain bonus)
) CombatResult {
	attackPow := effectivePower(attackerIDs, units, unitConfigs)
	defPow := effectivePower(defenderIDs, units, unitConfigs)

	terrainStr := ""
	if len(terrain) > 0 {
		terrainStr = terrain[0]
	}
	defPow += TerrainBonusForTerrain(terrainStr, attackerIDs, unitConfigs)
	defPow += fortificationBonus(region)

	if attackPow > defPow {
		return CombatResult{
			AttackerWon: true,
			Damage:      attackPow - defPow,
		}
	}
	// Tie or defender wins: each attacker loses 1 strength
	return CombatResult{
		AttackerWon:  false,
		AttackerLoss: 1,
	}
}

// effectivePower sums the effective strengths of a group of units.
func effectivePower(
	unitIDs []string,
	units map[string]state.UnitSnapshot,
	unitConfigs map[string]config.UnitConfig,
) int {
	total := 0
	for _, id := range unitIDs {
		u := units[id]
		eff := u.Strength
		eff += leadershipBonus(id, unitIDs, unitConfigs)
		total += eff
	}
	return total
}

// TerrainBonusForTerrain computes terrain bonus given terrain string and attackers.
func TerrainBonusForTerrain(terrain string, attackerIDs []string, unitConfigs map[string]config.UnitConfig) int {
	anyIgnoresFortress := false
	for _, id := range attackerIDs {
		if cfg, ok := unitConfigs[id]; ok && cfg.IgnoresFortress {
			anyIgnoresFortress = true
			break
		}
	}

	// Spec Section 4: FORTRESS +2, MOUNTAINS +1, all others → 0
	switch terrain {
	case config.TerrainFortress:
		if anyIgnoresFortress {
			return 0 // terrain bonus skipped for IgnoresFortress attackers
		}
		return 2
	case config.TerrainMountains:
		return 1
	default:
		return 0
	}
}

// fortificationBonus returns the fortification bonus (always applies, even vs IgnoresFortress).
func fortificationBonus(region state.RegionState) int {
	if region.Fortified {
		return 2
	}
	return 0
}

// leadershipBonus returns the leadership bonus for a unit from co-located leaders.
func leadershipBonus(unitID string, groupIDs []string, unitConfigs map[string]config.UnitConfig) int {
	bonus := 0
	selfCfg := unitConfigs[unitID]
	for _, id := range groupIDs {
		if id == unitID {
			continue
		}
		cfg := unitConfigs[id]
		if cfg.Leadership && cfg.Side == selfCfg.Side {
			bonus += cfg.LeadershipBonus
		}
	}
	return bonus
}

// ApplyDamage applies damage to a unit according to spec rules.
func ApplyDamage(u state.UnitSnapshot, cfg config.UnitConfig, damage int) state.UnitSnapshot {
	raw := u.Strength - damage
	if cfg.Indestructible {
		if raw < 1 {
			raw = 1
		}
		u.Strength = raw
		u.Status = state.StatusActive
		return u
	}
	if raw <= 0 {
		if cfg.Respawns {
			u.Strength = 0
			u.Status = state.StatusRespawning
			u.RespawnTurns = cfg.RespawnTurns
			u.Region = ""
		} else {
			u.Strength = 0
			u.Status = state.StatusDestroyed
		}
	} else {
		u.Strength = raw
	}
	return u
}
