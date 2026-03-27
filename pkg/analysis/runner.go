package analysis

import (
	"github.com/umbralcalc/18xxdesigner/pkg/engine"
	"github.com/umbralcalc/18xxdesigner/pkg/gamedata"
	"github.com/umbralcalc/18xxdesigner/pkg/policy"
	"github.com/umbralcalc/stochadex/pkg/simulator"
)

// RunResult captures the outcome of a single simulation.
type RunResult struct {
	Seed            int64
	Steps           int
	BankCash        float64
	PortfolioValues []float64 // per player
	CompanyFloated  []bool    // per company
	CompanySurvived []bool    // floated and has trains at end
	CompanyRevenue  []float64 // last revenue per company
	HexTileIDs      []int     // tile ID placed on each hex (-1 = empty)
	WinnerIndex     int       // player with highest portfolio
}

// AgentType selects which gameplay agent to use for simulations.
type AgentType int

const (
	AgentHeuristic AgentType = iota
	AgentMCTS
)

// RunConfig configures a batch of simulations.
type RunConfig struct {
	NumGames        int
	NumPlayers      int
	MaxSteps        int
	Agent           AgentType
	MCTSPlayouts    int
	MCTSMaxPlayout  int
	MCTSPlayerIndex int
}

// DefaultRunConfig returns sensible defaults for a batch run.
func DefaultRunConfig() RunConfig {
	return RunConfig{
		NumGames:        100,
		NumPlayers:      4,
		MaxSteps:        5000,
		Agent:           AgentHeuristic,
		MCTSPlayouts:    5,
		MCTSMaxPlayout:  1000,
		MCTSPlayerIndex: 0,
	}
}

// RunBatch runs NumGames simulations and returns all results.
func RunBatch(cfg RunConfig) []RunResult {
	results := make([]RunResult, cfg.NumGames)
	for i := 0; i < cfg.NumGames; i++ {
		results[i] = runSingle(cfg, int64(i+1), nil)
	}
	return results
}

// RunBatchWithConfig runs simulations using a custom GameConfig (for variant comparison).
func RunBatchWithConfig(cfg RunConfig, gameCfg *gamedata.GameConfig) []RunResult {
	results := make([]RunResult, cfg.NumGames)
	for i := 0; i < cfg.NumGames; i++ {
		results[i] = runSingle(cfg, int64(i+1), gameCfg)
	}
	return results
}

func runSingle(cfg RunConfig, seed int64, gameCfgOverride *gamedata.GameConfig) RunResult {
	var agent engine.Agent
	switch cfg.Agent {
	case AgentMCTS:
		mcts := policy.NewMCTSAgent(cfg.MCTSPlayerIndex, cfg.MCTSPlayouts)
		mcts.MaxPlayoutSteps = cfg.MCTSMaxPlayout
		agent = mcts
	default:
		agent = &policy.HeuristicAgent{}
	}

	builder := engine.NewGameBuilder(cfg.NumPlayers, agent)
	builder.Seed = seed
	if gameCfgOverride != nil {
		builder.Config = gameCfgOverride
	}
	settings, impls := builder.Build()
	layout := builder.Layout()

	impls.TerminationCondition = &engine.OrTerminationCondition{
		Conditions: []simulator.TerminationCondition{
			&engine.BankBrokenTerminationCondition{
				BankPartitionIndex: layout.BankPartition,
			},
			&simulator.NumberOfStepsTerminationCondition{
				MaxNumberOfSteps: cfg.MaxSteps,
			},
		},
	}

	coordinator := simulator.NewPartitionCoordinator(settings, impls)
	coordinator.Run()

	sh := coordinator.Shared.StateHistories
	numCompanies := len(builder.Config.Companies)
	hexes := builder.Hexes

	return extractResult(sh, layout, builder.Market, builder.Config,
		cfg.NumPlayers, numCompanies, hexes, seed,
		int(coordinator.Shared.TimestepsHistory.CurrentStepNumber))
}

func extractResult(
	sh []*simulator.StateHistory,
	layout *engine.PartitionLayout,
	grid *gamedata.MarketGrid,
	cfg *gamedata.GameConfig,
	numPlayers, numCompanies int,
	hexes []gamedata.HexDef,
	seed int64,
	steps int,
) RunResult {
	bankState := sh[layout.BankPartition].Values.RawRowView(0)
	mapState := sh[layout.MapPartition].Values.RawRowView(0)

	r := RunResult{
		Seed:            seed,
		Steps:           steps,
		BankCash:        bankState[engine.BankCash],
		PortfolioValues: make([]float64, numPlayers),
		CompanyFloated:  make([]bool, numCompanies),
		CompanySurvived: make([]bool, numCompanies),
		CompanyRevenue:  make([]float64, numCompanies),
		HexTileIDs:      make([]int, len(hexes)),
	}

	for p := 0; p < numPlayers; p++ {
		r.PortfolioValues[p] = policy.PortfolioValue(
			sh, layout, p, grid, numCompanies)
	}

	for c := 0; c < numCompanies; c++ {
		cs := sh[layout.CompanyPartitions[c]].Values.RawRowView(0)
		r.CompanyFloated[c] = cs[engine.CompFloated] != 0
		r.CompanyRevenue[c] = cs[engine.CompLastRevenue]

		totalTrains := 0
		for t := range cfg.Trains {
			totalTrains += int(cs[engine.CompTrainsBase+t])
		}
		r.CompanySurvived[c] = r.CompanyFloated[c] && totalTrains > 0
	}

	for i := range hexes {
		r.HexTileIDs[i] = int(mapState[engine.MapTileIdx(i)])
	}

	// Winner.
	bestVal := -1.0
	for p, v := range r.PortfolioValues {
		if v > bestVal {
			bestVal = v
			r.WinnerIndex = p
		}
	}

	return r
}
