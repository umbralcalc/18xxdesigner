package engine

import (
	"github.com/umbralcalc/stochadex/pkg/simulator"
	"github.com/umbralcalc/18xxdesigner/pkg/gamedata"
)

// CompanyIteration tracks a single company's treasury, trains, tokens, and shares.
//
// State layout: see CompTreasury..CompStateWidth constants in state.go.
//
// Receives action_values and turn_values via params_from_upstream.
type CompanyIteration struct {
	CompanyIndex int
	Config       *gamedata.GameConfig
}

func (c *CompanyIteration) Configure(partitionIndex int, settings *simulator.Settings) {}

func (c *CompanyIteration) Iterate(
	params *simulator.Params,
	partitionIndex int,
	stateHistories []*simulator.StateHistory,
	timestepsHistory *simulator.CumulativeTimestepsHistory,
) []float64 {
	prev := stateHistories[partitionIndex].Values.RawRowView(0)
	state := make([]float64, CompStateWidth)
	copy(state, prev)

	turnValues := params.Get("turn_values")
	actionValues := params.Get("action_values")
	bankValues := params.Get("bank_values")
	actionType := actionValues[ActionType]

	// Handle actions directed at this company.
	switch actionType {
	case ActionParCompany:
		c.handlePar(state, actionValues, turnValues)
	case ActionBuyShare:
		c.handleBuyShare(state, actionValues)
	case ActionSellShares:
		c.handleSellShares(state, actionValues)
	case ActionBuyTrain:
		c.handleBuyTrain(state, actionValues, turnValues)
	case ActionPayDividends:
		c.handlePayDividends(state, actionValues, turnValues)
	case ActionWithhold:
		c.handleWithhold(state, actionValues, turnValues)
	case ActionPlaceToken:
		c.handlePlaceToken(state, actionValues, turnValues)
	case ActionBuyTrainFromCompany:
		c.handleBuyTrainFromCompany(state, actionValues, turnValues)
	case ActionExchangeTrain:
		c.handleExchangeTrain(state, actionValues, turnValues)
	}

	// Check for train rusting after processing actions (bank phase may have
	// advanced this step via upstream params).
	c.checkTrainRusting(state, bankValues)

	return state
}

func (c *CompanyIteration) handlePar(state []float64, action, turn []float64) {
	targetCompany := int(action[ActionArg0])
	if targetCompany != c.CompanyIndex {
		return
	}

	parPrice := action[ActionArg0+1]
	numShares := action[ActionArg0+2]
	playerID := turn[TurnActiveID]

	state[CompFloated] = 0 // not yet floated; floats when 50% sold
	state[CompParPrice] = parPrice
	state[CompPresident] = playerID
	state[CompSharesIPO] = 10 - numShares // 10 total shares, minus bought
	state[CompTokensRemain] = float64(len(c.Config.Companies[c.CompanyIndex].TokenCosts))
}

func (c *CompanyIteration) handleBuyShare(state []float64, action []float64) {
	targetCompany := int(action[ActionArg0])
	if targetCompany != c.CompanyIndex {
		return
	}
	// Decrease IPO shares. Check if company should float.
	state[CompSharesIPO] -= 1
	if state[CompSharesIPO] <= 5 && state[CompFloated] == 0 {
		// 50% sold (5 of 10 shares) → float the company.
		state[CompFloated] = 1
		state[CompTreasury] = state[CompParPrice] * 10 // full capitalisation
	}
}

func (c *CompanyIteration) handleSellShares(state []float64, action []float64) {
	targetCompany := int(action[ActionArg0])
	if targetCompany != c.CompanyIndex {
		return
	}
	numShares := action[ActionArg0+1]
	state[CompSharesMarket] += numShares
}

func (c *CompanyIteration) handleBuyTrain(state []float64, action, turn []float64) {
	// Only process if this is the active company in an OR.
	if turn[TurnActiveType] != ActiveCompany || int(turn[TurnActiveID]) != c.CompanyIndex {
		return
	}
	trainIdx := int(action[ActionArg0])
	cost := action[ActionArg0+1]

	state[CompTreasury] -= cost
	state[CompTrainsBase+trainIdx] += 1
}

func (c *CompanyIteration) handlePayDividends(state []float64, action, turn []float64) {
	targetCompany := int(action[ActionArg0])
	if targetCompany != c.CompanyIndex {
		return
	}
	revenue := action[ActionArg0+1]
	state[CompLastRevenue] = revenue
}

func (c *CompanyIteration) handleWithhold(state []float64, action, turn []float64) {
	targetCompany := int(action[ActionArg0])
	if targetCompany != c.CompanyIndex {
		return
	}
	revenue := action[ActionArg0+1]
	state[CompTreasury] += revenue
	state[CompLastRevenue] = revenue
}

// checkTrainRusting removes trains that have rusted based on the current bank phase.
// A train rusts when the train type named in its RustsOn field has been purchased,
// which triggers a phase advance. We detect this by checking the current phase
// against the phase triggered by the RustsOn train type.
func (c *CompanyIteration) checkTrainRusting(state []float64, bankValues []float64) {
	currentPhase := int(bankValues[BankTrainPhase])
	for i, train := range c.Config.Trains {
		if train.RustsOn == "" || state[CompTrainsBase+i] <= 0 {
			continue
		}
		// Find the phase triggered by the RustsOn train type.
		for p, phase := range c.Config.Phases {
			if phase.TriggerTrain == train.RustsOn && currentPhase >= p {
				state[CompTrainsBase+i] = 0
				break
			}
		}
	}
}

// handleBuyTrainFromCompany processes an inter-company train sale.
// Action layout: [type, trainIdx, cost, sellerCompanyIdx]
// The active company (buyer) pays cost and gains the train.
// The seller company loses the train and gains the cash.
func (c *CompanyIteration) handleBuyTrainFromCompany(state []float64, action, turn []float64) {
	buyerIdx := int(turn[TurnActiveID])
	trainIdx := int(action[ActionArg0])
	cost := action[ActionArg0+1]
	sellerIdx := int(action[ActionArg0+2])

	if c.CompanyIndex == buyerIdx {
		state[CompTreasury] -= cost
		state[CompTrainsBase+trainIdx] += 1
	}
	if c.CompanyIndex == sellerIdx {
		state[CompTreasury] += cost
		state[CompTrainsBase+trainIdx] -= 1
	}
}

// handleExchangeTrain processes a train exchange with the depot.
// Action layout: [type, newTrainIdx, cost, oldTrainIdx]
// The active company pays cost, returns old train, and gets new train.
func (c *CompanyIteration) handleExchangeTrain(state []float64, action, turn []float64) {
	if turn[TurnActiveType] != ActiveCompany || int(turn[TurnActiveID]) != c.CompanyIndex {
		return
	}
	newTrainIdx := int(action[ActionArg0])
	cost := action[ActionArg0+1]
	oldTrainIdx := int(action[ActionArg0+2])

	state[CompTreasury] -= cost
	state[CompTrainsBase+oldTrainIdx] -= 1
	state[CompTrainsBase+newTrainIdx] += 1
}

func (c *CompanyIteration) handlePlaceToken(state []float64, action, turn []float64) {
	if turn[TurnActiveType] != ActiveCompany || int(turn[TurnActiveID]) != c.CompanyIndex {
		return
	}
	cost := action[ActionArg0+2]
	state[CompTreasury] -= cost
	state[CompTokensRemain] -= 1
}

// InitCompanyState returns the initial state for a company partition.
func InitCompanyState() []float64 {
	state := make([]float64, CompStateWidth)
	state[CompSharesIPO] = 10 // all shares start in IPO
	return state
}
