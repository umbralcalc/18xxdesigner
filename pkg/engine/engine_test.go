package engine

import (
	"testing"

	"github.com/umbralcalc/stochadex/pkg/simulator"
	"github.com/umbralcalc/ttdesigner/pkg/gamedata"
)

func TestBuildCompiles(t *testing.T) {
	t.Run("build_4_player_game", func(t *testing.T) {
		builder := NewGameBuilder(4, &PassAgent{})
		settings, implementations := builder.Build()

		if settings == nil {
			t.Fatal("settings is nil")
		}
		if implementations == nil {
			t.Fatal("implementations is nil")
		}

		// Expected partitions: turn + action + bank + market + map + 7 companies + 4 players = 16
		expected := 2 + 1 + 1 + 1 + 7 + 4
		if len(settings.Iterations) != expected {
			t.Errorf("expected %d partitions, got %d", expected, len(settings.Iterations))
		}
	})

	t.Run("build_2_player_game", func(t *testing.T) {
		builder := NewGameBuilder(2, &PassAgent{})
		settings, _ := builder.Build()

		expected := 2 + 1 + 1 + 1 + 7 + 2
		if len(settings.Iterations) != expected {
			t.Errorf("expected %d partitions, got %d", expected, len(settings.Iterations))
		}
	})
}

func TestRunWithPassAgent(t *testing.T) {
	t.Run("10_steps_all_pass", func(t *testing.T) {
		builder := NewGameBuilder(4, &PassAgent{})
		settings, implementations := builder.Build()

		// Override termination to 10 steps for fast test.
		implementations.TerminationCondition = &simulator.NumberOfStepsTerminationCondition{
			MaxNumberOfSteps: 10,
		}

		coordinator := simulator.NewPartitionCoordinator(settings, implementations)
		coordinator.Run()

		// After 10 all-pass steps with 4 players, the turn controller should advance.
		turnState := coordinator.Shared.StateHistories[0].Values.RawRowView(0)

		// Verify the simulation ran without panicking.
		roundType := turnState[TurnRoundType]
		if roundType < 0 || roundType > 2 {
			t.Errorf("invalid round type: %v", roundType)
		}
	})
}

func TestInitStates(t *testing.T) {
	cfg := gamedata.Default1889Config()

	t.Run("bank_init", func(t *testing.T) {
		state := InitBankState(cfg)
		if state[BankCash] != 7000 {
			t.Errorf("expected bank cash 7000, got %v", state[BankCash])
		}
		// 6 type-2 trains available.
		if state[BankTrainsBase] != 6 {
			t.Errorf("expected 6 type-2 trains, got %v", state[BankTrainsBase])
		}
	})

	t.Run("player_init", func(t *testing.T) {
		state := InitPlayerState(cfg, 4)
		if state[PlayerCash] != 420 {
			t.Errorf("expected player cash 420, got %v", state[PlayerCash])
		}
	})

	t.Run("company_init", func(t *testing.T) {
		state := InitCompanyState()
		if state[CompSharesIPO] != 10 {
			t.Errorf("expected 10 IPO shares, got %v", state[CompSharesIPO])
		}
		if state[CompFloated] != 0 {
			t.Errorf("expected not floated, got %v", state[CompFloated])
		}
	})

	t.Run("market_init", func(t *testing.T) {
		state := InitMarketState(cfg)
		// All companies should start at (-1, -1).
		for i := 0; i < len(cfg.Companies); i++ {
			if state[MarketRowIdx(i)] != -1 {
				t.Errorf("company %d: expected market row -1, got %v", i, state[MarketRowIdx(i)])
			}
		}
	})

	t.Run("map_init", func(t *testing.T) {
		hexes := gamedata.Default1889Map()
		state := InitMapState(hexes)
		// Most hexes should have tile_id = -1 (empty).
		emptyCount := 0
		for i := range hexes {
			if state[MapTileIdx(i)] == -1 {
				emptyCount++
			}
		}
		if emptyCount == 0 {
			t.Error("expected some empty hexes in initial map state")
		}
	})
}

func TestRunWithHarnesses(t *testing.T) {
	t.Run("all_iterations_pass_harness_4_player", func(t *testing.T) {
		builder := NewGameBuilder(4, &PassAgent{})

		// Override to EveryStepOutputCondition so the harness statefulness
		// check has data to compare between runs.
		settings, implementations := builder.Build()
		implementations.OutputCondition = &simulator.EveryStepOutputCondition{}
		implementations.TerminationCondition = &simulator.NumberOfStepsTerminationCondition{
			MaxNumberOfSteps: 20,
		}

		if err := simulator.RunWithHarnesses(settings, implementations); err != nil {
			t.Errorf("harness failed: %v", err)
		}
	})

	t.Run("all_iterations_pass_harness_2_player", func(t *testing.T) {
		builder := NewGameBuilder(2, &PassAgent{})
		settings, implementations := builder.Build()
		implementations.OutputCondition = &simulator.EveryStepOutputCondition{}
		implementations.TerminationCondition = &simulator.NumberOfStepsTerminationCondition{
			MaxNumberOfSteps: 20,
		}

		if err := simulator.RunWithHarnesses(settings, implementations); err != nil {
			t.Errorf("harness failed: %v", err)
		}
	})
}

func TestTurnFSM(t *testing.T) {
	t.Run("private_auction_to_sr", func(t *testing.T) {
		turn := &TurnControllerIteration{
			NumPlayers:   4,
			NumCompanies: 7,
			ORsPerPhase:  []int{1, 2, 2, 3, 3, 3},
		}

		state := make([]float64, TurnStateWidth)
		state[TurnRoundType] = RoundPrivateAuction
		state[TurnActiveType] = ActivePlayer
		state[TurnActiveID] = 0

		// Simulate 4 players passing through auction.
		for i := 0; i < 4; i++ {
			turn.advancePrivateAuction(state, ActionPass)
		}

		if state[TurnRoundType] != RoundStockRound {
			t.Errorf("expected SR after auction, got round type %v", state[TurnRoundType])
		}
		if state[TurnActiveType] != ActivePlayer {
			t.Errorf("expected active player in SR, got type %v", state[TurnActiveType])
		}
	})

	t.Run("sr_to_or", func(t *testing.T) {
		turn := &TurnControllerIteration{
			NumPlayers:   4,
			NumCompanies: 7,
			ORsPerPhase:  []int{1, 2, 2, 3, 3, 3},
		}

		state := make([]float64, TurnStateWidth)
		state[TurnRoundType] = RoundStockRound
		state[TurnActiveType] = ActivePlayer
		state[TurnActiveID] = 0
		state[TurnActionStep] = 0

		// 4 consecutive passes should trigger OR.
		for i := 0; i < 4; i++ {
			turn.advanceStockRound(state, ActionPass)
		}

		if state[TurnRoundType] != RoundOperatingRound {
			t.Errorf("expected OR after 4 passes, got round type %v", state[TurnRoundType])
		}
		if state[TurnActiveType] != ActiveCompany {
			t.Errorf("expected active company in OR, got type %v", state[TurnActiveType])
		}
		if state[TurnORNumber] != 1 {
			t.Errorf("expected OR number 1, got %v", state[TurnORNumber])
		}
	})

	t.Run("or_substeps", func(t *testing.T) {
		turn := &TurnControllerIteration{
			NumPlayers:   4,
			NumCompanies: 7,
			ORsPerPhase:  []int{1, 2, 2, 3, 3, 3},
		}

		state := make([]float64, TurnStateWidth)
		state[TurnRoundType] = RoundOperatingRound
		state[TurnActiveType] = ActiveCompany
		state[TurnActiveID] = 0
		state[TurnORNumber] = 1
		state[TurnORsThisSet] = 1
		state[TurnActionStep] = ORStepTileLay

		// First company: 4 sub-steps (tile→token→routes→train).
		turn.advanceOperatingRound(state, ActionPass, 0)
		if state[TurnActionStep] != ORStepToken {
			t.Errorf("expected token step, got %v", state[TurnActionStep])
		}
		turn.advanceOperatingRound(state, ActionPass, 0)
		if state[TurnActionStep] != ORStepRoutes {
			t.Errorf("expected routes step, got %v", state[TurnActionStep])
		}
		turn.advanceOperatingRound(state, ActionPass, 0)
		if state[TurnActionStep] != ORStepBuyTrain {
			t.Errorf("expected buy train step, got %v", state[TurnActionStep])
		}
		turn.advanceOperatingRound(state, ActionPass, 0)
		// After train step → done → advance to next company at tile step.
		if state[TurnActiveID] != 1 {
			t.Errorf("expected company 1, got %v", state[TurnActiveID])
		}
		if state[TurnActionStep] != ORStepTileLay {
			t.Errorf("expected tile lay step for next company, got %v", state[TurnActionStep])
		}
	})

	t.Run("or_to_sr", func(t *testing.T) {
		turn := &TurnControllerIteration{
			NumPlayers:   4,
			NumCompanies: 7,
			ORsPerPhase:  []int{1, 2, 2, 3, 3, 3},
		}

		state := make([]float64, TurnStateWidth)
		state[TurnRoundType] = RoundOperatingRound
		state[TurnActiveType] = ActiveCompany
		state[TurnActiveID] = 0
		state[TurnORNumber] = 1
		state[TurnORsThisSet] = 1
		state[TurnActionStep] = ORStepTileLay

		// 7 companies × 4 sub-steps = 28 steps to complete one OR.
		for i := 0; i < 7*4; i++ {
			turn.advanceOperatingRound(state, ActionPass, 0)
		}

		if state[TurnRoundType] != RoundStockRound {
			t.Errorf("expected SR after all companies complete in single OR, got %v", state[TurnRoundType])
		}
	})

	t.Run("multiple_ors", func(t *testing.T) {
		turn := &TurnControllerIteration{
			NumPlayers:   4,
			NumCompanies: 7,
			ORsPerPhase:  []int{1, 2, 2, 3, 3, 3},
		}

		state := make([]float64, TurnStateWidth)
		state[TurnRoundType] = RoundOperatingRound
		state[TurnActiveType] = ActiveCompany
		state[TurnActiveID] = 0
		state[TurnORNumber] = 1
		state[TurnORsThisSet] = 2
		state[TurnActionStep] = ORStepTileLay

		// First OR: 7 companies × 4 sub-steps.
		for i := 0; i < 7*4; i++ {
			turn.advanceOperatingRound(state, ActionPass, 1)
		}

		if state[TurnRoundType] != RoundOperatingRound {
			t.Errorf("expected still in OR, got %v", state[TurnRoundType])
		}
		if state[TurnORNumber] != 2 {
			t.Errorf("expected OR number 2, got %v", state[TurnORNumber])
		}

		// Second OR: 7 companies × 4 sub-steps.
		for i := 0; i < 7*4; i++ {
			turn.advanceOperatingRound(state, ActionPass, 1)
		}

		if state[TurnRoundType] != RoundStockRound {
			t.Errorf("expected SR after 2 ORs complete, got %v", state[TurnRoundType])
		}
	})
}
