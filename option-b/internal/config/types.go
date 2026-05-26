package config

// UnitConfig holds static configuration for a unit, loaded once at startup.
// No unit ID string literal must ever appear in game logic — read config fields instead.
type UnitConfig struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Class            string   `json:"class"`
	Side             string   `json:"side"`
	StartRegion      string   `json:"startRegion"`
	Strength         int      `json:"strength"`
	Leadership       bool     `json:"leadership"`
	LeadershipBonus  int      `json:"leadershipBonus"`
	Indestructible   bool     `json:"indestructible"`
	DetectionRange   int      `json:"detectionRange"`
	Respawns         bool     `json:"respawns"`
	RespawnTurns     int      `json:"respawnTurns"`
	Maia             bool     `json:"maia"`
	MaiaAbilityPaths []string `json:"maiaAbilityPaths"`
	IgnoresFortress  bool     `json:"ignoresFortress"`
	CanFortify       bool     `json:"canFortify"`
	Cooldown         int      `json:"cooldown"`
}

type RegionConfig struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Terrain      string `json:"terrain"`
	SpecialRole  string `json:"specialRole"`
	StartControl string `json:"startControl"`
	StartThreat  int    `json:"startThreat"`
}

type PathConfig struct {
	ID   string `json:"id"`
	From string `json:"from"`
	To   string `json:"to"`
	Cost int    `json:"cost"`
}

type GameConfig struct {
	HiddenUntilTurn     int          `json:"hiddenUntilTurn"`
	MaxTurns            int          `json:"maxTurns"`
	TurnDurationSeconds int          `json:"turnDurationSeconds"`
	Units               []UnitConfig `json:"units"`
}

type MapConfig struct {
	Regions []RegionConfig `json:"regions"`
	Paths   []PathConfig   `json:"paths"`
}

// UnitClass constants — use these instead of string literals in switch statements
const (
	ClassRingBearer     = "RingBearer"
	ClassFellowshipGuard = "FellowshipGuard"
	ClassGondorArmy     = "GondorArmy"
	ClassNazgul         = "Nazgul"
	ClassUrukHaiLegion  = "UrukHaiLegion"
	ClassMaia           = "Maia"
)

const (
	SideLight = "FREE_PEOPLES"
	SideDark  = "SHADOW"
)

const (
	TerrainPlains   = "PLAINS"
	TerrainMountains = "MOUNTAINS"
	TerrainForest   = "FOREST"
	TerrainFortress = "FORTRESS"
	TerrainVolcanic = "VOLCANIC"
	TerrainSwamp    = "SWAMP"
)

const (
	RoleRingBearerStart    = "RING_BEARER_START"
	RoleRingDestructionSite = "RING_DESTRUCTION_SITE"
	RoleShadowStronghold   = "SHADOW_STRONGHOLD"
)
