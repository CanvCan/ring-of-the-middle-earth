package game

// OrderType constants
const (
	OrderAssignRoute     = "ASSIGN_ROUTE"
	OrderRedirectUnit    = "REDIRECT_UNIT"
	OrderDestroyRing     = "DESTROY_RING"
	OrderMaiaAbility     = "MAIA_ABILITY"
	OrderBlockPath       = "BLOCK_PATH"
	OrderSearchPath      = "SEARCH_PATH"
	OrderAttackRegion    = "ATTACK_REGION"
	OrderReinforceRegion = "REINFORCE_REGION"
	OrderFortifyRegion   = "FORTIFY_REGION"
	OrderDeployNazgul    = "DEPLOY_NAZGUL"
)

// Error codes
const (
	ErrWrongTurn              = "WRONG_TURN"
	ErrNotYourUnit            = "NOT_YOUR_UNIT"
	ErrPathBlocked            = "PATH_BLOCKED"
	ErrInvalidPath            = "INVALID_PATH"
	ErrUnitNotAdjacent        = "UNIT_NOT_ADJACENT"
	ErrInvalidTarget          = "INVALID_TARGET"
	ErrDuplicateUnitOrder     = "DUPLICATE_UNIT_ORDER"
	ErrAbilityOnCooldown      = "ABILITY_ON_COOLDOWN"
	ErrMaiaDisabled           = "MAIA_DISABLED"
	ErrDestroyConditionNotMet = "DESTROY_CONDITION_NOT_MET"
)

// BaseOrder contains fields common to all orders.
type BaseOrder struct {
	OrderType string `json:"orderType"`
	PlayerID  string `json:"playerId"`
	UnitID    string `json:"unitId"`
	Turn      int    `json:"turn"`
}

type AssignRouteOrder struct {
	BaseOrder
	PathIDs []string `json:"pathIds"`
}

type RedirectUnitOrder struct {
	BaseOrder
	NewPathIDs []string `json:"newPathIds"`
}

type DestroyRingOrder struct {
	BaseOrder
}

type MaiaAbilityOrder struct {
	BaseOrder
	TargetPathID string `json:"targetPathId"`
}

type BlockPathOrder struct {
	BaseOrder
	PathID string `json:"pathId"`
}

type SearchPathOrder struct {
	BaseOrder
	PathID string `json:"pathId"`
}

type AttackRegionOrder struct {
	BaseOrder
	TargetRegionID string `json:"targetRegionId"`
}

type ReinforceRegionOrder struct {
	BaseOrder
	TargetRegionID string `json:"targetRegionId"`
}

type FortifyRegionOrder struct {
	BaseOrder
}

type DeployNazgulOrder struct {
	BaseOrder
	TargetRegionID string `json:"targetRegionId"`
}

// ValidationError represents a validation failure.
type ValidationError struct {
	ErrorCode string
	Message   string
}

func (e *ValidationError) Error() string {
	return e.ErrorCode + ": " + e.Message
}
