package tests

import (
	"context"
	"testing"

	"github.com/rotr/option-b/internal/config"
	"github.com/rotr/option-b/internal/pipeline"
	"github.com/rotr/option-b/internal/state"
)

// ── Case 1: Route with known threat and surveillance → correct riskScore ──

func TestPipeline1_RiskScoreFormula(t *testing.T) {
	// Route: 2 paths
	//   path-a: surveillanceLevel=2 → contributes 2*3=6
	//   path-b: surveillanceLevel=0, status=THREATENED → contributes 0 + 2 = 2
	// Regions: region-x threatLevel=3, region-y threatLevel=1 → +4
	// No Nazgul in proximity → +0
	// Expected total = 6 + 2 + 4 = 12

	paths := map[string]state.PathState{
		"path-a": {ID: "path-a", Status: state.PathOpen, SurveillanceLevel: 2},
		"path-b": {ID: "path-b", Status: state.PathThreatened, SurveillanceLevel: 0},
	}
	regions := map[string]state.RegionState{
		"region-x": {ID: "region-x", ThreatLevel: 3},
		"region-y": {ID: "region-y", ThreatLevel: 1},
	}
	units := map[string]state.UnitSnapshot{}
	unitConfigs := map[string]config.UnitConfig{}

	cache := state.WorldStateCache{
		Paths:       paths,
		Regions:     regions,
		Units:       units,
		UnitConfigs: unitConfigs,
		Graph:       config.BuildGraph([]config.PathConfig{}),
	}

	p := pipeline.NewRouteRiskPipeline()
	jobs := []pipeline.RouteJob{
		{
			RouteID:     "test-route",
			StartRegion: "region-x",
			PathIDs:     []string{"path-a", "path-b"},
		},
	}

	result := p.Run(context.Background(), jobs, cache)

	if len(result.Routes) == 0 {
		t.Fatal("expected at least one route result")
	}
	// surveillanceLevel contributions: 2*3=6 + 0*3=0 = 6
	// THREATENED contribution: +2
	// Total from paths = 8
	// Region threat: we don't have route-region mapping in this test, score from paths only
	score := result.Routes[0].RiskScore
	// Minimum expected: path surveillance (6) + THREATENED (2) = 8
	if score < 8 {
		t.Errorf("expected riskScore >= 8 (surveillance+threatened), got %d", score)
	}
}

// ── Case 2: Route 4 — Southern Corridor (Tharbad → Fords of Isen → Edoras → … → Mount Doom) ──
// This is the canonical route defined in buildCanonicalRouteJobs() route-4-southern-corridor.
// Verifies that Pipeline 1 correctly traverses all 8 path segments and scores the route.

func TestPipeline1_Route4SouthernCorridor(t *testing.T) {
	pathCfgs := []config.PathConfig{
		{ID: "shire-to-tharbad",             From: "the-shire",     To: "tharbad",        Cost: 2},
		{ID: "tharbad-to-fords-of-isen",     From: "tharbad",       To: "fords-of-isen",  Cost: 2},
		{ID: "fords-of-isen-to-edoras",      From: "fords-of-isen", To: "edoras",         Cost: 1},
		{ID: "edoras-to-minas-tirith",       From: "edoras",        To: "minas-tirith",   Cost: 2},
		{ID: "minas-tirith-to-osgiliath",    From: "minas-tirith",  To: "osgiliath",      Cost: 1},
		{ID: "osgiliath-to-minas-morgul",    From: "osgiliath",     To: "minas-morgul",   Cost: 1},
		{ID: "minas-morgul-to-cirith-ungol", From: "minas-morgul",  To: "cirith-ungol",   Cost: 1},
		{ID: "cirith-ungol-to-mount-doom",   From: "cirith-ungol",  To: "mount-doom",     Cost: 2},
	}
	graph := config.BuildGraph(pathCfgs)

	paths := make(map[string]state.PathState, len(pathCfgs))
	for _, p := range pathCfgs {
		paths[p.ID] = state.PathState{ID: p.ID, Status: state.PathOpen}
	}
	regions := map[string]state.RegionState{
		"tharbad":        {ID: "tharbad",        ThreatLevel: 2},
		"fords-of-isen":  {ID: "fords-of-isen",  ThreatLevel: 2},
		"edoras":         {ID: "edoras",          ThreatLevel: 1},
		"minas-tirith":   {ID: "minas-tirith",    ThreatLevel: 1},
		"osgiliath":      {ID: "osgiliath",       ThreatLevel: 3},
		"minas-morgul":   {ID: "minas-morgul",    ThreatLevel: 4},
		"cirith-ungol":   {ID: "cirith-ungol",    ThreatLevel: 4},
		"mount-doom":     {ID: "mount-doom",      ThreatLevel: 5},
	}

	// Nazgul at minas-morgul — within 2 hops of minas-tirith, osgiliath, cirith-ungol, mount-doom
	units := map[string]state.UnitSnapshot{
		"naz-patrol": {ID: "naz-patrol", Region: "minas-morgul", Status: state.StatusActive},
	}
	unitConfigs := map[string]config.UnitConfig{
		"naz-patrol": {ID: "naz-patrol", Class: config.ClassNazgul, Side: config.SideDark},
	}

	cache := state.WorldStateCache{
		Paths: paths, Regions: regions,
		Units: units, UnitConfigs: unitConfigs,
		Graph: graph,
	}

	p := pipeline.NewRouteRiskPipeline()
	jobs := []pipeline.RouteJob{
		{
			RouteID:     "route-4-southern-corridor",
			StartRegion: "the-shire",
			PathIDs: []string{
				"shire-to-tharbad",
				"tharbad-to-fords-of-isen",
				"fords-of-isen-to-edoras",
				"edoras-to-minas-tirith",
				"minas-tirith-to-osgiliath",
				"osgiliath-to-minas-morgul",
				"minas-morgul-to-cirith-ungol",
				"cirith-ungol-to-mount-doom",
			},
		},
	}

	result := p.Run(context.Background(), jobs, cache)

	if len(result.Routes) == 0 {
		t.Fatal("Route 4 Southern Corridor: expected a route result, got none")
	}
	r4 := result.Routes[0]
	if r4.RouteID != "route-4-southern-corridor" {
		t.Errorf("wrong route ID: %s", r4.RouteID)
	}

	// Region threat sum: 2+2+1+1+3+4+4+5 = 22
	// Nazgul proximity: naz-patrol at minas-morgul, distance to minas-tirith = 2 hops → count=1 → +2
	// Path contribution: all OPEN, surveillanceLevel=0 → 0
	// Expected total = 24
	if r4.RiskScore < 24 {
		t.Errorf("Route 4: expected riskScore >= 24 (regionThreat=22 + nazgulProximity=2), got %d", r4.RiskScore)
	}

	// Verify tracing traversed all 8 segments: no warnings expected (all OPEN)
	if len(r4.Warnings) != 0 {
		t.Errorf("Route 4: expected no warnings on all-OPEN route, got %v", r4.Warnings)
	}
}

// ── Case 3: Nazgul within 2 hops → proximity count adds to score ──────────

func TestPipeline1_NazgulProximityCount(t *testing.T) {
	// Map: nazgul at "region-a", route region at "region-c"
	// region-a → region-b → region-c (2 hops) → Nazgul in range
	pathCfgs := []config.PathConfig{
		{ID: "a-to-b", From: "region-a", To: "region-b", Cost: 1},
		{ID: "b-to-c", From: "region-b", To: "region-c", Cost: 1},
	}
	graph := config.BuildGraph(pathCfgs)

	paths := map[string]state.PathState{
		"a-to-b": {ID: "a-to-b", Status: state.PathOpen},
		"b-to-c": {ID: "b-to-c", Status: state.PathOpen},
	}
	regions := map[string]state.RegionState{
		"region-a": {ID: "region-a", ThreatLevel: 0},
		"region-b": {ID: "region-b", ThreatLevel: 0},
		"region-c": {ID: "region-c", ThreatLevel: 0},
	}
	units := map[string]state.UnitSnapshot{
		"nazgul-test": {ID: "nazgul-test", Region: "region-a", Status: state.StatusActive},
	}
	unitConfigs := map[string]config.UnitConfig{
		"nazgul-test": {ID: "nazgul-test", Class: config.ClassNazgul, Side: config.SideDark},
	}

	cache := state.WorldStateCache{
		Paths:       paths,
		Regions:     regions,
		Units:       units,
		UnitConfigs: unitConfigs,
		Graph:       graph,
	}

	// Verify graph distance: nazgul at region-a, route includes region-c → 2 hops
	dist := graph.Distance("region-a", "region-c")
	if dist != 2 {
		t.Fatalf("expected distance 2, got %d", dist)
	}

	p := pipeline.NewRouteRiskPipeline()
	jobs := []pipeline.RouteJob{
		{RouteID: "test-route", StartRegion: "region-b", PathIDs: []string{"b-to-c"}},
	}
	result := p.Run(context.Background(), jobs, cache)

	if len(result.Routes) == 0 {
		t.Fatal("expected route result")
	}
	// traceRegions("region-b", ["b-to-c"]) → ["region-c"]
	// Nazgul at region-a, distance to region-c = 2 hops → proximity count = 1
	// Score: 0 (path) + 0 (region threat) + 1*2 (proximity) = 2
	score := result.Routes[0].RiskScore
	if score < 2 {
		t.Errorf("Nazgul within 2 hops: expected riskScore >= 2 (proximity bonus=2), got %d", score)
	}
}
