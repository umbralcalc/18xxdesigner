package engine

import (
	"github.com/umbralcalc/stochadex/pkg/simulator"
)

// TurnControllerIteration is the master FSM that controls game flow.
// It reads the action from the action partition (via params_from_upstream)
// and advances the game phase: Private Auction → SR → OR(s) → SR → ...
//
// State layout: see TurnGamePhase..TurnStateWidth constants in state.go.
type TurnControllerIteration struct {
	NumPlayers   int
	NumCompanies int
	ORsPerPhase  []int // indexed by game phase: how many ORs per SR
}

func (t *TurnControllerIteration) Configure(partitionIndex int, settings *simulator.Settings) {
	// No-op: all config comes from fields set at construction time.
}

func (t *TurnControllerIteration) Iterate(
	params *simulator.Params,
	partitionIndex int,
	stateHistories []*simulator.StateHistory,
	timestepsHistory *simulator.CumulativeTimestepsHistory,
) []float64 {
	prev := stateHistories[partitionIndex].Values.RawRowView(0)
	state := make([]float64, TurnStateWidth)
	copy(state, prev)

	// Read the action that was chosen this step.
	actionValues := params.Get("action_values")
	actionType := actionValues[ActionType]

	roundType := state[TurnRoundType]
	phase := int(state[TurnGamePhase])

	switch roundType {
	case RoundPrivateAuction:
		t.advancePrivateAuction(state, actionType)
	case RoundStockRound:
		t.advanceStockRound(state, actionType)
	case RoundOperatingRound:
		t.advanceOperatingRound(state, actionType, phase)
	}

	return state
}

// advancePrivateAuction handles the initial private company auction.
// For now, simplified: each player either bids or passes, then move to SR.
func (t *TurnControllerIteration) advancePrivateAuction(state []float64, actionType float64) {
	activePlayer := int(state[TurnActiveID])

	// Move to next player.
	nextPlayer := (activePlayer + 1) % t.NumPlayers
	state[TurnActiveID] = float64(nextPlayer)

	// If we've gone around the table, transition to Stock Round.
	// (Simplified: real auction has multiple rounds with bidding.)
	if nextPlayer == 0 {
		state[TurnRoundType] = RoundStockRound
		state[TurnActiveID] = state[TurnPriorityDeal]
		state[TurnActiveType] = ActivePlayer
	}
}

// advanceStockRound handles player turns in the stock round.
// Priority deal moves to the player left of the last player who acted (bought/sold/parred).
func (t *TurnControllerIteration) advanceStockRound(state []float64, actionType float64) {
	activePlayer := int(state[TurnActiveID])
	nextPlayer := (activePlayer + 1) % t.NumPlayers

	if actionType == ActionPass {
		// Increment consecutive pass counter.
		state[TurnActionStep] += 1
		if int(state[TurnActionStep]) >= t.NumPlayers {
			t.transitionToOR(state)
			return
		}
	} else {
		// Player took an action → reset consecutive pass counter.
		// Priority deal moves to the next player after the actor.
		state[TurnActionStep] = 0
		state[TurnPriorityDeal] = float64(nextPlayer)
	}

	state[TurnActiveID] = float64(nextPlayer)
}

// transitionToOR moves from Stock Round to Operating Round(s).
func (t *TurnControllerIteration) transitionToOR(state []float64) {
	phase := int(state[TurnGamePhase])
	ors := 1
	if phase < len(t.ORsPerPhase) {
		ors = t.ORsPerPhase[phase]
	}
	state[TurnRoundType] = RoundOperatingRound
	state[TurnORNumber] = 1
	state[TurnORsThisSet] = float64(ors)
	state[TurnActiveType] = ActiveCompany
	state[TurnActiveID] = 0
	state[TurnActionStep] = 0
}

// advanceOperatingRound handles company turns in the operating round.
func (t *TurnControllerIteration) advanceOperatingRound(state []float64, actionType float64, phase int) {
	activeCompany := int(state[TurnActiveID])

	// Move to next company. Skip unfloated companies (handled by action layer returning pass).
	nextCompany := activeCompany + 1
	if nextCompany >= t.NumCompanies {
		// All companies have operated. Check if more ORs remain.
		orNum := int(state[TurnORNumber])
		orsThisSet := int(state[TurnORsThisSet])
		if orNum < orsThisSet {
			// Start next OR.
			state[TurnORNumber] = float64(orNum + 1)
			state[TurnActiveID] = 0
			state[TurnActionStep] = 0
		} else {
			// All ORs done → back to Stock Round.
			state[TurnRoundType] = RoundStockRound
			state[TurnActiveType] = ActivePlayer
			state[TurnActiveID] = state[TurnPriorityDeal]
			state[TurnActionStep] = 0
			state[TurnORNumber] = 0
		}
	} else {
		state[TurnActiveID] = float64(nextCompany)
		state[TurnActionStep] = 0
	}
}
