package engine

import (
	"testing"

	"github.com/umbralcalc/18xxdesigner/pkg/gamedata"
)

// makeTestGraph builds a small track graph for testing.
// We use the real 1889 map but place specific tiles and tokens.
func makeTestGraph(placements []testPlacement, tokens []testToken) (*TrackGraph, []gamedata.HexDef) {
	hexes := gamedata.Default1889Map()
	adjacency := gamedata.Default1889Adjacency()
	tileDefs := gamedata.Default1889Tiles()
	mapState := InitMapState(hexes)

	// Place tiles.
	hexIndex := make(map[string]int, len(hexes))
	for i, h := range hexes {
		hexIndex[h.ID] = i
	}

	for _, p := range placements {
		idx, ok := hexIndex[p.hexID]
		if !ok {
			continue
		}
		mapState[MapTileIdx(idx)] = float64(p.tileID)
		mapState[MapOrientIdx(idx)] = float64(p.orientation)
	}

	// Place tokens.
	for _, tok := range tokens {
		idx, ok := hexIndex[tok.hexID]
		if !ok {
			continue
		}
		mapState[MapTokenIdx(idx)] = float64(int(mapState[MapTokenIdx(idx)]) | (1 << tok.companyID))
	}

	graph := BuildTrackGraph(mapState, hexes, tileDefs, adjacency)
	return graph, hexes
}

type testPlacement struct {
	hexID       string
	tileID      int
	orientation int
}

type testToken struct {
	hexID     string
	companyID int
}

func TestBuildTrackGraph(t *testing.T) {
	t.Run("empty_map_has_gray_and_offboard_track", func(t *testing.T) {
		graph, _ := makeTestGraph(nil, nil)

		// Uwajima (B7) is gray with 3 exits and a stop.
		// It should have a stop node connected to at least one edge.
		hexIndex := graph.hexIndex["B7"]
		stopNode := TrackNode{hexIndex, -1}
		if _, ok := graph.edges[stopNode]; !ok {
			t.Error("expected Uwajima gray hex to have stop node in graph")
		}

		// Off-board Imabari (F1) should have a stop node.
		f1Idx := graph.hexIndex["F1"]
		f1Stop := TrackNode{f1Idx, -1}
		if _, ok := graph.edges[f1Stop]; !ok {
			t.Error("expected Imabari off-board to have stop node in graph")
		}
	})
}

func TestEnumerateRoutes(t *testing.T) {
	t.Run("simple_two_city_route", func(t *testing.T) {
		// Place tile 57 (city, edges 0-3 through city) on Matsuyama (E2)
		// and tile 57 on Saijou (F3), oriented so they connect.
		// E2 is adjacent to F3 via edge 2 (SE) → F3 edge 5 (NW).
		// Tile 57: city, paths 0→city, city→3.
		// For E2 to have track on edge 2: rotate tile 57 by 2 → paths become 2→city, city→5.
		// For F3 to have track on edge 5: rotate tile 57 by 5 → paths become 5→city, city→2.
		placements := []testPlacement{
			{"E2", 57, 2}, // E2: city with edges 2 and 5
			{"F3", 57, 5}, // F3: city with edges 5 and 2
		}
		tokens := []testToken{
			{"E2", 1}, // IR has token on Matsuyama
		}

		graph, _ := makeTestGraph(placements, tokens)

		routes := EnumerateRoutes(graph, 1, 2, 0)

		if len(routes) == 0 {
			t.Fatal("expected at least one route, got none")
		}

		// Find a route with revenue = 20 + 20 = 40.
		found40 := false
		for _, r := range routes {
			if r.Revenue == 40 && len(r.Stops) == 2 {
				found40 = true
			}
		}
		if !found40 {
			t.Errorf("expected a 2-stop route with revenue 40, routes found: %d", len(routes))
			for _, r := range routes {
				t.Logf("  route: stops=%d revenue=%d", len(r.Stops), r.Revenue)
			}
		}
	})

	t.Run("no_token_no_routes", func(t *testing.T) {
		placements := []testPlacement{
			{"E2", 57, 0},
		}

		graph, _ := makeTestGraph(placements, nil)
		routes := EnumerateRoutes(graph, 1, 2, 0)

		if len(routes) != 0 {
			t.Errorf("expected no routes without tokens, got %d", len(routes))
		}
	})
}

func TestOptimalRouteAssignment(t *testing.T) {
	t.Run("single_train_picks_best_route", func(t *testing.T) {
		// Set up a line of 3 cities connected through E2 → F3 → G4.
		// E2-F3 via edge 2/5, F3-G4 via edge 2/5.
		placements := []testPlacement{
			{"E2", 57, 2}, // city edges 2,5 (rev 20)
			{"F3", 57, 5}, // city edges 5,2 (rev 20)
			{"G4", 57, 5}, // city edges 5,2 (rev 20)
		}
		tokens := []testToken{
			{"E2", 1}, // IR token on Matsuyama
		}

		graph, _ := makeTestGraph(placements, tokens)

		// A 3-train should be able to reach all 3 stops = 60 revenue.
		assignments, totalRevenue := OptimalRouteAssignment(
			graph, 1,
			[]int{0}, []int{3},
			0,
		)

		if totalRevenue != 60 {
			t.Errorf("expected revenue 60, got %d", totalRevenue)
			for _, a := range assignments {
				t.Logf("  train %d: stops=%d revenue=%d", a.TrainIdx, len(a.Route.Stops), a.Route.Revenue)
			}
		}
	})

	t.Run("no_trains_zero_revenue", func(t *testing.T) {
		graph, _ := makeTestGraph(nil, nil)

		_, totalRevenue := OptimalRouteAssignment(
			graph, 0,
			nil, nil, 0,
		)

		if totalRevenue != 0 {
			t.Errorf("expected 0 revenue with no trains, got %d", totalRevenue)
		}
	})
}

func TestOppositeEdge(t *testing.T) {
	tests := []struct{ edge, want int }{
		{0, 3}, {1, 4}, {2, 5}, {3, 0}, {4, 1}, {5, 2},
	}
	for _, tt := range tests {
		got := oppositeEdge(tt.edge)
		if got != tt.want {
			t.Errorf("oppositeEdge(%d) = %d, want %d", tt.edge, got, tt.want)
		}
	}
}

func TestSortRoutesByRevenue(t *testing.T) {
	routes := []Route{
		{Revenue: 20},
		{Revenue: 60},
		{Revenue: 40},
		{Revenue: 10},
	}
	sortRoutesByRevenue(routes)

	for i := 1; i < len(routes); i++ {
		if routes[i].Revenue > routes[i-1].Revenue {
			t.Errorf("routes not sorted descending at index %d: %d > %d",
				i, routes[i].Revenue, routes[i-1].Revenue)
		}
	}
}
