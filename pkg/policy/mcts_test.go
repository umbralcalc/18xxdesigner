package policy

import (
	"math"
	"testing"

	"github.com/umbralcalc/stochadex/pkg/simulator"
	"github.com/umbralcalc/18xxdesigner/pkg/engine"
)

func TestMCTSAgent(t *testing.T) {
	t.Run("chooses_valid_action", func(t *testing.T) {
		// Run a game for 30 steps to reach SR, then use MCTS for one decision.
		builder := engine.NewGameBuilder(4, &HeuristicAgent{})
		settings, impls := builder.Build()
		impls.TerminationCondition = &simulator.NumberOfStepsTerminationCondition{
			MaxNumberOfSteps: 30,
		}
		coordinator := simulator.NewPartitionCoordinator(settings, impls)
		coordinator.Run()

		layout := builder.Layout()
		turnState := coordinator.Shared.StateHistories[layout.TurnPartition].Values.RawRowView(0)
		ctx := &engine.GameContext{
			TurnState:        turnState,
			StateHistories:   coordinator.Shared.StateHistories,
			TimestepsHistory: coordinator.Shared.TimestepsHistory,
			Layout:           layout,
			Config:           builder.Config,
			MarketGrid:       builder.Market,
			NumPlayers:       4,
		}

		mcts := NewMCTSAgent(0, 5)
		mcts.MaxPlayoutSteps = 1000

		action := mcts.ChooseAction(ctx)
		if len(action) != engine.ActionStateWidth {
			t.Errorf("expected action width %d, got %d",
				engine.ActionStateWidth, len(action))
		}
	})

	t.Run("full_game_completes", func(t *testing.T) {
		agent := NewMCTSAgent(0, 3)
		agent.MaxPlayoutSteps = 500

		builder := engine.NewGameBuilder(4, agent)
		settings, impls := builder.Build()
		layout := builder.Layout()

		impls.TerminationCondition = &engine.OrTerminationCondition{
			Conditions: []simulator.TerminationCondition{
				&engine.BankBrokenTerminationCondition{
					BankPartitionIndex: layout.BankPartition,
				},
				&simulator.NumberOfStepsTerminationCondition{
					MaxNumberOfSteps: 5000,
				},
			},
		}

		coordinator := simulator.NewPartitionCoordinator(settings, impls)
		coordinator.Run()

		steps := coordinator.Shared.TimestepsHistory.CurrentStepNumber
		t.Logf("game ended after %d steps", steps)
		if steps >= 5000 {
			t.Error("game did not terminate within 5000 steps")
		}
	})
}

func TestMCTSPlayoutPartitions(t *testing.T) {
	t.Run("runs_without_nan", func(t *testing.T) {
		// Get a mid-game snapshot by running heuristic for 30 steps.
		builder := engine.NewGameBuilder(4, &HeuristicAgent{})
		settings, impls := builder.Build()
		impls.TerminationCondition = &simulator.NumberOfStepsTerminationCondition{
			MaxNumberOfSteps: 30,
		}
		coordinator := simulator.NewPartitionCoordinator(settings, impls)
		coordinator.Run()

		snapshot := make([][]float64, len(coordinator.Shared.StateHistories))
		for i, sh := range coordinator.Shared.StateHistories {
			row := sh.Values.RawRowView(0)
			cp := make([]float64, len(row))
			copy(cp, row)
			snapshot[i] = cp
		}

		candidates := [][]float64{passAction(), passAction()}
		partitions := NewMCTSPlayoutPartitions(AppliedMCTSOptimisation{
			Selector: MCTSSelector{
				CandidateActions: candidates,
				ExplorationC:     1.414,
			},
			Playout: MCTSPlayout{
				Snapshot:        snapshot,
				NumPlayers:      4,
				MaxPlayoutSteps: 1000,
				PlayerIndex:     0,
			},
		})

		gen := simulator.NewConfigGenerator()
		gen.SetSimulation(&simulator.SimulationConfig{
			OutputCondition: &simulator.NilOutputCondition{},
			OutputFunction:  &simulator.NilOutputFunction{},
			TerminationCondition: &simulator.NumberOfStepsTerminationCondition{
				MaxNumberOfSteps: 3,
			},
			TimestepFunction: &simulator.ConstantTimestepFunction{Stepsize: 1.0},
			InitTimeValue:    0.0,
		})
		for _, p := range partitions {
			gen.SetPartition(p)
		}

		outerSettings, outerImpls := gen.GenerateConfigs()
		outerCoordinator := simulator.NewPartitionCoordinator(
			outerSettings, outerImpls)
		outerCoordinator.Run()

		if err := simulator.RunWithHarnesses(outerSettings, outerImpls); err != nil {
			t.Fatalf("MCTS playout partitions harness: %v", err)
		}

		// Verify no NaN in statistics partition (index 2).
		statsState := outerCoordinator.Shared.StateHistories[2].Values.RawRowView(0)
		for i, v := range statsState {
			if math.IsNaN(v) {
				t.Errorf("NaN at statistics state index %d", i)
			}
		}

		// Verify some playouts were recorded.
		totalVisits := statsState[0] + statsState[2]
		if totalVisits < 1 {
			t.Error("expected at least one playout visit")
		}
		t.Logf("visits: action0=%.0f action1=%.0f", statsState[0], statsState[2])
	})
}

func TestMCTSBeatsHeuristic(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping MCTS benchmark in short mode")
	}

	numGames := 10
	mctsPlayer := 0
	numPlayers := 4
	numCompanies := 7

	mctsWins := 0
	mctsTotalRank := 0

	for g := 0; g < numGames; g++ {
		agent := NewMCTSAgent(mctsPlayer, 5)
		agent.MaxPlayoutSteps = 1000

		builder := engine.NewGameBuilder(numPlayers, agent)
		builder.Seed = int64(g + 1) // each game gets a different random private auction
		settings, impls := builder.Build()
		layout := builder.Layout()

		impls.TerminationCondition = &engine.OrTerminationCondition{
			Conditions: []simulator.TerminationCondition{
				&engine.BankBrokenTerminationCondition{
					BankPartitionIndex: layout.BankPartition,
				},
				&simulator.NumberOfStepsTerminationCondition{
					MaxNumberOfSteps: 5000,
				},
			},
		}

		coordinator := simulator.NewPartitionCoordinator(settings, impls)
		coordinator.Run()

		// Compute portfolio values for all players.
		values := make([]float64, numPlayers)
		for p := 0; p < numPlayers; p++ {
			values[p] = PortfolioValue(
				coordinator.Shared.StateHistories,
				layout, p, builder.Market, numCompanies)
		}

		// Determine rank of MCTS player (0 = best).
		mctsVal := values[mctsPlayer]
		rank := 0
		for p := 0; p < numPlayers; p++ {
			if p != mctsPlayer && values[p] > mctsVal {
				rank++
			}
		}
		mctsTotalRank += rank
		if rank == 0 {
			mctsWins++
		}

		steps := coordinator.Shared.TimestepsHistory.CurrentStepNumber
		t.Logf("game %d: steps=%d mcts=%.0f values=%v rank=%d",
			g, steps, mctsVal, values, rank)
	}

	winRate := float64(mctsWins) / float64(numGames)
	avgRank := float64(mctsTotalRank) / float64(numGames)
	t.Logf("MCTS wins: %d/%d (%.0f%%), avg rank: %.2f",
		mctsWins, numGames, winRate*100, avgRank)

	// MCTS should win more than chance (25% for 4 players).
	if winRate < 0.25 {
		t.Errorf("MCTS win rate %.0f%% is not above chance (25%%)", winRate*100)
	}
}

func TestEnumerateLegalMoves(t *testing.T) {
	t.Run("sr_has_multiple_options", func(t *testing.T) {
		builder := engine.NewGameBuilder(4, &HeuristicAgent{})
		settings, impls := builder.Build()
		impls.TerminationCondition = &simulator.NumberOfStepsTerminationCondition{
			MaxNumberOfSteps: 20,
		}
		coordinator := simulator.NewPartitionCoordinator(settings, impls)
		coordinator.Run()

		layout := builder.Layout()
		turnState := coordinator.Shared.StateHistories[layout.TurnPartition].Values.RawRowView(0)

		// Skip if not in SR.
		if turnState[engine.TurnRoundType] != engine.RoundStockRound {
			t.Skip("not in stock round after 20 steps")
		}

		ctx := &engine.GameContext{
			TurnState:        turnState,
			StateHistories:   coordinator.Shared.StateHistories,
			TimestepsHistory: coordinator.Shared.TimestepsHistory,
			Layout:           layout,
			Config:           builder.Config,
			MarketGrid:       builder.Market,
			NumPlayers:       4,
		}

		moves := enumerateLegalMoves(ctx)
		if len(moves) == 0 {
			t.Error("expected at least one legal move")
		}
		t.Logf("found %d legal moves in SR", len(moves))
	})
}
