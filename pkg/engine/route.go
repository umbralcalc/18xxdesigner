package engine

import "github.com/umbralcalc/18xxdesigner/pkg/gamedata"

// TrackGraph represents the connected track network built from the current map state.
// It is rebuilt each time route-finding is needed (typically once per company per OR).
//
// Nodes are (hexIdx, edge) pairs for hex edges and (hexIdx, -1) for city/town nodes.
// An edge in the graph means two nodes are connected by a track segment on a tile.
type TrackGraph struct {
	// edges[node] → list of connected nodes.
	edges map[TrackNode][]TrackNode
	// hexes from the map.
	hexes []gamedata.HexDef
	// mapState is the current map partition state.
	mapState []float64
	// tileDefs used to decode placed tiles.
	tileDefs map[int]gamedata.TileDef
	// adjacency for cross-hex connections.
	adjacency gamedata.HexAdjacency
	// hexIndex: hexID → index in hexes slice.
	hexIndex map[string]int
}

// TrackNode identifies a point in the track graph.
type TrackNode struct {
	HexIdx int
	Edge   int // 0-5 for hex edge, -1 for city/town stop
}

// Route represents a path through the track network.
type Route struct {
	Stops   []RouteStop // ordered stops visited (cities/towns/off-board)
	Revenue int
}

// RouteStop is a single stop along a route.
type RouteStop struct {
	HexIdx  int
	Revenue int
}

// BuildTrackGraph constructs the track graph from the current map state.
func BuildTrackGraph(
	mapState []float64,
	hexes []gamedata.HexDef,
	tileDefs map[int]gamedata.TileDef,
	adjacency gamedata.HexAdjacency,
) *TrackGraph {
	g := &TrackGraph{
		edges:     make(map[TrackNode][]TrackNode),
		hexes:     hexes,
		mapState:  mapState,
		tileDefs:  tileDefs,
		adjacency: adjacency,
		hexIndex:  make(map[string]int, len(hexes)),
	}
	for i, h := range hexes {
		g.hexIndex[h.ID] = i
	}

	// Add intra-hex connections from tile segments.
	for i, h := range hexes {
		// Gray and off-board hexes always use built-in track, regardless of tile state.
		if h.Type == gamedata.HexGray || h.Type == gamedata.HexOffBoard {
			g.addBuiltInTrack(i, h)
			continue
		}

		tileID := int(mapState[MapTileIdx(i)])
		if tileID < 0 {
			continue // no tile placed on this buildable hex
		}
		tile, ok := tileDefs[tileID]
		if !ok {
			continue
		}
		orientation := int(mapState[MapOrientIdx(i)])
		segs := rotatedSegments(tile, orientation)
		for _, seg := range segs {
			from := TrackNode{i, seg.From}
			to := TrackNode{i, seg.To}
			g.addEdge(from, to)
		}
	}

	// Add inter-hex connections: if hex A's edge E connects to hex B,
	// then (A, E) connects to (B, oppositeEdge(E)).
	for i, h := range hexes {
		neighbors := adjacency[h.ID]
		for edge := 0; edge < 6; edge++ {
			nID := neighbors[edge]
			if nID == "" {
				continue
			}
			nIdx, ok := g.hexIndex[nID]
			if !ok {
				continue
			}
			from := TrackNode{i, edge}
			to := TrackNode{nIdx, oppositeEdge(edge)}
			// Only connect if both hexes have track reaching this edge.
			if g.hasNode(from) && g.hasNode(to) {
				g.addEdge(from, to)
			}
		}
	}

	return g
}

// addBuiltInTrack adds track for gray and off-board hexes that have permanent connections.
func (g *TrackGraph) addBuiltInTrack(hexIdx int, hex gamedata.HexDef) {
	switch hex.Type {
	case gamedata.HexGray:
		// Gray hexes with stops connect all exits through the stop.
		if hex.Revenue > 0 || hex.Slots > 0 {
			// Has a stop — connect each exit to the stop node.
			// We derive exits from the adjacency: any edge with a neighbor.
			neighbors := g.adjacency[hex.ID]
			for edge := 0; edge < 6; edge++ {
				if neighbors[edge] != "" {
					g.addEdge(TrackNode{hexIdx, edge}, TrackNode{hexIdx, -1})
				}
			}
		} else if hex.ID == "J7" {
			// J7 is a pass-through gray: edges 1 and 5 connected directly.
			g.addEdge(TrackNode{hexIdx, 1}, TrackNode{hexIdx, 5})
		}
	case gamedata.HexOffBoard:
		// Off-board: connect listed exits through a virtual stop node.
		for _, e := range hex.Exits {
			g.addEdge(TrackNode{hexIdx, e}, TrackNode{hexIdx, -1})
		}
	}
}

func (g *TrackGraph) addEdge(a, b TrackNode) {
	// Avoid duplicates.
	for _, existing := range g.edges[a] {
		if existing == b {
			return
		}
	}
	g.edges[a] = append(g.edges[a], b)
	g.edges[b] = append(g.edges[b], a)
}

func (g *TrackGraph) hasNode(n TrackNode) bool {
	_, ok := g.edges[n]
	return ok
}

// oppositeEdge returns the edge on the adjacent hex that faces back.
func oppositeEdge(edge int) int {
	return (edge + 3) % 6
}

// isStop returns true if the hex at the given index is a revenue-earning stop.
func (g *TrackGraph) isStop(hexIdx int) bool {
	h := g.hexes[hexIdx]
	tileID := int(g.mapState[MapTileIdx(hexIdx)])

	// Tile-based stop.
	if tileID >= 0 {
		if tile, ok := g.tileDefs[tileID]; ok {
			return tile.Stop != gamedata.StopNone
		}
	}

	// Built-in stop (gray/off-board).
	return h.Revenue > 0 || h.Slots > 0 || h.Type == gamedata.HexOffBoard
}

// stopRevenue returns the revenue for a stop at the given hex.
func (g *TrackGraph) stopRevenue(hexIdx int, gamePhase int) int {
	h := g.hexes[hexIdx]

	// Off-board revenue depends on phase.
	if h.Type == gamedata.HexOffBoard && h.OffBoard != nil {
		switch {
		case gamePhase >= 5: // D phase
			return h.OffBoard.Diesel
		case gamePhase >= 3: // brown phases (5, 6)
			return h.OffBoard.Brown
		default:
			return h.OffBoard.Yellow
		}
	}

	// Tile-based revenue.
	tileID := int(g.mapState[MapTileIdx(hexIdx)])
	if tileID >= 0 {
		if tile, ok := g.tileDefs[tileID]; ok {
			return tile.Revenue
		}
	}

	return h.Revenue
}

// hasCompanyToken returns true if the given company has a token on this hex.
func hasCompanyToken(mapState []float64, hexIdx int, companyID int) bool {
	tokenField := int(mapState[MapTokenIdx(hexIdx)])
	return tokenField&(1<<companyID) != 0
}

// EnumerateRoutes finds all valid routes for a given train type from a company's tokens.
//
// A route is valid if:
//   - It starts from a hex where the company has a token.
//   - It visits at most `distance` stops (cities/towns/off-board).
//   - It visits at least 2 stops.
//   - It does not visit the same hex twice (no loops).
//   - Track segments are continuous (connected through the graph).
//
// For diesel trains (distance = -1), there is no stop limit.
func EnumerateRoutes(
	graph *TrackGraph,
	companyID int,
	distance int, // max stops; -1 = unlimited (diesel)
	gamePhase int,
) []Route {
	var routes []Route

	// Find all hexes where the company has a token.
	var tokenHexes []int
	for i := range graph.hexes {
		if hasCompanyToken(graph.mapState, i, companyID) {
			tokenHexes = append(tokenHexes, i)
		}
	}

	if len(tokenHexes) == 0 {
		return nil
	}

	// DFS from each token hex to find all valid routes.
	for _, startHex := range tokenHexes {
		startNode := TrackNode{startHex, -1}
		if _, ok := graph.edges[startNode]; !ok {
			continue
		}

		visited := make(map[int]bool)
		visited[startHex] = true

		stoppedAt := make(map[int]bool)
		stoppedAt[startHex] = true

		startRevenue := graph.stopRevenue(startHex, gamePhase)
		path := []RouteStop{{HexIdx: startHex, Revenue: startRevenue}}

		graph.dfsRoutes(startNode, TrackNode{-1, -1}, companyID, distance, gamePhase,
			visited, stoppedAt, path, 1, startRevenue, &routes)
	}

	return routes
}

// dfsRoutes performs depth-first search to enumerate routes.
// prevNode prevents immediately backtracking to the node we just came from.
// visitedHexes tracks hexes we've traversed (to prevent loops).
// stoppedAt tracks hexes whose stops we've already counted (to prevent double-counting).
func (g *TrackGraph) dfsRoutes(
	current TrackNode,
	prevNode TrackNode,
	companyID int,
	maxStops int,
	gamePhase int,
	visitedHexes map[int]bool,
	stoppedAt map[int]bool,
	path []RouteStop,
	stopCount int,
	currentRevenue int,
	routes *[]Route,
) {
	for _, next := range g.edges[current] {
		if next == prevNode {
			continue
		}

		// Don't revisit hexes (prevents graph cycles).
		if next.HexIdx != current.HexIdx && visitedHexes[next.HexIdx] {
			continue
		}

		if next.Edge == -1 {
			// Reaching a stop node.
			if stoppedAt[next.HexIdx] {
				// Already counted this stop — just continue through.
				g.dfsRoutes(next, current, companyID, maxStops, gamePhase,
					visitedHexes, stoppedAt, path, stopCount, currentRevenue, routes)
			} else {
				// New stop.
				newStops := stopCount + 1
				if maxStops > 0 && newStops > maxStops {
					continue
				}

				rev := g.stopRevenue(next.HexIdx, gamePhase)
				newRevenue := currentRevenue + rev
				newPath := append(append([]RouteStop{}, path...), RouteStop{next.HexIdx, rev})

				if newStops >= 2 {
					route := Route{
						Stops:   make([]RouteStop, len(newPath)),
						Revenue: newRevenue,
					}
					copy(route.Stops, newPath)
					*routes = append(*routes, route)
				}

				visitedHexes[next.HexIdx] = true
				stoppedAt[next.HexIdx] = true
				g.dfsRoutes(next, current, companyID, maxStops, gamePhase,
					visitedHexes, stoppedAt, newPath, newStops, newRevenue, routes)
				delete(visitedHexes, next.HexIdx)
				delete(stoppedAt, next.HexIdx)
			}
		} else {
			// Edge node — traversal without stopping.
			if next.HexIdx != current.HexIdx {
				if visitedHexes[next.HexIdx] {
					continue
				}
				visitedHexes[next.HexIdx] = true
				g.dfsRoutes(next, current, companyID, maxStops, gamePhase,
					visitedHexes, stoppedAt, path, stopCount, currentRevenue, routes)
				delete(visitedHexes, next.HexIdx)
			} else {
				// Same hex, different edge — mark this edge as visited to prevent intra-hex cycles.
				edgeKey := next.HexIdx*10 + next.Edge
				if visitedHexes[edgeKey+1000000] {
					continue
				}
				visitedHexes[edgeKey+1000000] = true
				g.dfsRoutes(next, current, companyID, maxStops, gamePhase,
					visitedHexes, stoppedAt, path, stopCount, currentRevenue, routes)
				delete(visitedHexes, edgeKey+1000000)
			}
		}
	}
}

// TrainRoute pairs a train with its assigned route.
type TrainRoute struct {
	TrainIdx int
	Route    Route
}

// OptimalRouteAssignment finds the revenue-maximizing assignment of trains to
// non-overlapping routes. Two routes overlap if they share any hex.
//
// This uses backtracking with pruning. For 1889 (max 2-3 trains per company,
// small map), this is fast enough.
func OptimalRouteAssignment(
	graph *TrackGraph,
	companyID int,
	trains []int, // indices into Config.Trains for trains held by this company
	trainDistances []int, // distance for each train (-1 = diesel)
	gamePhase int,
) ([]TrainRoute, int) {
	if len(trains) == 0 {
		return nil, 0
	}

	// Enumerate all candidate routes for each train.
	type trainRoutes struct {
		trainIdx int
		routes   []Route
	}
	var allTrainRoutes []trainRoutes

	for i, dist := range trainDistances {
		routes := EnumerateRoutes(graph, companyID, dist, gamePhase)
		if len(routes) > 0 {
			allTrainRoutes = append(allTrainRoutes, trainRoutes{trains[i], routes})
		}
	}

	if len(allTrainRoutes) == 0 {
		return nil, 0
	}

	// Sort routes for each train by revenue (descending) for better pruning.
	for i := range allTrainRoutes {
		sortRoutesByRevenue(allTrainRoutes[i].routes)
	}

	// Backtracking search.
	var bestAssignment []TrainRoute
	bestRevenue := 0

	var search func(trainIndex int, usedHexes map[int]bool, current []TrainRoute, currentRevenue int)
	search = func(trainIndex int, usedHexes map[int]bool, current []TrainRoute, currentRevenue int) {
		if trainIndex >= len(allTrainRoutes) {
			if currentRevenue > bestRevenue {
				bestRevenue = currentRevenue
				bestAssignment = make([]TrainRoute, len(current))
				copy(bestAssignment, current)
			}
			return
		}

		// Upper bound pruning: even if remaining trains get max possible revenue.
		upperBound := currentRevenue
		for j := trainIndex; j < len(allTrainRoutes); j++ {
			if len(allTrainRoutes[j].routes) > 0 {
				upperBound += allTrainRoutes[j].routes[0].Revenue // already sorted desc
			}
		}
		if upperBound <= bestRevenue {
			return
		}

		tr := allTrainRoutes[trainIndex]

		// Option 1: skip this train (don't assign a route).
		search(trainIndex+1, usedHexes, current, currentRevenue)

		// Option 2: try each route for this train.
		for _, route := range tr.routes {
			// Check for hex overlap with already-used hexes.
			overlaps := false
			for _, stop := range route.Stops {
				if usedHexes[stop.HexIdx] {
					overlaps = true
					break
				}
			}
			if overlaps {
				continue
			}

			// Mark hexes as used.
			for _, stop := range route.Stops {
				usedHexes[stop.HexIdx] = true
			}

			assignment := TrainRoute{TrainIdx: tr.trainIdx, Route: route}
			search(trainIndex+1, usedHexes, append(current, assignment), currentRevenue+route.Revenue)

			// Unmark hexes.
			for _, stop := range route.Stops {
				delete(usedHexes, stop.HexIdx)
			}
		}
	}

	search(0, make(map[int]bool), nil, 0)

	return bestAssignment, bestRevenue
}

// sortRoutesByRevenue sorts routes in descending order of revenue.
func sortRoutesByRevenue(routes []Route) {
	// Simple insertion sort — route counts are small.
	for i := 1; i < len(routes); i++ {
		for j := i; j > 0 && routes[j].Revenue > routes[j-1].Revenue; j-- {
			routes[j], routes[j-1] = routes[j-1], routes[j]
		}
	}
}
