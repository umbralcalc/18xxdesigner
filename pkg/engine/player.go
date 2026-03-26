package engine

import (
	"github.com/umbralcalc/stochadex/pkg/simulator"
	"github.com/umbralcalc/18xxdesigner/pkg/gamedata"
)

// PlayerIteration tracks a single player's cash, shares, privates, and cert count.
//
// State layout: see PlayerCash..PlayerStateWidth constants in state.go.
//
// Receives action_values and turn_values via params_from_upstream.
type PlayerIteration struct {
	PlayerIndex int
	Config      *gamedata.GameConfig
}

func (p *PlayerIteration) Configure(partitionIndex int, settings *simulator.Settings) {}

func (p *PlayerIteration) Iterate(
	params *simulator.Params,
	partitionIndex int,
	stateHistories []*simulator.StateHistory,
	timestepsHistory *simulator.CumulativeTimestepsHistory,
) []float64 {
	prev := stateHistories[partitionIndex].Values.RawRowView(0)
	state := make([]float64, PlayerStateWidth)
	copy(state, prev)

	turnValues := params.Get("turn_values")
	actionValues := params.Get("action_values")
	bankValues := params.Get("bank_values")
	actionType := actionValues[ActionType]

	// Close privates at phase 5+ (first 5-train triggers close_companies).
	p.checkPrivateClosure(state, bankValues)

	activeType := turnValues[TurnActiveType]
	activeID := int(turnValues[TurnActiveID])

	// Dividends affect ALL players (each shareholder gets paid).
	// Handle these before the active-player check.
	switch actionType {
	case ActionPayDividends:
		p.handleReceiveDividend(state, actionValues)
	}

	// Only process remaining actions if this player is the active entity.
	isActivePlayer := activeType == ActivePlayer && activeID == p.PlayerIndex
	if !isActivePlayer {
		return state
	}

	switch actionType {
	case ActionBuyShare:
		p.handleBuyShare(state, actionValues)
	case ActionSellShares:
		p.handleSellShares(state, actionValues)
	case ActionParCompany:
		p.handleParCompany(state, actionValues)
	case ActionPass:
		state[PlayerPassed] = 1.0
	}

	return state
}

func (p *PlayerIteration) handleBuyShare(state []float64, action []float64) {
	companyID := int(action[ActionArg0])
	cost := action[ActionArg0+1]

	if state[PlayerCash] < cost {
		return
	}

	state[PlayerCash] -= cost
	state[PlayerShareIdx(companyID)] += 1
	state[PlayerCertCount] += 1
}

func (p *PlayerIteration) handleSellShares(state []float64, action []float64) {
	companyID := int(action[ActionArg0])
	numShares := action[ActionArg0+1]
	revenue := action[ActionArg0+2]

	state[PlayerShareIdx(companyID)] -= numShares
	state[PlayerCash] += revenue
	state[PlayerCertCount] -= numShares
}

func (p *PlayerIteration) handleParCompany(state []float64, action []float64) {
	companyID := int(action[ActionArg0])
	parPrice := action[ActionArg0+1]
	numShares := action[ActionArg0+2] // shares bought (usually 2 = 20%)

	cost := parPrice * numShares
	if state[PlayerCash] < cost {
		return
	}

	state[PlayerCash] -= cost
	state[PlayerShareIdx(companyID)] += numShares
	state[PlayerCertCount] += numShares
}

// checkPrivateClosure zeroes out all private holdings when phase >= 3 (5-train phase).
// Phase indices: 0=2-train, 1=3-train, 2=4-train, 3=5-train, 4=6-train, 5=D-train.
func (p *PlayerIteration) checkPrivateClosure(state []float64, bankValues []float64) {
	phase := int(bankValues[BankTrainPhase])
	if phase < 3 { // phase 3 = 5-train
		return
	}
	for i := range p.Config.Privates {
		state[PlayerPrivateIdx(i)] = 0
	}
}

func (p *PlayerIteration) handleReceiveDividend(state []float64, action []float64) {
	companyID := int(action[ActionArg0])
	totalRevenue := action[ActionArg0+1]

	sharesHeld := state[PlayerShareIdx(companyID)]
	if sharesHeld <= 0 {
		return
	}

	// Each share receives 1/10 of total revenue (10 shares total).
	perShare := totalRevenue / 10.0
	state[PlayerCash] += perShare * sharesHeld
}

// InitPlayerState returns the initial state for a player partition.
func InitPlayerState(cfg *gamedata.GameConfig, numPlayers int) []float64 {
	state := make([]float64, PlayerStateWidth)
	state[PlayerCash] = float64(cfg.StartingCash[numPlayers])
	return state
}
