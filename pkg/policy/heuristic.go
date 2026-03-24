package policy

import (
	"github.com/umbralcalc/ttdesigner/pkg/engine"
)

// HeuristicAgent implements a simple rule-based agent for 1889.
//
// Stock Round strategy:
//   - If cash > 2x cheapest par price and no company parred yet, par one.
//   - If a floated company has shares in IPO and we can afford it, buy.
//   - Otherwise pass.
//
// Operating Round: always pass (stub for Step 2 skeleton).
type HeuristicAgent struct{}

func (h *HeuristicAgent) ChooseAction(ctx *engine.GameContext) []float64 {
	turnState := ctx.TurnState
	roundType := turnState[engine.TurnRoundType]

	switch roundType {
	case engine.RoundStockRound:
		return h.chooseStockRoundAction(ctx)
	case engine.RoundOperatingRound:
		return h.chooseOperatingRoundAction(ctx)
	default:
		// Private auction: pass for now.
		return passAction()
	}
}

func (h *HeuristicAgent) chooseStockRoundAction(ctx *engine.GameContext) []float64 {
	playerIndex := int(ctx.TurnState[engine.TurnActiveID])

	srCtx := engine.ExtractSRContext(
		playerIndex,
		ctx.StateHistories,
		ctx.Config,
		ctx.MarketGrid,
		ctx.NumPlayers,
		ctx.Layout,
	)

	actions := engine.LegalStockRoundActions(srCtx)
	if len(actions) <= 1 {
		return passAction() // only pass is legal
	}

	// Priority 1: Par a company if we haven't parred one yet and can afford it.
	// Choose the cheapest par price to conserve cash.
	var bestPar *engine.Action
	bestParCost := float64(999999)
	for i := range actions {
		if actions[i].Values[engine.ActionType] == engine.ActionParCompany {
			cost := actions[i].Values[engine.ActionArg0+1] * actions[i].Values[engine.ActionArg0+2]
			if cost < bestParCost {
				bestParCost = cost
				a := actions[i]
				bestPar = &a
			}
		}
	}

	// Only par if we don't already hold shares in 2+ companies.
	companiesHeld := 0
	for _, shares := range srCtx.PlayerShares {
		if shares > 0 {
			companiesHeld++
		}
	}
	if bestPar != nil && companiesHeld < 2 {
		return bestPar.Values[:]
	}

	// Priority 2: Buy a share of the cheapest floated company with IPO shares.
	var bestBuy *engine.Action
	bestBuyCost := float64(999999)
	for i := range actions {
		if actions[i].Values[engine.ActionType] == engine.ActionBuyShare {
			cost := actions[i].Values[engine.ActionArg0+1]
			if cost < bestBuyCost {
				bestBuyCost = cost
				a := actions[i]
				bestBuy = &a
			}
		}
	}
	if bestBuy != nil {
		return bestBuy.Values[:]
	}

	return passAction()
}

func (h *HeuristicAgent) chooseOperatingRoundAction(ctx *engine.GameContext) []float64 {
	// Stub: pass during OR. Will be expanded in later steps.
	return passAction()
}

func passAction() []float64 {
	action := make([]float64, engine.ActionStateWidth)
	action[engine.ActionType] = engine.ActionPass
	return action
}
