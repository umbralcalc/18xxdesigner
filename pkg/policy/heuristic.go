package policy

import (
	"github.com/umbralcalc/stochadex/pkg/simulator"
	"github.com/umbralcalc/18xxdesigner/pkg/engine"
	"github.com/umbralcalc/18xxdesigner/pkg/gamedata"
)

// HeuristicAgent implements a rule-based agent for 1889.
//
// Stock Round:
//   - Par companies at cheapest price until holding 2+.
//   - Buy shares of highest-revenue floated company.
//   - Sell shares of companies with decreasing price (tanking).
//
// Operating Round:
//   - Tile: prefer placements on hexes adjacent to own tokens.
//   - Token: place cheapest available.
//   - Routes: use OptimalRouteAssignment; pay dividends unless company
//     needs to save for a train (withhold if revenue < 2x cheapest train cost).
//   - Trains: buy cheapest available when under train limit.
type HeuristicAgent struct{}

func (h *HeuristicAgent) ChooseAction(ctx *engine.GameContext) []float64 {
	roundType := ctx.TurnState[engine.TurnRoundType]

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

	// Count companies held.
	companiesHeld := 0
	for _, shares := range srCtx.PlayerShares {
		if shares > 0 {
			companiesHeld++
		}
	}

	// Priority 1: Par a company if holding fewer than 2.
	if companiesHeld < 2 {
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
		if bestPar != nil {
			return bestPar.Values[:]
		}
	}

	// Collect last revenue per company for scoring buys.
	companyRevenue := make([]float64, len(ctx.Config.Companies))
	for i := range ctx.Config.Companies {
		cs := ctx.StateHistories[ctx.Layout.CompanyPartitions[i]].Values.RawRowView(0)
		companyRevenue[i] = cs[engine.CompLastRevenue]
	}

	// Priority 2: Buy share of highest-revenue floated company.
	var bestBuy *engine.Action
	bestBuyScore := -1.0
	for i := range actions {
		if actions[i].Values[engine.ActionType] == engine.ActionBuyShare {
			compID := int(actions[i].Values[engine.ActionArg0])
			score := companyRevenue[compID]
			if score > bestBuyScore {
				bestBuyScore = score
				a := actions[i]
				bestBuy = &a
			}
		}
	}
	if bestBuy != nil {
		return bestBuy.Values[:]
	}

	// Priority 3: Sell shares of tanking companies (withheld last OR and share price declining).
	for i := range actions {
		if actions[i].Values[engine.ActionType] == engine.ActionSellShares {
			compID := int(actions[i].Values[engine.ActionArg0])
			cs := ctx.StateHistories[ctx.Layout.CompanyPartitions[compID]].Values.RawRowView(0)
			// Only sell if company has operated and is withholding (revenue goes to treasury, not shareholders).
			hasOperated := cs[engine.CompOperatedThisOR] > 0 || cs[engine.CompLastRevenue] > 0
			if hasOperated && companyRevenue[compID] == 0 && srCtx.PlayerShares[compID] > 2 {
				return actions[i].Values[:]
			}
		}
	}

	return passAction()
}

func (h *HeuristicAgent) chooseOperatingRoundAction(ctx *engine.GameContext) []float64 {
	orStep := ctx.TurnState[engine.TurnActionStep]
	companyIndex := int(ctx.TurnState[engine.TurnActiveID])

	compState := ctx.StateHistories[ctx.Layout.CompanyPartitions[companyIndex]].Values.RawRowView(0)
	if compState[engine.CompFloated] == 0 {
		return passAction()
	}

	switch orStep {
	case engine.ORStepTileLay:
		return h.chooseTileLay(ctx, companyIndex)
	case engine.ORStepToken:
		return h.chooseToken(ctx, companyIndex)
	case engine.ORStepRoutes:
		return h.chooseRouteAction(ctx, companyIndex)
	case engine.ORStepBuyTrain:
		return h.chooseBuyTrain(ctx, companyIndex)
	default:
		return passAction()
	}
}

func (h *HeuristicAgent) chooseTileLay(ctx *engine.GameContext, companyIndex int) []float64 {
	tileCtx := engine.ExtractTileLayContext(
		companyIndex,
		ctx.StateHistories,
		ctx.Config,
		gamedata.Default1889Map(),
		ctx.Layout,
	)
	actions := engine.LegalTileLayActions(tileCtx)
	if len(actions) == 0 {
		return passAction()
	}

	// Prefer tiles on hexes adjacent to company tokens (extends routes).
	mapState := ctx.StateHistories[ctx.Layout.MapPartition].Values.RawRowView(0)
	hexes := gamedata.Default1889Map()
	adjacency := gamedata.Default1889Adjacency()
	companyBit := float64(int(1) << companyIndex)

	tokenHexes := make(map[int]bool)
	for i := range hexes {
		tokens := mapState[engine.MapTokenIdx(i)]
		if int(tokens)&int(companyBit) != 0 {
			tokenHexes[i] = true
		}
	}

	// Build hex ID → index lookup.
	hexIDToIdx := make(map[string]int, len(hexes))
	for i := range hexes {
		hexIDToIdx[hexes[i].ID] = i
	}

	// Score each action: 2 if on token hex, 1 if adjacent to token hex, 0 otherwise.
	bestScore := -1
	bestIdx := 0
	for i := range actions {
		hexIdx := int(actions[i].Values[engine.ActionArg0])
		score := 0
		if tokenHexes[hexIdx] {
			score = 2
		} else if adj, ok := adjacency[hexes[hexIdx].ID]; ok {
			for _, neighborID := range adj {
				if neighborID == "" {
					continue
				}
				if j, ok := hexIDToIdx[neighborID]; ok && tokenHexes[j] {
					score = 1
					break
				}
			}
		}
		if score > bestScore {
			bestScore = score
			bestIdx = i
		}
	}

	return actions[bestIdx].Values[:]
}

func (h *HeuristicAgent) chooseToken(ctx *engine.GameContext, companyIndex int) []float64 {
	tokenCtx := engine.ExtractTokenContext(
		companyIndex,
		ctx.StateHistories,
		ctx.Config,
		gamedata.Default1889Map(),
		ctx.Layout,
	)
	actions := engine.LegalTokenActions(tokenCtx)
	if len(actions) == 0 {
		return passAction()
	}
	return actions[0].Values[:]
}

func (h *HeuristicAgent) chooseRouteAction(ctx *engine.GameContext, companyIndex int) []float64 {
	compState := ctx.StateHistories[ctx.Layout.CompanyPartitions[companyIndex]].Values.RawRowView(0)
	mapState := ctx.StateHistories[ctx.Layout.MapPartition].Values.RawRowView(0)
	bankState := ctx.StateHistories[ctx.Layout.BankPartition].Values.RawRowView(0)
	gamePhase := int(bankState[engine.BankTrainPhase])

	hexes := gamedata.Default1889Map()
	tileDefs := gamedata.Default1889Tiles()
	adjacency := gamedata.Default1889Adjacency()

	graph := engine.BuildTrackGraph(mapState, hexes, tileDefs, adjacency)

	var trains []int
	var distances []int
	for i, tr := range ctx.Config.Trains {
		count := int(compState[engine.CompTrainsBase+i])
		for j := 0; j < count; j++ {
			trains = append(trains, i)
			distances = append(distances, tr.Distance)
		}
	}

	if len(trains) == 0 {
		return passAction()
	}

	_, totalRevenue := engine.OptimalRouteAssignment(graph, companyIndex, trains, distances, gamePhase)

	if totalRevenue == 0 {
		return passAction()
	}

	// Withhold if company needs to save for a train purchase.
	treasury := compState[engine.CompTreasury]
	cheapestTrainCost := h.cheapestAvailableTrainCost(ctx)

	if cheapestTrainCost > 0 && treasury+float64(totalRevenue) < float64(cheapestTrainCost)*2 {
		action := make([]float64, engine.ActionStateWidth)
		action[engine.ActionType] = engine.ActionWithhold
		action[engine.ActionArg0] = float64(companyIndex)
		action[engine.ActionArg0+1] = float64(totalRevenue)
		return action
	}

	action := make([]float64, engine.ActionStateWidth)
	action[engine.ActionType] = engine.ActionPayDividends
	action[engine.ActionArg0] = float64(companyIndex)
	action[engine.ActionArg0+1] = float64(totalRevenue)
	return action
}

func (h *HeuristicAgent) chooseBuyTrain(ctx *engine.GameContext, companyIndex int) []float64 {
	compState := ctx.StateHistories[ctx.Layout.CompanyPartitions[companyIndex]].Values.RawRowView(0)
	bankState := ctx.StateHistories[ctx.Layout.BankPartition].Values.RawRowView(0)
	treasury := compState[engine.CompTreasury]

	totalTrains := 0
	for i := range ctx.Config.Trains {
		totalTrains += int(compState[engine.CompTrainsBase+i])
	}

	gamePhase := int(bankState[engine.BankTrainPhase])
	trainLimit := ctx.Config.Phases[gamePhase].TrainLimit

	if totalTrains >= trainLimit {
		return passAction()
	}

	// Buy cheapest available train that the company can afford.
	for i, tr := range ctx.Config.Trains {
		avail := bankState[engine.BankTrainsBase+i]
		if avail <= 0 {
			continue
		}
		cost := float64(tr.Price)
		if treasury < cost {
			continue
		}
		action := make([]float64, engine.ActionStateWidth)
		action[engine.ActionType] = engine.ActionBuyTrain
		action[engine.ActionArg0] = float64(i)
		action[engine.ActionArg0+1] = cost
		return action
	}

	return passAction()
}

// cheapestAvailableTrainCost returns the cost of the cheapest train in the depot.
func (h *HeuristicAgent) cheapestAvailableTrainCost(ctx *engine.GameContext) int {
	bankState := ctx.StateHistories[ctx.Layout.BankPartition].Values.RawRowView(0)
	for i, tr := range ctx.Config.Trains {
		if bankState[engine.BankTrainsBase+i] > 0 {
			return tr.Price
		}
	}
	return 0
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
