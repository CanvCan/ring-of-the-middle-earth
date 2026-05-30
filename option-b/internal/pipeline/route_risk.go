package pipeline

import (
	"context"
	"sync"
	"time"

	"github.com/lotr/option-b/internal/config"
	"github.com/lotr/option-b/internal/state"
)

const (
	routeWorkers    = 4
	routeBufferCap  = 20
	pipelineTimeout = 2 * time.Second
)

type RouteJob struct {
	RouteID   string
	PathIDs   []string
	StartRegion string
}

type RouteResult struct {
	RouteID   string
	PathIDs   []string
	RiskScore int
	Warnings  []string
}

type RankedRouteList struct {
	Routes      []RouteResult
	Recommended string
	Warnings    []string
	Partial     bool // true if returned due to timeout
}

// RouteRiskPipeline — Pipeline 1 (Light Side).
// Dispatcher → buffered ch (cap=20) → 4 workers → unbuffered ch → Aggregator
type RouteRiskPipeline struct{}

func NewRouteRiskPipeline() *RouteRiskPipeline { return &RouteRiskPipeline{} }

func (p *RouteRiskPipeline) Run(
	parentCtx context.Context,
	jobs []RouteJob,
	cache state.WorldStateCache,
) RankedRouteList {
	ctx, cancel := context.WithTimeout(parentCtx, pipelineTimeout)
	defer cancel()

	workCh := make(chan RouteJob, routeBufferCap)
	resultCh := make(chan RouteResult) // unbuffered — Aggregator must be ready
	var wg sync.WaitGroup

	// 4 workers
	for i := 0; i < routeWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case job, ok := <-workCh:
					if !ok {
						return
					}
					score, warnings := calcRouteRisk(job, cache)
					select {
					case resultCh <- RouteResult{
						RouteID: job.RouteID, PathIDs: job.PathIDs,
						RiskScore: score, Warnings: warnings,
					}:
					case <-ctx.Done():
						return
					}
				}
			}
		}()
	}

	// Dispatcher
	go func() {
		for _, job := range jobs {
			select {
			case workCh <- job:
			case <-ctx.Done():
			}
		}
		close(workCh)
		wg.Wait()
		close(resultCh) // signal aggregator
	}()

	return aggregateRoutes(ctx, resultCh, len(jobs))
}

// calcRouteRisk implements spec formula (Topology 2 / Section 32).
func calcRouteRisk(job RouteJob, cache state.WorldStateCache) (int, []string) {
	score := 0
	var warnings []string

	for _, pathID := range job.PathIDs {
		path, ok := cache.Paths[pathID]
		if !ok {
			continue
		}
		// + sum(path.surveillanceLevel) * 3
		score += path.SurveillanceLevel * 3

		switch path.Status {
		case state.PathBlocked:
			score += 5
			warnings = append(warnings, "BLOCKED:"+pathID)
		case state.PathThreatened:
			score += 2
			warnings = append(warnings, "THREATENED:"+pathID)
		}
	}

	// + sum(region.threatLevel for each destination region)
	routeRegions := traceRegions(job.StartRegion, job.PathIDs, cache)
	for _, rid := range routeRegions {
		if r, ok := cache.Regions[rid]; ok {
			score += r.ThreatLevel
		}
	}

	// + nazgulProximityCount * 2
	score += calcNazgulProximity(routeRegions, cache) * 2

	return score, warnings
}

// traceRegions returns the ordered list of destination regions traversed by a route.
// Each entry is the region arrived at after traversing the corresponding path.
func traceRegions(start string, pathIDs []string, cache state.WorldStateCache) []string {
	cur := start
	var result []string
	for _, pid := range pathIDs {
		next := cache.Graph.NextRegionAlongPath(cur, pid)
		if next == "" {
			break // path does not connect from current region — route is invalid
		}
		result = append(result, next)
		cur = next
	}
	return result
}

// calcNazgulProximity counts Nazgul within 2 hops of any region in the route.
func calcNazgulProximity(routeRegions []string, cache state.WorldStateCache) int {
	count := 0
	for _, unit := range cache.Units {
		cfg := cache.UnitConfigs[unit.ID]
		if cfg.Class != config.ClassNazgul || unit.Status != state.StatusActive {
			continue
		}
		for _, regionID := range routeRegions {
			dist := cache.Graph.Distance(unit.Region, regionID)
			if dist >= 0 && dist <= 2 {
				count++
				break
			}
		}
	}
	return count
}

func aggregateRoutes(ctx context.Context, resultCh <-chan RouteResult, total int) RankedRouteList {
	var results []RouteResult
	for {
		select {
		case r, ok := <-resultCh:
			if !ok {
				return buildRankedList(results, false)
			}
			results = append(results, r)
			if len(results) == total {
				return buildRankedList(results, false)
			}
		case <-ctx.Done():
			return buildRankedList(results, true)
		}
	}
}

func buildRankedList(results []RouteResult, partial bool) RankedRouteList {
	for i := 0; i < len(results); i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].RiskScore < results[i].RiskScore {
				results[i], results[j] = results[j], results[i]
			}
		}
	}
	list := RankedRouteList{Routes: results, Partial: partial}
	if len(results) > 0 {
		list.Recommended = results[0].RouteID
	}
	return list
}
