package tests

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/lotr/option-b/internal/config"
	"github.com/lotr/option-b/internal/game"
	"github.com/lotr/option-b/internal/pipeline"
	"github.com/lotr/option-b/internal/state"
)

// TestGoroutineLeak_Pipeline verifies that Pipeline 1 and Pipeline 2 goroutines
// are fully cleaned up after Run() returns (spec §31: zero goroutine leaks).
//
// Run with: go test -race ./tests/ -run TestGoroutineLeak -v
func TestGoroutineLeak_Pipeline(t *testing.T) {
	// Allow existing goroutines (test runner etc.) to settle.
	runtime.GC()
	time.Sleep(20 * time.Millisecond)
	before := countPipelineGoroutines()

	cache := state.WorldStateCache{
		Units:       map[string]state.UnitSnapshot{},
		UnitConfigs: map[string]config.UnitConfig{},
		Paths:       map[string]state.PathState{},
		Regions:     map[string]state.RegionState{},
		Graph:       config.BuildGraph([]config.PathConfig{}),
	}

	// Run Pipeline 1 ten times
	p1 := pipeline.NewRouteRiskPipeline()
	for i := 0; i < 10; i++ {
		ctx := context.Background()
		p1.Run(ctx, []pipeline.RouteJob{
			{RouteID: "r1", StartRegion: "a", PathIDs: []string{}},
		}, cache)
	}

	// Run Pipeline 2 ten times
	p2 := pipeline.NewInterceptPipeline()
	for i := 0; i < 10; i++ {
		ctx := context.Background()
		p2.Run(ctx, []pipeline.InterceptJob{}, cache)
	}

	// Give goroutines a moment to exit.
	runtime.GC()
	time.Sleep(50 * time.Millisecond)

	after := countPipelineGoroutines()
	if after > before {
		t.Errorf("goroutine leak: %d pipeline goroutines before, %d after — delta=%d",
			before, after, after-before)
	}
}

// TestGoroutineLeak_CacheManager verifies CacheManager goroutine exits when context is cancelled.
func TestGoroutineLeak_CacheManager(t *testing.T) {
	runtime.GC()
	time.Sleep(20 * time.Millisecond)
	before := runtime.NumGoroutine()

	ctx, cancel := context.WithCancel(context.Background())
	updateCh := make(chan func(*state.WorldStateCache), 4)
	cm := state.NewCacheManager(state.WorldStateCache{})
	go cm.Run(ctx, updateCh)

	// Apply 10 updates
	for i := 0; i < 10; i++ {
		updateCh <- func(c *state.WorldStateCache) { c.Turn++ }
	}
	time.Sleep(10 * time.Millisecond)

	// Cancel context — CacheManager goroutine must exit.
	cancel()
	close(updateCh)
	time.Sleep(50 * time.Millisecond)
	runtime.GC()

	after := runtime.NumGoroutine()
	if after > before+2 { // allow ±2 for test runtime variance
		t.Errorf("goroutine leak: %d before, %d after CacheManager shutdown", before, after)
	}
}

// TestGoroutineLeak_TurnProcessing verifies no goroutines leak from ProcessTurn.
func TestGoroutineLeak_TurnProcessing(t *testing.T) {
	runtime.GC()
	time.Sleep(20 * time.Millisecond)
	before := runtime.NumGoroutine()

	tp := game.NewTurnProcessor(
		map[string]config.PathConfig{},
		map[string]config.RegionConfig{},
	)
	cache := &state.WorldStateCache{
		Turn:        1,
		MaxTurns:    40,
		Units:       map[string]state.UnitSnapshot{},
		UnitConfigs: map[string]config.UnitConfig{},
		Paths:       map[string]state.PathState{},
		Regions:     map[string]state.RegionState{},
		Graph:       config.BuildGraph([]config.PathConfig{}),
		RingDestructionSiteID: "",
	}

	for i := 0; i < 10; i++ {
		_, _ = tp.ProcessTurn(cache, game.OrderBatch{Turn: cache.Turn})
	}

	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	after := runtime.NumGoroutine()

	if after > before+2 {
		t.Errorf("goroutine leak in ProcessTurn: %d before, %d after 10 turns", before, after)
	}
}

// countPipelineGoroutines counts goroutines whose stack contains pipeline-related frames.
func countPipelineGoroutines() int {
	buf := make([]byte, 1<<16)
	n := runtime.Stack(buf, true)
	stacks := string(buf[:n])
	count := 0
	for _, g := range strings.Split(stacks, "\n\n") {
		if strings.Contains(g, "pipeline.") {
			count++
		}
	}
	return count
}
