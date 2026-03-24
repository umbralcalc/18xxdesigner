package engine

import (
	"testing"

	"github.com/umbralcalc/ttdesigner/pkg/gamedata"
)

func newTestTileCtx() *TileLayContext {
	hexes := gamedata.Default1889Map()
	tileDefs := gamedata.Default1889Tiles()
	manifest := gamedata.Default1889TileManifest()

	mapState := InitMapState(hexes)

	tilesAvail := make([]float64, len(manifest))
	for i, entry := range manifest {
		tilesAvail[i] = float64(entry.Count)
	}

	return &TileLayContext{
		CompanyIndex:    0,
		CompanyTreasury: 500,
		MapState:        mapState,
		Hexes:           hexes,
		TileDefs:        tileDefs,
		Manifest:        manifest,
		TilesAvail:      tilesAvail,
		AvailableColors: map[gamedata.TileColor]bool{gamedata.TileColorYellow: true},
		Adjacency:       gamedata.Default1889Adjacency(),
		Config:          gamedata.Default1889Config(),
	}
}

func TestLegalTileLayActions(t *testing.T) {
	t.Run("yellow_phase_generates_placements", func(t *testing.T) {
		ctx := newTestTileCtx()
		actions := LegalTileLayActions(ctx)

		if len(actions) == 0 {
			t.Error("expected at least some legal tile placements in yellow phase")
		}

		// All actions should be tile lay.
		for _, a := range actions {
			if a.Values[ActionType] != ActionLayTile {
				t.Errorf("expected ActionLayTile, got %v", a.Values[ActionType])
			}
		}
	})

	t.Run("no_actions_without_treasury", func(t *testing.T) {
		ctx := newTestTileCtx()
		ctx.CompanyTreasury = 0

		actions := LegalTileLayActions(ctx)

		// Should still have placements on hexes with no terrain cost.
		for _, a := range actions {
			cost := a.Values[ActionArg0+4]
			if cost > 0 {
				t.Errorf("should not generate actions with cost > treasury, got cost %v", cost)
			}
		}
	})

	t.Run("terrain_cost_blocks_mountain", func(t *testing.T) {
		ctx := newTestTileCtx()
		ctx.CompanyTreasury = 50 // less than mountain cost of 80

		actions := LegalTileLayActions(ctx)

		for _, a := range actions {
			hexIdx := int(a.Values[ActionArg0])
			if ctx.Hexes[hexIdx].Terrain == gamedata.TerrainMountain {
				t.Errorf("should not be able to place on mountain hex %s with treasury 50",
					ctx.Hexes[hexIdx].ID)
			}
		}
	})

	t.Run("no_green_tiles_in_yellow_phase", func(t *testing.T) {
		ctx := newTestTileCtx()

		actions := LegalTileLayActions(ctx)

		for _, a := range actions {
			tileID := int(a.Values[ActionArg0+1])
			tile, ok := ctx.TileDefs[tileID]
			if ok && tile.Color != gamedata.TileColorYellow {
				t.Errorf("should not place non-yellow tile %d in yellow phase", tileID)
			}
		}
	})

	t.Run("upgrade_available_in_green_phase", func(t *testing.T) {
		ctx := newTestTileCtx()
		ctx.AvailableColors[gamedata.TileColorGreen] = true

		// Place a yellow city tile on a city hex.
		// Find hex E2 (Matsuyama, a city) and place tile 6 (yellow city).
		hexIdx := -1
		for i, h := range ctx.Hexes {
			if h.ID == "E2" {
				hexIdx = i
				break
			}
		}
		if hexIdx < 0 {
			t.Fatal("could not find hex E2")
		}

		ctx.MapState[MapTileIdx(hexIdx)] = 6
		ctx.MapState[MapOrientIdx(hexIdx)] = 0

		actions := LegalTileLayActions(ctx)

		// Should have upgrade actions for hex E2 (tile 6 upgrades to green tiles).
		upgradeCount := 0
		for _, a := range actions {
			if int(a.Values[ActionArg0]) == hexIdx {
				tileID := int(a.Values[ActionArg0+1])
				tile := ctx.TileDefs[tileID]
				if tile.Color == gamedata.TileColorGreen {
					upgradeCount++
				}
			}
		}
		if upgradeCount == 0 {
			t.Error("expected green upgrade actions for tile 6 on hex E2")
		}
	})
}

func TestRotateEdge(t *testing.T) {
	t.Run("city_node_unchanged", func(t *testing.T) {
		if rotateEdge(-1, 3) != -1 {
			t.Error("city node should not rotate")
		}
	})

	t.Run("rotate_by_0", func(t *testing.T) {
		if rotateEdge(2, 0) != 2 {
			t.Errorf("expected 2, got %d", rotateEdge(2, 0))
		}
	})

	t.Run("rotate_wraps", func(t *testing.T) {
		if rotateEdge(4, 3) != 1 {
			t.Errorf("expected 1, got %d", rotateEdge(4, 3))
		}
	})
}

func TestPathSubset(t *testing.T) {
	tileDefs := gamedata.Default1889Tiles()

	t.Run("tile6_subset_of_tile12", func(t *testing.T) {
		// Tile 6 (yellow city: edges 0,2 to city) should be subset of tile 12 (green city: edges 0,1,2 to city)
		// at some orientation.
		tile6 := tileDefs[6]
		tile12 := tileDefs[12]

		found := false
		for orient := 0; orient < 6; orient++ {
			if pathSubset(tile6, 0, tile12, orient) {
				found = true
				break
			}
		}
		if !found {
			t.Error("tile 6 should be a subset of tile 12 at some orientation")
		}
	})

	t.Run("same_tile_is_subset", func(t *testing.T) {
		tile9 := tileDefs[9]
		if !pathSubset(tile9, 0, tile9, 0) {
			t.Error("tile should be subset of itself")
		}
	})
}
