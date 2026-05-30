package pipeline

import (
	"context"
	"sync"

	"github.com/lotr/option-b/internal/config"
	"github.com/lotr/option-b/internal/state"
)

const interceptWorkers   = 4
const interceptBufferCap = 30

// InterceptJob is one (Nazgul, route-candidate) pair.
type InterceptJob struct {
	NazgulID        string
	NazgulRegion    string
	CandidateRegion string // a region along a possible RB route
	RouteLength     int    // total path count of the candidate route
	RBTurnsToReach  int    // turns for RB to reach candidateRegion
}

// InterceptScore is the result for one job.
type InterceptScore struct {
	NazgulID        string
	TargetRegion    string
	TurnsToIntercept int
	Score           float64
}

// InterceptPlan is the final output of Pipeline 2.
type InterceptPlan struct {
	ByUnit  []UnitInterceptPlan
	Partial bool
}

type UnitInterceptPlan struct {
	UnitID      string
	TargetRegion string
	Score       float64
}

// InterceptPipeline — Pipeline 2 (Dark Side).
// Dispatcher → buffered ch (cap=30) → 4 workers → unbuffered ch → Aggregator
type InterceptPipeline struct{}

func NewInterceptPipeline() *InterceptPipeline { return &InterceptPipeline{} }

func (p *InterceptPipeline) Run(
	parentCtx context.Context,
	jobs []InterceptJob,
	cache state.WorldStateCache,
) InterceptPlan {
	ctx, cancel := context.WithTimeout(parentCtx, pipelineTimeout)
	defer cancel()

	workCh := make(chan InterceptJob, interceptBufferCap)
	resultCh := make(chan InterceptScore) // unbuffered
	var wg sync.WaitGroup

	for i := 0; i < interceptWorkers; i++ {
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
					score := calcInterceptScore(job, cache)
					select {
					case resultCh <- score:
					case <-ctx.Done():
						return
					}
				}
			}
		}()
	}

	go func() {
		for _, job := range jobs {
			select {
			case workCh <- job:
			case <-ctx.Done():
			}
		}
		close(workCh)
		wg.Wait()
		close(resultCh)
	}()

	return aggregateIntercept(ctx, resultCh, len(jobs))
}

// calcInterceptScore implements the interception formula from spec Section 33.
func calcInterceptScore(job InterceptJob, cache state.WorldStateCache) InterceptScore {
	turnsToIntercept := cache.Graph.ShortestPathCost(job.NazgulRegion, job.CandidateRegion)
	interceptWindow := job.RBTurnsToReach - turnsToIntercept

	var score float64
	if interceptWindow >= 0 && job.RouteLength > 0 {
		score = 1.0 - (float64(turnsToIntercept) / float64(job.RouteLength))
	}
	// If interceptWindow < 0, Nazgul cannot make it in time → score = 0.0

	return InterceptScore{
		NazgulID:         job.NazgulID,
		TargetRegion:     job.CandidateRegion,
		TurnsToIntercept: turnsToIntercept,
		Score:            score,
	}
}

// BuildInterceptJobs creates jobs for all (Nazgul, route-candidate) pairs.
// RBTurnsToReach is the cumulative path cost from the route start to each candidate
// region — uses actual path costs (not hop count) to match the cost-based movement model.
func BuildInterceptJobs(cache state.WorldStateCache, candidateRoutes []RouteJob) []InterceptJob {
	var jobs []InterceptJob
	for _, unit := range cache.Units {
		cfg := cache.UnitConfigs[unit.ID]
		if cfg.Class != config.ClassNazgul || unit.Status != state.StatusActive {
			continue
		}
		for _, route := range candidateRoutes {
			cur := route.StartRegion
			cumulativeCost := 0
			for _, pathID := range route.PathIDs {
				cumulativeCost += cache.Graph.PathCost(pathID)
				next := cache.Graph.NextRegionAlongPath(cur, pathID)
				if next == "" {
					break
				}
				jobs = append(jobs, InterceptJob{
					NazgulID:        unit.ID,
					NazgulRegion:    unit.Region,
					CandidateRegion: next,
					RouteLength:     len(route.PathIDs),
					RBTurnsToReach:  cumulativeCost,
				})
				cur = next
			}
		}
	}
	return jobs
}

func aggregateIntercept(ctx context.Context, resultCh <-chan InterceptScore, total int) InterceptPlan {
	// Best score per Nazgul
	best := map[string]InterceptScore{}
	received := 0
	for {
		select {
		case r, ok := <-resultCh:
			if !ok {
				return buildInterceptPlan(best, false)
			}
			received++
			if existing, exists := best[r.NazgulID]; !exists || r.Score > existing.Score {
				best[r.NazgulID] = r
			}
			if received == total {
				return buildInterceptPlan(best, false)
			}
		case <-ctx.Done():
			return buildInterceptPlan(best, true)
		}
	}
}

func buildInterceptPlan(best map[string]InterceptScore, partial bool) InterceptPlan {
	var plans []UnitInterceptPlan
	for _, s := range best {
		plans = append(plans, UnitInterceptPlan{
			UnitID:       s.NazgulID,
			TargetRegion: s.TargetRegion,
			Score:        s.Score,
		})
	}
	return InterceptPlan{ByUnit: plans, Partial: partial}
}
