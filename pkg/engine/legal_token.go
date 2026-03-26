package engine

import (
	"math/bits"

	"github.com/umbralcalc/stochadex/pkg/simulator"
	"github.com/umbralcalc/18xxdesigner/pkg/gamedata"
)

// TokenContext holds state needed for token placement legal move generation.
type TokenContext struct {
	CompanyIndex   int
	CompanyTreasury float64
	TokensRemaining int
	TokenCosts     []int // from CompanyDef

	MapState  []float64
	Hexes     []gamedata.HexDef
	TileDefs  map[int]gamedata.TileDef
	Adjacency gamedata.HexAdjacency
	Config    *gamedata.GameConfig
}

// ExtractTokenContext reads partition states to build a TokenContext.
func ExtractTokenContext(
	companyIndex int,
	stateHistories []*simulator.StateHistory,
	cfg *gamedata.GameConfig,
	hexes []gamedata.HexDef,
	layout *PartitionLayout,
) *TokenContext {
	compState := stateHistories[layout.CompanyPartitions[companyIndex]].Values.RawRowView(0)
	mapState := stateHistories[layout.MapPartition].Values.RawRowView(0)

	return &TokenContext{
		CompanyIndex:    companyIndex,
		CompanyTreasury: compState[CompTreasury],
		TokensRemaining: int(compState[CompTokensRemain]),
		TokenCosts:      cfg.Companies[companyIndex].TokenCosts,
		MapState:        mapState,
		Hexes:           hexes,
		TileDefs:        gamedata.Default1889Tiles(),
		Adjacency:       gamedata.Default1889Adjacency(),
		Config:          cfg,
	}
}

// LegalTokenActions returns all legal token placement actions for a company.
// A token can be placed on a city hex that:
//   - Has an open slot (not fully tokened)
//   - Is reachable by the company's existing track network
//   - The company can afford the token cost
//   - Is not the home hex (home token placed on float, handled separately)
func LegalTokenActions(ctx *TokenContext) []Action {
	if ctx.TokensRemaining <= 0 {
		return nil
	}

	// Determine token cost (index into TokenCosts by tokens already placed).
	tokensPlaced := len(ctx.TokenCosts) - ctx.TokensRemaining
	cost := 0
	if tokensPlaced < len(ctx.TokenCosts) {
		cost = ctx.TokenCosts[tokensPlaced]
	}

	if ctx.CompanyTreasury < float64(cost) {
		return nil
	}

	// Find hexes reachable by the company's track that have open city slots.
	var actions []Action
	companyBit := 1 << ctx.CompanyIndex

	for i, hex := range ctx.Hexes {
		// Must be a city with slots.
		slots := hexSlots(hex, ctx.MapState, i, ctx.TileDefs)
		if slots <= 0 {
			continue
		}

		// Check if company already has a token here.
		tokenField := int(ctx.MapState[MapTokenIdx(i)])
		if tokenField&companyBit != 0 {
			continue
		}

		// Check if there's an open slot.
		tokensHere := bits.OnesCount(uint(tokenField))
		if tokensHere >= slots {
			continue
		}

		// Check reachability: company must have a token on a connected hex.
		// For simplicity, check if there's ANY company token on the map and
		// the hex has placed track. Full graph reachability is a refinement.
		if !hasTrackOnHex(ctx.MapState, i, hex) {
			continue
		}

		var a Action
		a.Values[ActionType] = ActionPlaceToken
		a.Values[ActionArg0] = float64(i)              // hex index
		a.Values[ActionArg0+1] = float64(companyBit)   // bit to set
		a.Values[ActionArg0+2] = float64(cost)         // token cost
		actions = append(actions, a)
	}

	return actions
}

// hexSlots returns the number of token slots for a hex.
func hexSlots(hex gamedata.HexDef, mapState []float64, hexIdx int, tileDefs map[int]gamedata.TileDef) int {
	tileID := int(mapState[MapTileIdx(hexIdx)])
	if tileID >= 0 {
		if tile, ok := tileDefs[tileID]; ok && tile.Stop == gamedata.StopCity {
			return tile.Slots
		}
	}
	// Built-in slots (gray cities like Uwajima).
	if hex.Type == gamedata.HexCity || hex.Type == gamedata.HexGray {
		return hex.Slots
	}
	return 0
}

// hasTrackOnHex returns true if the hex has placed track or is a built-in track hex.
func hasTrackOnHex(mapState []float64, hexIdx int, hex gamedata.HexDef) bool {
	if int(mapState[MapTileIdx(hexIdx)]) >= 0 {
		return true
	}
	return hex.Type == gamedata.HexGray || hex.Type == gamedata.HexOffBoard
}
