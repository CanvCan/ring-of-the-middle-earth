package main

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
)

// Topology1 implements Order Validation (spec Section 11).
// Source: game.orders.raw
// Sinks:  game.orders.validated (valid) | game.dlq (invalid)
//
// KTables:
//   - TurnKTable   (game.session key="turn-state")
//   - UnitKTable   (game.events.unit)
//   - PathKTable   (game.events.path)
//   - PlayerKTable (game.session key=playerID)

// OrderRaw mirrors the OrderSubmitted Avro schema fields.
type OrderRaw struct {
	PlayerID  string          `json:"playerId"`
	UnitID    string          `json:"unitId"`
	OrderType string          `json:"orderType"`
	Payload   json.RawMessage `json:"payload"`
	Turn      int             `json:"turn"`
	Timestamp int64           `json:"timestamp"`
}

// UnitState is the KTable view of a unit.
type UnitState struct {
	ID     string `json:"id"`
	Side   string `json:"side"`
	Region string `json:"region"`
	Status string `json:"status"`
	Route  []string `json:"route"`
	Class  string `json:"class"`
	Cooldown int  `json:"cooldown"`
}

// PathState is the KTable view of a path.
type PathState struct {
	ID                string `json:"id"`
	Status            string `json:"status"`
	SurveillanceLevel int    `json:"surveillanceLevel"`
	BlockedBy         string `json:"blockedBy"`
	From              string `json:"from"`
	To                string `json:"to"`
}

// TurnState holds the current turn from game.session.
type TurnState struct {
	CurrentTurn int    `json:"currentTurn"`
	GameID      string `json:"gameId"`
}

// PlayerSession maps a player to their side.
type PlayerSession struct {
	PlayerID string `json:"playerId"`
	Side     string `json:"side"`
}

// RegionStateT1 is the Topology 1 view of a region (for Rule 6 control check).
type RegionStateT1 struct {
	ID      string `json:"id"`
	Control string `json:"control"` // "FREE_PEOPLES" | "SHADOW" | "NEUTRAL"
}

// DLQEntry mirrors the DLQEntry Avro schema.
type DLQEntry struct {
	OriginalTopic string `json:"originalTopic"`
	Partition     int32  `json:"partition"`
	Offset        int64  `json:"offset"`
	ErrorCode     string `json:"errorCode"`
	ErrorMessage  string `json:"errorMessage"`
	RawPayload    []byte `json:"rawPayload"`
	Timestamp     int64  `json:"timestamp"`
}

// ValidationResult holds the outcome of validating one order.
type ValidationResult struct {
	Valid     bool
	ErrorCode string
	ErrorMsg  string
}

// Topology1Processor validates orders using KTable state.
type Topology1Processor struct {
	producer     *kafka.Producer
	turnTable    map[string]TurnState   // key: "current"
	unitTable    map[string]UnitState   // key: unitId
	pathTable    map[string]PathState   // key: pathId
	regionTable  map[string]RegionStateT1 // key: regionId — used by Rule 6
	playerTable  map[string]string      // key: playerID → side ("FREE_PEOPLES" | "SHADOW")
	seenThisTurn map[string]bool        // unitId → already ordered this turn
	graph        *SimpleGraph           // map graph for adjacency checks (Rules 5 & 6)
}

func NewTopology1Processor(producer *kafka.Producer) *Topology1Processor {
	t := &Topology1Processor{
		producer:     producer,
		turnTable:    make(map[string]TurnState),
		unitTable:    make(map[string]UnitState),
		pathTable:    make(map[string]PathState),
		regionTable:  make(map[string]RegionStateT1),
		playerTable:  make(map[string]string),
		seenThisTurn: make(map[string]bool),
		graph:        NewSimpleGraph(),
	}
	loadMapGraphT1(t)
	return t
}

// ProcessMessage handles one message from game.orders.raw.
func (t *Topology1Processor) ProcessMessage(msg *kafka.Message) {
	var order OrderRaw
	if err := json.Unmarshal(msg.Value, &order); err != nil {
		t.sendDLQ(msg, "PARSE_ERROR", fmt.Sprintf("json parse: %v", err))
		return
	}

	result := t.validate(order)
	if !result.Valid {
		t.sendDLQ(msg, result.ErrorCode, result.ErrorMsg)
		return
	}

	t.sendValidated(order)
}

// validate applies all 8 rules from spec Section 11.
func (t *Topology1Processor) validate(order OrderRaw) ValidationResult {

	// Rule 1: order.turn must match current turn.
	turnState := t.turnTable["current"]
	if turnState.CurrentTurn != 0 && order.Turn != turnState.CurrentTurn {
		return ValidationResult{false, "WRONG_TURN",
			fmt.Sprintf("order turn %d != current turn %d", order.Turn, turnState.CurrentTurn)}
	}

	// Rule 8 (checked early — immediately after turn validation):
	// Any second order for the same unitId this turn is rejected with DUPLICATE_UNIT_ORDER,
	// regardless of order type or the outcome of other rules.
	// The unit's order slot is consumed upon the FIRST order that passes Rule 1,
	// so retrying with a different action type (e.g. ASSIGN_ROUTE → REDIRECT_UNIT) is disallowed.
	if t.seenThisTurn[order.UnitID] {
		return ValidationResult{false, "DUPLICATE_UNIT_ORDER",
			fmt.Sprintf("unit %s already has an order this turn", order.UnitID)}
	}
	t.seenThisTurn[order.UnitID] = true

	// Rule 2: unit must belong to submitting player's side.
	unit, unitExists := t.unitTable[order.UnitID]
	if unitExists {
		playerSide := t.sideFromPlayerID(order.PlayerID)
		if playerSide != "" && unit.Side != playerSide {
			return ValidationResult{false, "NOT_YOUR_UNIT",
				fmt.Sprintf("unit %s belongs to %s, not %s", order.UnitID, unit.Side, playerSide)}
		}
	}

	// Rule 2b: Class-based order restrictions.
	// Each unit class may only submit the orders listed in the spec.
	// Sauron (passive Maia) receives no orders at all — handled separately at engine level,
	// but we cannot detect Sauron here without full config, so we allow Maia orders through
	// and let the engine silently discard them for Sauron.
	if unitExists && unit.Class != "" {
		classAllowed := map[string]map[string]bool{
			"RingBearer":     {"ASSIGN_ROUTE": true, "REDIRECT_UNIT": true, "DESTROY_RING": true},
			"FellowshipGuard": {"ASSIGN_ROUTE": true, "REDIRECT_UNIT": true, "ATTACK_REGION": true, "REINFORCE_REGION": true, "BLOCK_PATH": true},
			"GondorArmy":     {"ASSIGN_ROUTE": true, "REDIRECT_UNIT": true, "ATTACK_REGION": true, "REINFORCE_REGION": true, "FORTIFY_REGION": true},
			"Nazgul":         {"ASSIGN_ROUTE": true, "REDIRECT_UNIT": true, "ATTACK_REGION": true, "REINFORCE_REGION": true, "DEPLOY_NAZGUL": true, "BLOCK_PATH": true, "SEARCH_PATH": true},
			"UrukHaiLegion":  {"ASSIGN_ROUTE": true, "REDIRECT_UNIT": true, "ATTACK_REGION": true, "REINFORCE_REGION": true, "BLOCK_PATH": true},
			"Maia":           {"ASSIGN_ROUTE": true, "REDIRECT_UNIT": true, "ATTACK_REGION": true, "REINFORCE_REGION": true, "MAIA_ABILITY": true},
		}
		if allowed, classKnown := classAllowed[unit.Class]; classKnown {
			if !allowed[order.OrderType] {
				return ValidationResult{false, "ORDER_NOT_ALLOWED_FOR_CLASS",
					fmt.Sprintf("unit %s (class %s) cannot issue %s", order.UnitID, unit.Class, order.OrderType)}
			}
		}
	}

	// Rule 3 & 4: Route orders — validate path availability.
	if order.OrderType == "ASSIGN_ROUTE" || order.OrderType == "REDIRECT_UNIT" {
		pathIDs := extractPathIDs(order.Payload, order.OrderType)
		for _, pid := range pathIDs {
			p, ok := t.pathTable[pid]
			if !ok {
				return ValidationResult{false, "INVALID_PATH",
					fmt.Sprintf("path %s not found", pid)}
			}
			if p.Status == pathBlocked {
				return ValidationResult{false, "PATH_BLOCKED",
					fmt.Sprintf("path %s is BLOCKED", pid)}
			}
		}
	}

	// Rule 5: BlockPath / SearchPath — unit must be at an endpoint of the path.
	if order.OrderType == "BLOCK_PATH" || order.OrderType == "SEARCH_PATH" {
		pathID := extractSinglePathID(order.Payload)
		if pathID != "" && unitExists {
			endpoints, ok := t.graph.Endpoints(pathID)
			if ok {
				if unit.Region != endpoints[0] && unit.Region != endpoints[1] {
					return ValidationResult{false, "UNIT_NOT_ADJACENT",
						fmt.Sprintf("unit %s at %s is not at endpoint of path %s (%s/%s)",
							order.UnitID, unit.Region, pathID, endpoints[0], endpoints[1])}
				}
			}
		}
	}

	// Rule 6: AttackRegion — target must be adjacent AND enemy-controlled (or neutral).
	if order.OrderType == "ATTACK_REGION" {
		targetRegion := extractTargetRegion(order.Payload)
		if targetRegion != "" && unitExists {
			if !t.graph.IsAdjacent(unit.Region, targetRegion) {
				return ValidationResult{false, "INVALID_TARGET",
					fmt.Sprintf("unit %s at %s cannot attack non-adjacent %s",
						order.UnitID, unit.Region, targetRegion)}
			}
			// Cannot attack a region already controlled by own side.
			if rc, ok := t.regionTable[targetRegion]; ok {
				if rc.Control == unit.Side {
					return ValidationResult{false, "INVALID_TARGET",
						fmt.Sprintf("unit %s cannot attack friendly region %s",
							order.UnitID, targetRegion)}
				}
			}
		}
	}

	// Rule 7: MaiaAbility — unit cooldown must be 0.
	if order.OrderType == "MAIA_ABILITY" {
		if unitExists && unit.Cooldown > 0 {
			return ValidationResult{false, "ABILITY_ON_COOLDOWN",
				fmt.Sprintf("unit %s has cooldown %d", order.UnitID, unit.Cooldown)}
		}
	}

	return ValidationResult{Valid: true}
}

// sideFromPlayerID looks up the player's side from the player KTable.
func (t *Topology1Processor) sideFromPlayerID(playerID string) string {
	if side, ok := t.playerTable[playerID]; ok {
		return side
	}
	return "" // unknown player — cannot restrict
}

// ResetTurn clears the per-turn seen map when a new turn starts.
func (t *Topology1Processor) ResetTurn() {
	t.seenThisTurn = make(map[string]bool)
}

// UpdateTurnTable updates the TurnKTable from game.session (key="turn-state").
func (t *Topology1Processor) UpdateTurnTable(key string, value []byte) {
	var ts TurnState
	if err := json.Unmarshal(value, &ts); err == nil {
		if ts.CurrentTurn > t.turnTable["current"].CurrentTurn {
			t.ResetTurn()
		}
		t.turnTable["current"] = ts
	}
}

// UpdateUnitTable updates the UnitKTable from game.events.unit.
func (t *Topology1Processor) UpdateUnitTable(key string, value []byte) {
	if value == nil {
		delete(t.unitTable, key)
		return
	}
	var us UnitState
	if err := json.Unmarshal(value, &us); err == nil {
		t.unitTable[key] = us
	}
}

// UpdateRegionTable updates the RegionKTable from game.events.region (for Rule 6).
func (t *Topology1Processor) UpdateRegionTable(key string, value []byte) {
	if value == nil {
		delete(t.regionTable, key)
		return
	}
	var rs RegionStateT1
	if err := json.Unmarshal(value, &rs); err == nil {
		t.regionTable[key] = rs
	}
}

// UpdatePathTable updates the PathKTable from game.events.path.
func (t *Topology1Processor) UpdatePathTable(key string, value []byte) {
	if value == nil {
		delete(t.pathTable, key)
		return
	}
	var ps PathState
	if err := json.Unmarshal(value, &ps); err == nil {
		t.pathTable[key] = ps
	}
}

// UpdatePlayerTable updates the PlayerKTable from game.session (key=playerID).
func (t *Topology1Processor) UpdatePlayerTable(key string, value []byte) {
	if value == nil {
		delete(t.playerTable, key)
		return
	}
	var ps PlayerSession
	if err := json.Unmarshal(value, &ps); err == nil && ps.PlayerID != "" && ps.Side != "" {
		t.playerTable[ps.PlayerID] = ps.Side
	}
}

func (t *Topology1Processor) sendValidated(order OrderRaw) {
	data, _ := json.Marshal(order)
	topic := "game.orders.validated"
	err := t.producer.Produce(&kafka.Message{
		TopicPartition: kafka.TopicPartition{Topic: &topic, Partition: kafka.PartitionAny},
		Key:            []byte(order.UnitID),
		Value:          data,
	}, nil)
	if err != nil {
		log.Printf("[topology1] produce validated: %v", err)
	}
}

func (t *Topology1Processor) sendDLQ(msg *kafka.Message, errorCode, errorMsg string) {
	origTopic := ""
	if msg.TopicPartition.Topic != nil {
		origTopic = *msg.TopicPartition.Topic
	}
	entry := DLQEntry{
		OriginalTopic: origTopic,
		Partition:     int32(msg.TopicPartition.Partition),
		Offset:        int64(msg.TopicPartition.Offset),
		ErrorCode:     errorCode,
		ErrorMessage:  errorMsg,
		RawPayload:    msg.Value,
		Timestamp:     time.Now().UnixMilli(),
	}
	data, _ := json.Marshal(entry)
	dlqTopic := "game.dlq"
	err := t.producer.Produce(&kafka.Message{
		TopicPartition: kafka.TopicPartition{Topic: &dlqTopic, Partition: kafka.PartitionAny},
		Key:            []byte(errorCode),
		Value:          data,
	}, nil)
	if err != nil {
		log.Printf("[topology1] produce dlq: %v", err)
	}
	log.Printf("[topology1] DLQ %s: %s", errorCode, errorMsg)
}

// ── helpers ───────────────────────────────────────────────────────────────

func extractPathIDs(payload json.RawMessage, orderType string) []string {
	var p struct {
		PathIDs    []string `json:"pathIds"`
		NewPathIDs []string `json:"newPathIds"`
	}
	_ = json.Unmarshal(payload, &p)
	if orderType == "REDIRECT_UNIT" {
		return p.NewPathIDs
	}
	return p.PathIDs
}

func extractSinglePathID(payload json.RawMessage) string {
	var p struct {
		PathID string `json:"pathId"`
	}
	_ = json.Unmarshal(payload, &p)
	return p.PathID
}

func extractTargetRegion(payload json.RawMessage) string {
	var p struct {
		TargetRegionID string `json:"targetRegionId"`
	}
	_ = json.Unmarshal(payload, &p)
	return p.TargetRegionID
}

// loadMapGraphT1 pre-populates Topology1's graph with all path endpoints
// so Rules 5 and 6 can check adjacency.
func loadMapGraphT1(t *Topology1Processor) {
	paths := allMapPaths()
	for _, p := range paths {
		t.graph.AddEdge(p[0], p[1], p[2])
	}
}
