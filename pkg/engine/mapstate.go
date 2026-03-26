package engine

import (
	"github.com/umbralcalc/stochadex/pkg/simulator"
	"github.com/umbralcalc/18xxdesigner/pkg/gamedata"
)

// MapIteration tracks the hex grid state: which tile is placed on each hex,
// its orientation, and which company tokens occupy the slots.
//
// State layout: numHexes × 3 (tile_id, orientation, token_bitfield).
// See MapTileIdx/MapOrientIdx/MapTokenIdx.
//
// Receives action_values and turn_values via params_from_upstream.
type MapIteration struct {
	Config *gamedata.GameConfig
	Hexes  []gamedata.HexDef
}

func (m *MapIteration) Configure(partitionIndex int, settings *simulator.Settings) {}

func (m *MapIteration) Iterate(
	params *simulator.Params,
	partitionIndex int,
	stateHistories []*simulator.StateHistory,
	timestepsHistory *simulator.CumulativeTimestepsHistory,
) []float64 {
	prev := stateHistories[partitionIndex].Values.RawRowView(0)
	state := make([]float64, len(prev))
	copy(state, prev)

	actionValues := params.Get("action_values")
	actionType := actionValues[ActionType]

	switch actionType {
	case ActionLayTile:
		m.handleLayTile(state, actionValues)
	case ActionPlaceToken:
		m.handlePlaceToken(state, actionValues)
	case ActionParCompany:
		m.handleParHomeToken(state, actionValues)
	}

	return state
}

func (m *MapIteration) handleLayTile(state []float64, action []float64) {
	hexIdx := int(action[ActionArg0])
	tileID := action[ActionArg0+1]
	orientation := action[ActionArg0+2]

	state[MapTileIdx(hexIdx)] = tileID
	state[MapOrientIdx(hexIdx)] = orientation
}

// handleParHomeToken places the home token when a company is parred.
func (m *MapIteration) handleParHomeToken(state []float64, action []float64) {
	companyID := int(action[ActionArg0])
	if companyID < 0 || companyID >= len(m.Config.Companies) {
		return
	}
	homeHex := m.Config.Companies[companyID].HomeHex
	for i, h := range m.Hexes {
		if h.ID == homeHex {
			companyBit := float64(int(1) << companyID)
			state[MapTokenIdx(i)] += companyBit
			return
		}
	}
}

func (m *MapIteration) handlePlaceToken(state []float64, action []float64) {
	hexIdx := int(action[ActionArg0])
	companyBit := action[ActionArg0+1] // bit to OR into token field

	state[MapTokenIdx(hexIdx)] += companyBit // use addition as simple bitfield
}

// MapStateWidth returns the total state width for the map partition.
func MapStateWidth(hexes []gamedata.HexDef) int {
	return len(hexes) * MapHexStride
}

// InitMapState returns the initial state for the map partition.
// Pre-printed tiles are placed; everything else is empty (tile_id = -1).
func InitMapState(hexes []gamedata.HexDef) []float64 {
	width := MapStateWidth(hexes)
	state := make([]float64, width)

	for i, h := range hexes {
		if h.PrePrintedTile >= 0 {
			state[MapTileIdx(i)] = float64(h.PrePrintedTile)
		} else {
			state[MapTileIdx(i)] = -1 // no tile placed
		}
		state[MapOrientIdx(i)] = 0
		state[MapTokenIdx(i)] = 0
	}

	return state
}
