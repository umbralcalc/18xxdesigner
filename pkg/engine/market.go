package engine

import (
	"github.com/umbralcalc/stochadex/pkg/simulator"
	"github.com/umbralcalc/18xxdesigner/pkg/gamedata"
)

// MarketIteration tracks share price positions for all companies on the stock market grid.
//
// State layout: 7 companies x 2 (row, col). See MarketRowIdx/MarketColIdx.
//
// Receives action_values and turn_values via params_from_upstream.
type MarketIteration struct {
	Config *gamedata.GameConfig
	Grid   *gamedata.MarketGrid
}

func (m *MarketIteration) Configure(partitionIndex int, settings *simulator.Settings) {}

func (m *MarketIteration) Iterate(
	params *simulator.Params,
	partitionIndex int,
	stateHistories []*simulator.StateHistory,
	timestepsHistory *simulator.CumulativeTimestepsHistory,
) []float64 {
	prev := stateHistories[partitionIndex].Values.RawRowView(0)
	width := len(m.Config.Companies) * MarketCompanyStride
	state := make([]float64, width)
	copy(state, prev)

	actionValues := params.Get("action_values")
	actionType := actionValues[ActionType]

	switch actionType {
	case ActionParCompany:
		m.handlePar(state, actionValues)
	case ActionSellShares:
		m.handleSell(state, actionValues)
	case ActionPayDividends:
		m.handleDividends(state, actionValues)
	case ActionWithhold:
		m.handleWithhold(state, actionValues)
	}

	return state
}

func (m *MarketIteration) handlePar(state []float64, action []float64) {
	companyID := int(action[ActionArg0])
	parRow := action[ActionArg0+3]
	parCol := action[ActionArg0+4]

	state[MarketRowIdx(companyID)] = parRow
	state[MarketColIdx(companyID)] = parCol
}

func (m *MarketIteration) handleSell(state []float64, action []float64) {
	companyID := int(action[ActionArg0])
	numShares := int(action[ActionArg0+1])

	row := int(state[MarketRowIdx(companyID)])
	col := int(state[MarketColIdx(companyID)])

	// Move down one row per share sold.
	for i := 0; i < numShares; i++ {
		row, col = m.Grid.MoveDown(row, col)
	}

	state[MarketRowIdx(companyID)] = float64(row)
	state[MarketColIdx(companyID)] = float64(col)
}

func (m *MarketIteration) handleDividends(state []float64, action []float64) {
	companyID := int(action[ActionArg0])

	row := int(state[MarketRowIdx(companyID)])
	col := int(state[MarketColIdx(companyID)])

	row, col = m.Grid.MoveRight(row, col)

	state[MarketRowIdx(companyID)] = float64(row)
	state[MarketColIdx(companyID)] = float64(col)
}

func (m *MarketIteration) handleWithhold(state []float64, action []float64) {
	companyID := int(action[ActionArg0])

	row := int(state[MarketRowIdx(companyID)])
	col := int(state[MarketColIdx(companyID)])

	row, col = m.Grid.MoveLeft(row, col)

	state[MarketRowIdx(companyID)] = float64(row)
	state[MarketColIdx(companyID)] = float64(col)
}

// MarketStateWidth returns the state width for the market partition.
func MarketStateWidth(cfg *gamedata.GameConfig) int {
	return len(cfg.Companies) * MarketCompanyStride
}

// InitMarketState returns the initial state for the market partition.
// All companies start with invalid position (-1, -1) until parred.
func InitMarketState(cfg *gamedata.GameConfig) []float64 {
	width := MarketStateWidth(cfg)
	state := make([]float64, width)
	for i := 0; i < len(cfg.Companies); i++ {
		state[MarketRowIdx(i)] = -1
		state[MarketColIdx(i)] = -1
	}
	return state
}
