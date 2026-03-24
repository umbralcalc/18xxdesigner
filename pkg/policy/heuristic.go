package policy

import (
	"github.com/umbralcalc/stochadex/pkg/simulator"
	"github.com/umbralcalc/ttdesigner/pkg/gamedata"
	"github.com/umbralcalc/ttdesigner/pkg/engine"
)

// HeuristicAgent implements a simple rule-based agent for 1889.
//
// Stock Round strategy:
//   - If cash > 2x cheapest par price and holds < 2 companies, par one.
//   - If a floated company has IPO shares and we can afford it, buy.
//   - Otherwise pass.
//
// Operating Round strategy:
//   - Tile lay: pick the first legal tile placement (if any).
//   - Token: pass (stub).
//   - Routes: pass/withhold (stub until route-finding is implemented).
//   - Buy train: pass (stub).
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
		return passAction()
	}

	// Priority 1: Par a company (cheapest par) if holding < 2 companies.
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

	companiesHeld := 0
	for _, shares := range srCtx.PlayerShares {
		if shares > 0 {
			companiesHeld++
		}
	}
	if bestPar != nil && companiesHeld < 2 {
		return bestPar.Values[:]
	}

	// Priority 2: Buy cheapest IPO share.
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
	orStep := ctx.TurnState[engine.TurnActionStep]
	companyIndex := int(ctx.TurnState[engine.TurnActiveID])

	// Check if company is floated; unfloated companies pass all steps.
	compState := ctx.StateHistories[ctx.Layout.CompanyPartitions[companyIndex]].Values.RawRowView(0)
	if compState[engine.CompFloated] == 0 {
		return passAction()
	}

	switch orStep {
	case engine.ORStepTileLay:
		return h.chooseTileLay(ctx, companyIndex)
	case engine.ORStepToken:
		return passAction() // stub
	case engine.ORStepRoutes:
		return passAction() // stub until route-finding
	case engine.ORStepBuyTrain:
		return passAction() // stub
	default:
		return passAction()
	}
}

func (h *HeuristicAgent) chooseTileLay(ctx *engine.GameContext, companyIndex int) []float64 {
	tileCtx := extractTileLayContextFromGameCtx(ctx, companyIndex)
	actions := engine.LegalTileLayActions(tileCtx)

	if len(actions) == 0 {
		return passAction()
	}

	// Pick the first legal tile placement (simple heuristic).
	// Prefer placements near the company's home hex — but for now, just pick first.
	return actions[0].Values[:]
}

// extractTileLayContextFromGameCtx bridges GameContext → TileLayContext.
func extractTileLayContextFromGameCtx(ctx *engine.GameContext, companyIndex int) *engine.TileLayContext {
	return engine.ExtractTileLayContext(
		companyIndex,
		ctx.StateHistories,
		ctx.Config,
		gamedata.Default1889Map(),
		ctx.Layout,
	)
}

// Ensure HeuristicAgent satisfies the Agent interface.
var _ engine.Agent = (*HeuristicAgent)(nil)

// stateHistoryValue is a helper to read a value from a partition's state.
func stateHistoryValue(
	stateHistories []*simulator.StateHistory,
	partitionIdx int,
	stateIdx int,
) float64 {
	return stateHistories[partitionIdx].Values.At(0, stateIdx)
}

func passAction() []float64 {
	action := make([]float64, engine.ActionStateWidth)
	action[engine.ActionType] = engine.ActionPass
	return action
}
