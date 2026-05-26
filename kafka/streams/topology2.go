package main

import (
	"encoding/json"
	"log"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
)

// Topology2Processor enriches validated ASSIGN_ROUTE and REDIRECT_UNIT orders
// with a routeRiskScore (spec Section 12 / K5).
//
// Source: game.orders.validated (filtered to ASSIGN_ROUTE + REDIRECT_UNIT)
// Sink:   game.orders.validated (re-emits enriched record — V2 schema)
// KTables: PathKTable, RegionKTable, UnitKTable (Nazgul proximity)

// OrderValidatedV2 adds routeRiskScore to the base order.
type OrderValidatedV2 struct {
	PlayerID        string          `json:"playerId"`
	UnitID          string          `json:"unitId"`
	OrderType       string          `json:"orderType"`
	Payload         json.RawMessage `json:"payload"`
	Turn            int             `json:"turn"`
	Timestamp       int64           `json:"timestamp"`
	RouteRiskScore  *int            `json:"routeRiskScore"` // nullable — V2 field
	ThreatenedPaths []string        `json:"threatenedPaths,omitempty"`
	BlockedPaths    []string        `json:"blockedPaths,omitempty"`
}

// RegionStateT2 is the Topology 2 view of a region.
type RegionStateT2 struct {
	ID          string `json:"id"`
	ThreatLevel int    `json:"threatLevel"`
	Control     string `json:"control"`
}

// UnitStateT2 is the Topology 2 view of a unit (Nazgul positions only needed).
type UnitStateT2 struct {
	ID     string `json:"id"`
	Class  string `json:"class"`
	Region string `json:"region"`
	Status string `json:"status"`
}

// Topology2Processor enriches route orders with risk scores.
type Topology2Processor struct {
	producer    *kafka.Producer
	pathTable   map[string]PathState // reuse PathState from topology1.go
	regionTable map[string]RegionStateT2
	unitTable   map[string]UnitStateT2
	graph       *SimpleGraph
}

func NewTopology2Processor(producer *kafka.Producer) *Topology2Processor {
	t := &Topology2Processor{
		producer:    producer,
		pathTable:   make(map[string]PathState),
		regionTable: make(map[string]RegionStateT2),
		unitTable:   make(map[string]UnitStateT2),
		graph:       NewSimpleGraph(),
	}
	loadMapGraphT2(t)
	return t
}

// ProcessMessage enriches a validated order if it is a route order.
func (t *Topology2Processor) ProcessMessage(msg *kafka.Message) {
	var order OrderValidatedV2
	if err := json.Unmarshal(msg.Value, &order); err != nil {
		return
	}

	if order.OrderType != "ASSIGN_ROUTE" && order.OrderType != "REDIRECT_UNIT" {
		return
	}
	// Skip already-enriched records to avoid infinite loop.
	if order.RouteRiskScore != nil {
		return
	}

	pathIDs := extractPathIDs(order.Payload, order.OrderType)
	score, threatened, blocked := t.calcRiskScore(pathIDs)

	order.RouteRiskScore = &score
	order.ThreatenedPaths = threatened
	order.BlockedPaths = blocked

	t.reEmit(order)
}

// calcRiskScore implements the formula from spec Section 12:
//
//	routeRiskScore =
//	  sum(region.threatLevel for each destination region)
//	  + sum(path.surveillanceLevel for each path) * 3
//	  + count(THREATENED paths) * 2
//	  + count(BLOCKED paths) * 5
//	  + nazgulProximityCount * 2
func (t *Topology2Processor) calcRiskScore(pathIDs []string) (int, []string, []string) {
	score := 0
	var threatened, blocked []string

	for _, pid := range pathIDs {
		p, ok := t.pathTable[pid]
		if !ok {
			continue
		}
		score += p.SurveillanceLevel * 3
		switch p.Status {
		case pathThreatened:
			score += 2
			threatened = append(threatened, pid)
		case pathBlocked:
			score += 5
			blocked = append(blocked, pid)
		}
	}

	// Region threat levels for all regions along the route.
	routeRegions := t.traceRouteRegions(pathIDs)
	for _, rid := range routeRegions {
		if r, ok := t.regionTable[rid]; ok {
			score += r.ThreatLevel
		}
	}

	// Nazgul proximity: count Nazgul within 2 graph hops of any route region.
	// Class identified by classNazgul constant — no hardcoded unit ID.
	proxCount := 0
	for _, unit := range t.unitTable {
		if unit.Class != classNazgul || unit.Status != statusActive {
			continue
		}
		for _, rid := range routeRegions {
			dist := t.graph.Distance(unit.Region, rid)
			if dist >= 0 && dist <= 2 {
				proxCount++
				break
			}
		}
	}
	score += proxCount * 2

	return score, threatened, blocked
}

// traceRouteRegions returns all unique region endpoints touched by the path sequence.
func (t *Topology2Processor) traceRouteRegions(pathIDs []string) []string {
	seen := map[string]bool{}
	var result []string
	for _, pid := range pathIDs {
		endpoints, ok := t.graph.Endpoints(pid)
		if !ok {
			continue
		}
		for _, ep := range endpoints {
			if !seen[ep] {
				seen[ep] = true
				result = append(result, ep)
			}
		}
	}
	return result
}

func (t *Topology2Processor) reEmit(order OrderValidatedV2) {
	data, err := json.Marshal(order)
	if err != nil {
		return
	}
	topic := "game.orders.validated"
	if err := t.producer.Produce(&kafka.Message{
		TopicPartition: kafka.TopicPartition{Topic: &topic, Partition: kafka.PartitionAny},
		Key:            []byte(order.UnitID),
		Value:          data,
	}, nil); err != nil {
		log.Printf("[topology2] re-emit: %v", err)
	}
}

// KTable update methods

func (t *Topology2Processor) UpdatePathTable(key string, value []byte) {
	if value == nil {
		delete(t.pathTable, key)
		return
	}
	var ps PathState
	if err := json.Unmarshal(value, &ps); err == nil {
		t.pathTable[key] = ps
	}
}

func (t *Topology2Processor) UpdateRegionTable(key string, value []byte) {
	if value == nil {
		delete(t.regionTable, key)
		return
	}
	var rs RegionStateT2
	if err := json.Unmarshal(value, &rs); err == nil {
		t.regionTable[key] = rs
	}
}

func (t *Topology2Processor) UpdateUnitTable(key string, value []byte) {
	if value == nil {
		delete(t.unitTable, key)
		return
	}
	var us UnitStateT2
	if err := json.Unmarshal(value, &us); err == nil {
		t.unitTable[key] = us
	}
}

// ── SimpleGraph ──────────────────────────────────────────────────────────

type SimpleGraph struct {
	adj       map[string][]string  // regionID → neighbour regions
	endpoints map[string][2]string // pathID → [from, to]
}

func NewSimpleGraph() *SimpleGraph {
	return &SimpleGraph{
		adj:       make(map[string][]string),
		endpoints: make(map[string][2]string),
	}
}

func (g *SimpleGraph) AddEdge(pathID, from, to string) {
	g.endpoints[pathID] = [2]string{from, to}
	g.adj[from] = appendUniq(g.adj[from], to)
	g.adj[to] = appendUniq(g.adj[to], from)
}

func (g *SimpleGraph) Endpoints(pathID string) ([2]string, bool) {
	ep, ok := g.endpoints[pathID]
	return ep, ok
}

// IsAdjacent returns true if from and to are directly connected by a path.
func (g *SimpleGraph) IsAdjacent(from, to string) bool {
	for _, nb := range g.adj[from] {
		if nb == to {
			return true
		}
	}
	return false
}

// Distance returns the BFS hop distance between two regions (-1 if unreachable).
func (g *SimpleGraph) Distance(from, to string) int {
	if from == to {
		return 0
	}
	visited := map[string]bool{from: true}
	queue := []struct {
		id string
		d  int
	}{{from, 0}}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, nb := range g.adj[cur.id] {
			if nb == to {
				return cur.d + 1
			}
			if !visited[nb] {
				visited[nb] = true
				queue = append(queue, struct {
					id string
					d  int
				}{nb, cur.d + 1})
			}
		}
	}
	return -1
}

func appendUniq(slice []string, s string) []string {
	for _, v := range slice {
		if v == s {
			return slice
		}
	}
	return append(slice, s)
}

// loadMapGraphT2 pre-populates Topology2's graph.
func loadMapGraphT2(t *Topology2Processor) {
	for _, p := range allMapPaths() {
		t.graph.AddEdge(p[0], p[1], p[2])
	}
}
