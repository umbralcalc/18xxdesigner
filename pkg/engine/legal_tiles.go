package engine

import (
	"github.com/umbralcalc/stochadex/pkg/simulator"
	"github.com/umbralcalc/18xxdesigner/pkg/gamedata"
)

// TileLayContext holds state needed for tile-laying legal move generation.
type TileLayContext struct {
	CompanyIndex   int
	CompanyTreasury float64

	// Current map state.
	MapState []float64
	Hexes    []gamedata.HexDef

	// Available tiles and definitions.
	TileDefs     map[int]gamedata.TileDef
	Manifest     []gamedata.TileManifestEntry
	TilesAvail   []float64 // bank tile counts, indexed same as manifest

	// Current phase determines available tile colors.
	AvailableColors map[gamedata.TileColor]bool

	// Adjacency for connectivity checks.
	Adjacency gamedata.HexAdjacency

	// Config for terrain cost discounts (e.g., Sumitomo Mines private).
	Config *gamedata.GameConfig
}

// ExtractTileLayContext reads partition states to build a TileLayContext.
func ExtractTileLayContext(
	companyIndex int,
	stateHistories []*simulator.StateHistory,
	cfg *gamedata.GameConfig,
	hexes []gamedata.HexDef,
	layout *PartitionLayout,
) *TileLayContext {
	compState := stateHistories[layout.CompanyPartitions[companyIndex]].Values.RawRowView(0)
	bankState := stateHistories[layout.BankPartition].Values.RawRowView(0)
	mapState := stateHistories[layout.MapPartition].Values.RawRowView(0)

	phase := int(bankState[BankTrainPhase])
	availColors := make(map[gamedata.TileColor]bool)
	if phase < len(cfg.Phases) {
		for _, colorName := range cfg.Phases[phase].TilesAvailable {
			switch colorName {
			case "yellow":
				availColors[gamedata.TileColorYellow] = true
			case "green":
				availColors[gamedata.TileColorGreen] = true
			case "brown":
				availColors[gamedata.TileColorBrown] = true
			}
		}
	}

	manifest := gamedata.Default1889TileManifest()
	tilesBase := BankTilesBase()
	tilesAvail := make([]float64, len(manifest))
	for i := range manifest {
		tilesAvail[i] = bankState[tilesBase+i]
	}

	return &TileLayContext{
		CompanyIndex:    companyIndex,
		CompanyTreasury: compState[CompTreasury],
		MapState:        mapState,
		Hexes:           hexes,
		TileDefs:        gamedata.Default1889Tiles(),
		Manifest:        manifest,
		TilesAvail:      tilesAvail,
		AvailableColors: availColors,
		Adjacency:       gamedata.Default1889Adjacency(),
		Config:          cfg,
	}
}

// LegalTileLayActions returns all legal tile placement/upgrade actions.
// Each action encodes: [ActionLayTile, hexIdx, tileID, orientation, manifestIdx, cost]
func LegalTileLayActions(ctx *TileLayContext) []Action {
	var actions []Action

	// Build manifest index lookup: tileID → manifest index.
	tileToManifest := make(map[int]int)
	for i, entry := range ctx.Manifest {
		tileToManifest[entry.TileID] = i
	}

	for hexIdx, hex := range ctx.Hexes {
		// Skip off-board and gray hexes (can't place tiles).
		if hex.Type == gamedata.HexOffBoard || hex.Type == gamedata.HexGray {
			continue
		}

		currentTileID := int(ctx.MapState[MapTileIdx(hexIdx)])
		currentOrientation := int(ctx.MapState[MapOrientIdx(hexIdx)])

		if currentTileID < 0 {
			// Empty hex: place a new yellow tile.
			actions = append(actions, ctx.legalNewPlacements(hexIdx, hex, tileToManifest)...)
		} else {
			// Hex has a tile: upgrade it.
			actions = append(actions, ctx.legalUpgrades(hexIdx, hex, currentTileID, currentOrientation, tileToManifest)...)
		}
	}

	return actions
}

// legalNewPlacements generates actions for placing a yellow tile on an empty hex.
func (ctx *TileLayContext) legalNewPlacements(hexIdx int, hex gamedata.HexDef, tileToManifest map[int]int) []Action {
	var actions []Action

	if !ctx.AvailableColors[gamedata.TileColorYellow] {
		return nil
	}

	cost := float64(hex.Cost)
	if ctx.CompanyTreasury < cost {
		return nil
	}

	// Determine valid yellow tiles for this hex type.
	for mIdx, entry := range ctx.Manifest {
		if ctx.TilesAvail[mIdx] <= 0 {
			continue
		}
		tileDef, ok := ctx.TileDefs[entry.TileID]
		if !ok || tileDef.Color != gamedata.TileColorYellow {
			continue
		}

		// Match tile type to hex type.
		if !tileMatchesHex(tileDef, hex) {
			continue
		}

		// Try all 6 orientations. For new placements on empty hexes,
		// we need at least one edge to connect to an adjacent hex that
		// has track or is a city/town.
		for orientation := 0; orientation < 6; orientation++ {
			if ctx.hasValidConnection(hexIdx, tileDef, orientation) {
				var a Action
				a.Values[ActionType] = ActionLayTile
				a.Values[ActionArg0] = float64(hexIdx)
				a.Values[ActionArg0+1] = float64(entry.TileID)
				a.Values[ActionArg0+2] = float64(orientation)
				a.Values[ActionArg0+3] = float64(mIdx)
				a.Values[ActionArg0+4] = cost
				actions = append(actions, a)
			}
		}
	}

	return actions
}

// legalUpgrades generates actions for upgrading an existing tile.
func (ctx *TileLayContext) legalUpgrades(hexIdx int, hex gamedata.HexDef, currentTileID, currentOrientation int, tileToManifest map[int]int) []Action {
	var actions []Action

	currentTile, ok := ctx.TileDefs[currentTileID]
	if !ok {
		return nil
	}

	cost := float64(hex.Cost)
	if ctx.CompanyTreasury < cost {
		return nil
	}

	for _, upgradeTileID := range currentTile.UpgradesTo {
		upgradeTile, ok := ctx.TileDefs[upgradeTileID]
		if !ok {
			continue
		}

		// Check color is available in current phase.
		if !ctx.AvailableColors[upgradeTile.Color] {
			continue
		}

		// Check manifest availability.
		mIdx, ok := tileToManifest[upgradeTileID]
		if !ok || ctx.TilesAvail[mIdx] <= 0 {
			continue
		}

		// Label must match.
		if currentTile.Label != upgradeTile.Label {
			continue
		}

		// Try all 6 orientations. The new tile must contain all paths
		// of the old tile (rotated) as a subset.
		for orientation := 0; orientation < 6; orientation++ {
			if pathSubset(currentTile, currentOrientation, upgradeTile, orientation) {
				var a Action
				a.Values[ActionType] = ActionLayTile
				a.Values[ActionArg0] = float64(hexIdx)
				a.Values[ActionArg0+1] = float64(upgradeTileID)
				a.Values[ActionArg0+2] = float64(orientation)
				a.Values[ActionArg0+3] = float64(mIdx)
				a.Values[ActionArg0+4] = cost
				actions = append(actions, a)
			}
		}
	}

	return actions
}

// tileMatchesHex checks if a tile's stop type is compatible with the hex type.
func tileMatchesHex(tile gamedata.TileDef, hex gamedata.HexDef) bool {
	switch hex.Type {
	case gamedata.HexCity:
		return tile.Stop == gamedata.StopCity || tile.Stop == gamedata.StopNone
	case gamedata.HexTown:
		return tile.Stop == gamedata.StopTown || tile.Stop == gamedata.StopNone
	case gamedata.HexEmpty:
		// Empty hexes accept track-only tiles, towns, or cities.
		return tile.Stop == gamedata.StopNone
	default:
		return false
	}
}

// rotateEdge rotates a hex edge by the given orientation offset.
// Edge values 0-5 are rotated; -1 (city/town node) is unchanged.
func rotateEdge(edge, orientation int) int {
	if edge < 0 {
		return edge // city/town node
	}
	return (edge + orientation) % 6
}

// rotatedSegments returns tile segments with edges rotated by orientation.
func rotatedSegments(tile gamedata.TileDef, orientation int) []gamedata.TrackSegment {
	segs := make([]gamedata.TrackSegment, len(tile.Segments))
	for i, s := range tile.Segments {
		segs[i] = gamedata.TrackSegment{
			From: rotateEdge(s.From, orientation),
			To:   rotateEdge(s.To, orientation),
		}
	}
	return segs
}

// pathSubset checks whether all paths of the old tile (at oldOrientation)
// are present in the new tile (at newOrientation).
// A path {a, b} matches if {a, b} or {b, a} exists.
func pathSubset(oldTile gamedata.TileDef, oldOrientation int, newTile gamedata.TileDef, newOrientation int) bool {
	oldSegs := rotatedSegments(oldTile, oldOrientation)
	newSegs := rotatedSegments(newTile, newOrientation)

	for _, old := range oldSegs {
		found := false
		for _, ns := range newSegs {
			if (old.From == ns.From && old.To == ns.To) ||
				(old.From == ns.To && old.To == ns.From) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// hasValidConnection checks if a tile placed at the given hex and orientation
// connects to at least one adjacent hex that has track or is a stop.
func (ctx *TileLayContext) hasValidConnection(hexIdx int, tile gamedata.TileDef, orientation int) bool {
	hex := ctx.Hexes[hexIdx]
	neighbors := ctx.Adjacency[hex.ID]

	segs := rotatedSegments(tile, orientation)

	for _, seg := range segs {
		// Check each edge endpoint (skip city/town node -1).
		for _, edge := range []int{seg.From, seg.To} {
			if edge < 0 || edge > 5 {
				continue
			}
			neighborID := neighbors[edge]
			if neighborID == "" {
				continue
			}
			// Find neighbor hex index.
			for nIdx, nh := range ctx.Hexes {
				if nh.ID != neighborID {
					continue
				}
				// Neighbor has a tile, or is a city/town/offboard/gray with track.
				neighborTileID := int(ctx.MapState[MapTileIdx(nIdx)])
				if neighborTileID >= 0 || nh.Type == gamedata.HexCity ||
					nh.Type == gamedata.HexTown || nh.Type == gamedata.HexOffBoard ||
					nh.Type == gamedata.HexGray {
					return true
				}
				break
			}
		}
	}

	return false
}
