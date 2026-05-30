package tests

import (
	"context"
	"testing"

	"github.com/lotr/option-b/internal/config"
	"github.com/lotr/option-b/internal/pipeline"
	"github.com/lotr/option-b/internal/state"
)

// ── Case 1: Positive intercept window → score > 0 ────────────────────────
// RB takes 4 turns to reach target, Nazgul takes 2 turns → window = 2 → score > 0

func TestPipeline2_PositiveInterceptWindow(t *testing.T) {
	pathCfgs := []config.PathConfig{
		{ID: "n-to-t", From: "nazgul-region", To: "target-region", Cost: 2},
	}
	graph := config.BuildGraph(pathCfgs)

	units := map[string]state.UnitSnapshot{
		"nazgul-x": {ID: "nazgul-x", Region: "nazgul-region", Status: state.StatusActive},
	}
	unitConfigs := map[string]config.UnitConfig{
		"nazgul-x": {ID: "nazgul-x", Class: config.ClassNazgul, Side: config.SideDark},
	}

	cache := state.WorldStateCache{
		Units:       units,
		UnitConfigs: unitConfigs,
		Paths:       map[string]state.PathState{},
		Regions:     map[string]state.RegionState{},
		Graph:       graph,
	}

	jobs := []pipeline.InterceptJob{
		{
			NazgulID:        "nazgul-x",
			NazgulRegion:    "nazgul-region",
			CandidateRegion: "target-region",
			RouteLength:     8,
			RBTurnsToReach:  4, // RB takes 4 turns
		},
	}

	p := pipeline.NewInterceptPipeline()
	result := p.Run(context.Background(), jobs, cache)

	if len(result.ByUnit) == 0 {
		t.Fatal("expected intercept plan")
	}
	plan := result.ByUnit[0]
	if plan.Score <= 0 {
		t.Errorf("positive intercept window should give score > 0, got %f", plan.Score)
	}
	// turnsToIntercept = ShortestPathCost("nazgul-region", "target-region") = 2
	// interceptWindow = 4 - 2 = 2 (positive)
	// score = 1.0 - (2/8) = 0.75
	expected := 1.0 - (2.0 / 8.0)
	if plan.Score < expected-0.01 || plan.Score > expected+0.01 {
		t.Errorf("expected score≈%.2f, got %.2f", expected, plan.Score)
	}
}

// ── Case 2: Negative intercept window → score = 0.0 ──────────────────────
// RB takes 1 turn to reach target, Nazgul takes 5 turns → window = -4 → score = 0

func TestPipeline2_NegativeInterceptWindow(t *testing.T) {
	// Nazgul is far away (5 hops) — cannot intercept
	pathCfgs := []config.PathConfig{
		{ID: "n-to-mid", From: "nazgul-region", To: "mid1", Cost: 1},
		{ID: "mid1-to-mid2", From: "mid1", To: "mid2", Cost: 1},
		{ID: "mid2-to-mid3", From: "mid2", To: "mid3", Cost: 1},
		{ID: "mid3-to-mid4", From: "mid3", To: "mid4", Cost: 1},
		{ID: "mid4-to-target", From: "mid4", To: "target-region", Cost: 1},
	}
	graph := config.BuildGraph(pathCfgs)

	units := map[string]state.UnitSnapshot{
		"nazgul-y": {ID: "nazgul-y", Region: "nazgul-region", Status: state.StatusActive},
	}
	unitConfigs := map[string]config.UnitConfig{
		"nazgul-y": {ID: "nazgul-y", Class: config.ClassNazgul, Side: config.SideDark},
	}

	cache := state.WorldStateCache{
		Units:       units,
		UnitConfigs: unitConfigs,
		Paths:       map[string]state.PathState{},
		Regions:     map[string]state.RegionState{},
		Graph:       graph,
	}

	jobs := []pipeline.InterceptJob{
		{
			NazgulID:        "nazgul-y",
			NazgulRegion:    "nazgul-region",
			CandidateRegion: "target-region",
			RouteLength:     6,
			RBTurnsToReach:  1, // RB arrives in 1 turn
		},
	}

	p := pipeline.NewInterceptPipeline()
	result := p.Run(context.Background(), jobs, cache)

	if len(result.ByUnit) == 0 {
		t.Fatal("expected intercept plan")
	}
	plan := result.ByUnit[0]
	if plan.Score != 0.0 {
		t.Errorf("negative intercept window must give score=0.0, got %f", plan.Score)
	}
	// turnsToIntercept = 5, rbTurnsToReach = 1
	// interceptWindow = 1 - 5 = -4 → score = 0.0
}
