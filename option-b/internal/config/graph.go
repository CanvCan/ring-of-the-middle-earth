package config

// Graph provides BFS-based graph operations over the map.
type Graph struct {
	// adj maps regionID → list of (neighborID, pathID, cost)
	adj map[string][]Edge
}

type Edge struct {
	To     string
	PathID string
	Cost   int
}

func BuildGraph(paths []PathConfig) *Graph {
	g := &Graph{adj: make(map[string][]Edge)}
	for _, p := range paths {
		g.adj[p.From] = append(g.adj[p.From], Edge{To: p.To, PathID: p.ID, Cost: p.Cost})
		g.adj[p.To] = append(g.adj[p.To], Edge{To: p.From, PathID: p.ID, Cost: p.Cost})
	}
	return g
}

// Distance returns the minimum number of hops (edges) between two regions.
// Returns -1 if unreachable.
func (g *Graph) Distance(from, to string) int {
	if from == to {
		return 0
	}
	visited := map[string]bool{from: true}
	queue := []struct {
		id   string
		dist int
	}{{from, 0}}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, e := range g.adj[cur.id] {
			if e.To == to {
				return cur.dist + 1
			}
			if !visited[e.To] {
				visited[e.To] = true
				queue = append(queue, struct {
					id   string
					dist int
				}{e.To, cur.dist + 1})
			}
		}
	}
	return -1
}

// ShortestPathCost returns the minimum turn cost (sum of path costs) between two regions.
// Returns -1 if unreachable.
func (g *Graph) ShortestPathCost(from, to string) int {
	if from == to {
		return 0
	}
	dist := map[string]int{from: 0}
	// Simple Dijkstra with a priority queue (slice-based for simplicity)
	type item struct {
		id   string
		cost int
	}
	queue := []item{{from, 0}}
	for len(queue) > 0 {
		// Pop minimum
		minIdx := 0
		for i, q := range queue {
			if q.cost < queue[minIdx].cost {
				minIdx = i
			}
		}
		cur := queue[minIdx]
		queue = append(queue[:minIdx], queue[minIdx+1:]...)

		if cur.id == to {
			return cur.cost
		}
		for _, e := range g.adj[cur.id] {
			newCost := cur.cost + e.Cost
			if d, ok := dist[e.To]; !ok || newCost < d {
				dist[e.To] = newCost
				queue = append(queue, item{e.To, newCost})
			}
		}
	}
	if d, ok := dist[to]; ok {
		return d
	}
	return -1
}

// RegionsWithinHops returns all region IDs reachable within maxHops hops from start.
func (g *Graph) RegionsWithinHops(start string, maxHops int) []string {
	visited := map[string]int{start: 0}
	queue := []struct {
		id   string
		hops int
	}{{start, 0}}
	var result []string
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if cur.id != start {
			result = append(result, cur.id)
		}
		if cur.hops >= maxHops {
			continue
		}
		for _, e := range g.adj[cur.id] {
			if _, seen := visited[e.To]; !seen {
				visited[e.To] = cur.hops + 1
				queue = append(queue, struct {
					id   string
					hops int
				}{e.To, cur.hops + 1})
			}
		}
	}
	return result
}

// Neighbors returns all direct neighbors of a region.
func (g *Graph) Neighbors(regionID string) []string {
	edges := g.adj[regionID]
	out := make([]string, 0, len(edges))
	for _, e := range edges {
		out = append(out, e.To)
	}
	return out
}

// PathEndpoints returns the two endpoint regions of a path.
func PathEndpoints(p PathConfig) [2]string {
	return [2]string{p.From, p.To}
}

// PathCost returns the cost of a single path segment, or 1 if not found.
func (g *Graph) PathCost(pathID string) int {
	for _, edges := range g.adj {
		for _, e := range edges {
			if e.PathID == pathID {
				return e.Cost
			}
		}
	}
	return 1 // fallback
}

// NextRegionAlongPath returns the neighboring region connected by pathID from 'from'.
// Because edges are stored bidirectionally, this handles traversal in either direction.
// Returns "" if the path is not an edge from 'from'.
func (g *Graph) NextRegionAlongPath(from, pathID string) string {
	for _, e := range g.adj[from] {
		if e.PathID == pathID {
			return e.To
		}
	}
	return ""
}
