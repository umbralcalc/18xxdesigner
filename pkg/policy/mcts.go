package policy

import (
	"math"

	"github.com/umbralcalc/18xxdesigner/pkg/engine"
	"github.com/umbralcalc/18xxdesigner/pkg/gamedata"
	"github.com/umbralcalc/stochadex/pkg/general"
	"github.com/umbralcalc/stochadex/pkg/simulator"
)

// --- Configuration types following the stochadex analysis/optimisation pattern ---

// MCTSSelector configures the action selection partition.
type MCTSSelector struct {
	CandidateActions [][]float64
	ExplorationC     float64
}

// MCTSPlayout configures the embedded playout simulation.
type MCTSPlayout struct {
	Snapshot        [][]float64 // per-partition state snapshot
	NumPlayers      int
	MaxPlayoutSteps int
	PlayerIndex     int // which player to compute portfolio value for
}

// AppliedMCTSOptimisation is the base configuration for an MCTS search
// using embedded game simulations for playouts, following the stochadex
// analysis/optimisation pattern.
type AppliedMCTSOptimisation struct {
	Selector MCTSSelector
	Playout  MCTSPlayout
}

// --- Custom iterations ---

// MCTSActionSelectorIteration selects candidate actions via UCB1.
// State: [selected_action_index, action_values...]
type MCTSActionSelectorIteration struct {
	CandidateActions [][]float64
	ExplorationC     float64
	statsPartition   int
}

func (m *MCTSActionSelectorIteration) Configure(
	partitionIndex int,
	settings *simulator.Settings,
) {
	m.statsPartition = int(
		settings.Iterations[partitionIndex].Params.GetIndex(
			"statistics_partition", 0))
}

func (m *MCTSActionSelectorIteration) Iterate(
	params *simulator.Params,
	partitionIndex int,
	stateHistories []*simulator.StateHistory,
	timestepsHistory *simulator.CumulativeTimestepsHistory,
) []float64 {
	stats := stateHistories[m.statsPartition].Values.RawRowView(0)
	numActions := len(m.CandidateActions)

	totalVisits := 0.0
	for i := 0; i < numActions; i++ {
		totalVisits += stats[i*2]
	}

	bestIdx := 0
	bestScore := math.Inf(-1)
	for i := 0; i < numActions; i++ {
		visits := stats[i*2]
		if visits == 0 {
			bestIdx = i
			break
		}
		avgScore := stats[i*2+1] / visits
		exploration := m.ExplorationC * math.Sqrt(
			math.Log(totalVisits)/visits)
		score := avgScore + exploration
		if score > bestScore {
			bestScore = score
			bestIdx = i
		}
	}

	output := make([]float64, 1+engine.ActionStateWidth)
	output[0] = float64(bestIdx)
	copy(output[1:], m.CandidateActions[bestIdx])
	return output
}

// MCTSStatisticsIteration accumulates per-action visit counts and scores.
// State: [visits_0, total_score_0, visits_1, total_score_1, ...]
type MCTSStatisticsIteration struct {
	NumActions   int
	PlayerOffset int // offset of player state in playout output
	MarketOffset int // offset of market state in playout output
	NumCompanies int
	MarketGrid   *gamedata.MarketGrid
}

func (m *MCTSStatisticsIteration) Configure(
	partitionIndex int,
	settings *simulator.Settings,
) {}

func (m *MCTSStatisticsIteration) Iterate(
	params *simulator.Params,
	partitionIndex int,
	stateHistories []*simulator.StateHistory,
	timestepsHistory *simulator.CumulativeTimestepsHistory,
) []float64 {
	prev := stateHistories[partitionIndex].Values.RawRowView(0)
	state := make([]float64, len(prev))
	copy(state, prev)

	selectorValues := params.Get("action_selector_values")
	actionIndex := int(selectorValues[0])

	playoutValues := params.Get("playout_values")

	// Compute portfolio value from the playout's concatenated final state.
	cash := playoutValues[m.PlayerOffset+engine.PlayerCash]
	total := cash
	for c := 0; c < m.NumCompanies; c++ {
		shares := playoutValues[m.PlayerOffset+engine.PlayerShareIdx(c)]
		if shares <= 0 {
			continue
		}
		row := int(playoutValues[m.MarketOffset+engine.MarketRowIdx(c)])
		col := int(playoutValues[m.MarketOffset+engine.MarketColIdx(c)])
		if row < 0 {
			continue
		}
		price := float64(m.MarketGrid.Price(row, col))
		total += shares * price
	}

	state[actionIndex*2] += 1
	state[actionIndex*2+1] += total
	return state
}

// PlayoutActionIteration forces the first action from params, then
// delegates to the heuristic agent for subsequent steps.
type PlayoutActionIteration struct {
	TurnPartition int
	Layout        *engine.PartitionLayout
	Config        *gamedata.GameConfig
	MarketGrid    *gamedata.MarketGrid
	NumPlayers    int
	firstStep     bool
}

func (p *PlayoutActionIteration) Configure(
	partitionIndex int,
	settings *simulator.Settings,
) {
	p.firstStep = true
}

func (p *PlayoutActionIteration) Iterate(
	params *simulator.Params,
	partitionIndex int,
	stateHistories []*simulator.StateHistory,
	timestepsHistory *simulator.CumulativeTimestepsHistory,
) []float64 {
	if p.firstStep {
		p.firstStep = false
		if forced, ok := params.GetOk("forced_action"); ok &&
			len(forced) > engine.ActionStateWidth {
			// forced_action = [action_index, action_values...]
			result := make([]float64, engine.ActionStateWidth)
			copy(result, forced[1:1+engine.ActionStateWidth])
			return result
		}
	}

	turnState := stateHistories[p.TurnPartition].Values.RawRowView(0)
	ctx := &engine.GameContext{
		TurnState:        turnState,
		StateHistories:   stateHistories,
		TimestepsHistory: timestepsHistory,
		Layout:           p.Layout,
		Config:           p.Config,
		MarketGrid:       p.MarketGrid,
		NumPlayers:       p.NumPlayers,
	}
	return (&HeuristicAgent{}).ChooseAction(ctx)
}

// --- Builder function ---

// NewMCTSPlayoutPartitions creates partition configs for an MCTS search
// using embedded game simulations, following the stochadex
// macros.NewEvolutionStrategyOptimisationPartitions pattern.
//
// Partitions:
//  1. action_selector — UCB1 selection over candidate actions
//  2. playout_simulation — EmbeddedSimulationRunIteration running a full game
//  3. statistics — accumulates visit counts and total portfolio scores
func NewMCTSPlayoutPartitions(
	applied AppliedMCTSOptimisation,
) []*simulator.PartitionConfig {
	numActions := len(applied.Selector.CandidateActions)
	partitions := make([]*simulator.PartitionConfig, 0, 3)

	// Build the inner game simulation for playouts.
	innerBuilder := engine.NewGameBuilder(
		applied.Playout.NumPlayers, &HeuristicAgent{})
	innerSettings, innerImpls := innerBuilder.Build()
	innerLayout := innerBuilder.Layout()

	// Override init states with snapshot.
	for i, snap := range applied.Playout.Snapshot {
		if i < len(innerSettings.Iterations) {
			innerSettings.Iterations[i].InitStateValues = snap
		}
	}

	// Replace the action iteration with the playout variant that
	// reads forced_action from params on step 1.
	innerImpls.Iterations[innerLayout.ActionPartition] = &PlayoutActionIteration{
		TurnPartition: innerLayout.TurnPartition,
		Layout:        innerLayout,
		Config:        innerBuilder.Config,
		MarketGrid:    innerBuilder.Market,
		NumPlayers:    applied.Playout.NumPlayers,
	}

	// Set playout termination: bank broken or max steps.
	innerImpls.TerminationCondition = &engine.OrTerminationCondition{
		Conditions: []simulator.TerminationCondition{
			&engine.BankBrokenTerminationCondition{
				BankPartitionIndex: innerLayout.BankPartition,
			},
			&simulator.NumberOfStepsTerminationCondition{
				MaxNumberOfSteps: applied.Playout.MaxPlayoutSteps,
			},
		},
	}

	// Compute offsets for extracting portfolio value from
	// the concatenated playout output.
	playerPartIdx := innerLayout.PlayerPartitions[applied.Playout.PlayerIndex]
	playerOffset := 0
	for i := 0; i < playerPartIdx; i++ {
		playerOffset += innerSettings.Iterations[i].StateWidth
	}
	marketOffset := 0
	for i := 0; i < innerLayout.MarketPartition; i++ {
		marketOffset += innerSettings.Iterations[i].StateWidth
	}

	// Concatenated init state for the outer embedded sim partition.
	simInitState := make([]float64, 0)
	for _, iter := range innerSettings.Iterations {
		simInitState = append(simInitState, iter.InitStateValues...)
	}

	// Partition 1: Action selector (UCB1).
	selectorStateWidth := 1 + engine.ActionStateWidth
	partitions = append(partitions, &simulator.PartitionConfig{
		Name: "action_selector",
		Iteration: &MCTSActionSelectorIteration{
			CandidateActions: applied.Selector.CandidateActions,
			ExplorationC:     applied.Selector.ExplorationC,
		},
		Params: simulator.NewParams(map[string][]float64{}),
		ParamsAsPartitions: map[string][]string{
			"statistics_partition": {"statistics"},
		},
		InitStateValues:   make([]float64, selectorStateWidth),
		StateHistoryDepth: 1,
		Seed:              0,
	})

	// Partition 2: Playout simulation (embedded).
	partitions = append(partitions, &simulator.PartitionConfig{
		Name: "playout_simulation",
		Iteration: general.NewEmbeddedSimulationRunIteration(
			innerSettings, innerImpls),
		Params: simulator.NewParams(map[string][]float64{
			"burn_in_steps": {0},
		}),
		ParamsFromUpstream: map[string]simulator.NamedUpstreamConfig{
			"action/forced_action": {Upstream: "action_selector"},
		},
		InitStateValues:   simInitState,
		StateHistoryDepth: 1,
		Seed:              0,
	})

	// Partition 3: Statistics accumulation.
	partitions = append(partitions, &simulator.PartitionConfig{
		Name: "statistics",
		Iteration: &MCTSStatisticsIteration{
			NumActions:   numActions,
			PlayerOffset: playerOffset,
			MarketOffset: marketOffset,
			NumCompanies: len(innerBuilder.Config.Companies),
			MarketGrid:   innerBuilder.Market,
		},
		Params: simulator.NewParams(map[string][]float64{}),
		ParamsFromUpstream: map[string]simulator.NamedUpstreamConfig{
			"action_selector_values": {Upstream: "action_selector"},
			"playout_values":         {Upstream: "playout_simulation"},
		},
		InitStateValues:   make([]float64, numActions*2),
		StateHistoryDepth: 1,
		Seed:              0,
	})

	return partitions
}

// --- MCTSAgent ---

// MCTSAgent implements Agent using Monte Carlo tree search.
// On decisions for its player, it builds embedded playout simulations
// as stochadex partitions and selects the action that maximises
// portfolio value (cash + shares at market price).
type MCTSAgent struct {
	PlayerIndex     int
	NumPlayouts     int
	ExplorationC    float64
	MaxPlayoutSteps int
}

// NewMCTSAgent creates an MCTS agent for the given player.
func NewMCTSAgent(playerIndex, numPlayouts int) *MCTSAgent {
	return &MCTSAgent{
		PlayerIndex:     playerIndex,
		NumPlayouts:     numPlayouts,
		ExplorationC:    1.414,
		MaxPlayoutSteps: 3000,
	}
}

var _ engine.Agent = (*MCTSAgent)(nil)

func (m *MCTSAgent) ChooseAction(ctx *engine.GameContext) []float64 {
	if !m.isOurTurn(ctx) {
		return (&HeuristicAgent{}).ChooseAction(ctx)
	}

	candidates := enumerateLegalMoves(ctx)
	if len(candidates) <= 1 {
		if len(candidates) == 1 {
			return candidates[0]
		}
		return passAction()
	}

	snapshot := snapshotState(ctx)
	partitions := NewMCTSPlayoutPartitions(AppliedMCTSOptimisation{
		Selector: MCTSSelector{
			CandidateActions: candidates,
			ExplorationC:     m.ExplorationC,
		},
		Playout: MCTSPlayout{
			Snapshot:        snapshot,
			NumPlayers:      ctx.NumPlayers,
			MaxPlayoutSteps: m.MaxPlayoutSteps,
			PlayerIndex:     m.PlayerIndex,
		},
	})

	// Run the MCTS search as a stochadex simulation.
	gen := simulator.NewConfigGenerator()
	gen.SetSimulation(&simulator.SimulationConfig{
		OutputCondition: &simulator.NilOutputCondition{},
		OutputFunction:  &simulator.NilOutputFunction{},
		TerminationCondition: &simulator.NumberOfStepsTerminationCondition{
			MaxNumberOfSteps: m.NumPlayouts,
		},
		TimestepFunction: &simulator.ConstantTimestepFunction{Stepsize: 1.0},
		InitTimeValue:    0.0,
	})
	for _, p := range partitions {
		gen.SetPartition(p)
	}

	settings, impls := gen.GenerateConfigs()
	coordinator := simulator.NewPartitionCoordinator(settings, impls)
	coordinator.Run()

	// Read statistics and select the action with highest average score.
	// Statistics is partition index 2 (action_selector=0, playout=1, stats=2).
	statsState := coordinator.Shared.StateHistories[2].Values.RawRowView(0)
	bestIdx := 0
	bestAvg := math.Inf(-1)
	for i := 0; i < len(candidates); i++ {
		visits := statsState[i*2]
		if visits == 0 {
			continue
		}
		avg := statsState[i*2+1] / visits
		if avg > bestAvg {
			bestAvg = avg
			bestIdx = i
		}
	}

	return candidates[bestIdx]
}

func (m *MCTSAgent) isOurTurn(ctx *engine.GameContext) bool {
	roundType := ctx.TurnState[engine.TurnRoundType]
	switch roundType {
	case engine.RoundStockRound:
		return int(ctx.TurnState[engine.TurnActiveID]) == m.PlayerIndex
	case engine.RoundOperatingRound:
		companyIndex := int(ctx.TurnState[engine.TurnActiveID])
		compState := ctx.StateHistories[ctx.Layout.CompanyPartitions[companyIndex]].Values.RawRowView(0)
		return int(compState[engine.CompPresident]) == m.PlayerIndex
	default:
		return false
	}
}

// --- Helpers ---

// PortfolioValue computes a player's total portfolio value:
// cash + sum(shares_held * market_price) for each company.
func PortfolioValue(
	stateHistories []*simulator.StateHistory,
	layout *engine.PartitionLayout,
	playerIndex int,
	marketGrid *gamedata.MarketGrid,
	numCompanies int,
) float64 {
	ps := stateHistories[layout.PlayerPartitions[playerIndex]].Values.RawRowView(0)
	mkt := stateHistories[layout.MarketPartition].Values.RawRowView(0)
	total := ps[engine.PlayerCash]
	for c := 0; c < numCompanies; c++ {
		shares := ps[engine.PlayerShareIdx(c)]
		if shares <= 0 {
			continue
		}
		row := int(mkt[engine.MarketRowIdx(c)])
		col := int(mkt[engine.MarketColIdx(c)])
		if row < 0 {
			continue
		}
		total += shares * float64(marketGrid.Price(row, col))
	}
	return total
}

func snapshotState(ctx *engine.GameContext) [][]float64 {
	snap := make([][]float64, len(ctx.StateHistories))
	for i, sh := range ctx.StateHistories {
		row := sh.Values.RawRowView(0)
		cp := make([]float64, len(row))
		copy(cp, row)
		snap[i] = cp
	}
	return snap
}

func enumerateLegalMoves(ctx *engine.GameContext) [][]float64 {
	roundType := ctx.TurnState[engine.TurnRoundType]
	switch roundType {
	case engine.RoundStockRound:
		return enumerateSRMoves(ctx)
	case engine.RoundOperatingRound:
		return enumerateORMoves(ctx)
	default:
		return [][]float64{passAction()}
	}
}

func enumerateSRMoves(ctx *engine.GameContext) [][]float64 {
	playerIndex := int(ctx.TurnState[engine.TurnActiveID])
	srCtx := engine.ExtractSRContext(
		playerIndex, ctx.StateHistories, ctx.Config,
		ctx.MarketGrid, ctx.NumPlayers, ctx.Layout)
	actions := engine.LegalStockRoundActions(srCtx)
	result := make([][]float64, len(actions))
	for i, a := range actions {
		vals := make([]float64, engine.ActionStateWidth)
		copy(vals, a.Values[:])
		result[i] = vals
	}
	return result
}

func enumerateORMoves(ctx *engine.GameContext) [][]float64 {
	orStep := ctx.TurnState[engine.TurnActionStep]
	companyIndex := int(ctx.TurnState[engine.TurnActiveID])
	compState := ctx.StateHistories[ctx.Layout.CompanyPartitions[companyIndex]].Values.RawRowView(0)
	if compState[engine.CompFloated] == 0 {
		return [][]float64{passAction()}
	}

	switch orStep {
	case engine.ORStepTileLay:
		return enumerateTileMoves(ctx, companyIndex)
	case engine.ORStepToken:
		return enumerateTokenMoves(ctx, companyIndex)
	case engine.ORStepRoutes:
		return enumerateRouteMoves(ctx, companyIndex)
	case engine.ORStepBuyTrain:
		return enumerateTrainMoves(ctx, companyIndex)
	default:
		return [][]float64{passAction()}
	}
}

func enumerateTileMoves(ctx *engine.GameContext, companyIndex int) [][]float64 {
	tileCtx := engine.ExtractTileLayContext(
		companyIndex, ctx.StateHistories, ctx.Config,
		gamedata.Default1889Map(), ctx.Layout)
	actions := engine.LegalTileLayActions(tileCtx)
	result := [][]float64{passAction()}
	for _, a := range actions {
		vals := make([]float64, engine.ActionStateWidth)
		copy(vals, a.Values[:])
		result = append(result, vals)
	}
	return result
}

func enumerateTokenMoves(ctx *engine.GameContext, companyIndex int) [][]float64 {
	tokenCtx := engine.ExtractTokenContext(
		companyIndex, ctx.StateHistories, ctx.Config,
		gamedata.Default1889Map(), ctx.Layout)
	actions := engine.LegalTokenActions(tokenCtx)
	result := [][]float64{passAction()}
	for _, a := range actions {
		vals := make([]float64, engine.ActionStateWidth)
		copy(vals, a.Values[:])
		result = append(result, vals)
	}
	return result
}

func enumerateRouteMoves(ctx *engine.GameContext, companyIndex int) [][]float64 {
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
		return [][]float64{passAction()}
	}

	_, totalRevenue := engine.OptimalRouteAssignment(
		graph, companyIndex, trains, distances, gamePhase)
	if totalRevenue == 0 {
		return [][]float64{passAction()}
	}

	pay := make([]float64, engine.ActionStateWidth)
	pay[engine.ActionType] = engine.ActionPayDividends
	pay[engine.ActionArg0] = float64(companyIndex)
	pay[engine.ActionArg0+1] = float64(totalRevenue)

	withhold := make([]float64, engine.ActionStateWidth)
	withhold[engine.ActionType] = engine.ActionWithhold
	withhold[engine.ActionArg0] = float64(companyIndex)
	withhold[engine.ActionArg0+1] = float64(totalRevenue)

	return [][]float64{pay, withhold}
}

func enumerateTrainMoves(ctx *engine.GameContext, companyIndex int) [][]float64 {
	compState := ctx.StateHistories[ctx.Layout.CompanyPartitions[companyIndex]].Values.RawRowView(0)
	bankState := ctx.StateHistories[ctx.Layout.BankPartition].Values.RawRowView(0)
	treasury := compState[engine.CompTreasury]
	gamePhase := int(bankState[engine.BankTrainPhase])
	trainLimit := ctx.Config.Phases[gamePhase].TrainLimit

	totalTrains := 0
	for i := range ctx.Config.Trains {
		totalTrains += int(compState[engine.CompTrainsBase+i])
	}

	result := [][]float64{passAction()}
	if totalTrains >= trainLimit {
		return result
	}

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
		result = append(result, action)
	}

	return result
}
