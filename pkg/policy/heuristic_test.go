package policy

import (
	"testing"

	"github.com/umbralcalc/stochadex/pkg/simulator"
	"github.com/umbralcalc/ttdesigner/pkg/engine"
)

func TestHeuristicAgent(t *testing.T) {
	t.Run("runs_50_steps_without_panic", func(t *testing.T) {
		builder := engine.NewGameBuilder(4, &HeuristicAgent{})
		settings, implementations := builder.Build()
		implementations.TerminationCondition = &simulator.NumberOfStepsTerminationCondition{
			MaxNumberOfSteps: 50,
		}

		coordinator := simulator.NewPartitionCoordinator(settings, implementations)
		coordinator.Run()

		// Verify the simulation ran and the turn state is valid.
		turnState := coordinator.Shared.StateHistories[0].Values.RawRowView(0)
		roundType := turnState[engine.TurnRoundType]
		if roundType < 0 || roundType > 2 {
			t.Errorf("invalid round type: %v", roundType)
		}
	})

	t.Run("harness_4_player", func(t *testing.T) {
		builder := engine.NewGameBuilder(4, &HeuristicAgent{})
		settings, implementations := builder.Build()
		implementations.OutputCondition = &simulator.EveryStepOutputCondition{}
		implementations.TerminationCondition = &simulator.NumberOfStepsTerminationCondition{
			MaxNumberOfSteps: 50,
		}

		if err := simulator.RunWithHarnesses(settings, implementations); err != nil {
			t.Errorf("harness failed: %v", err)
		}
	})

	t.Run("pars_a_company_in_sr", func(t *testing.T) {
		builder := engine.NewGameBuilder(4, &HeuristicAgent{})
		settings, implementations := builder.Build()
		implementations.TerminationCondition = &simulator.NumberOfStepsTerminationCondition{
			MaxNumberOfSteps: 30,
		}

		coordinator := simulator.NewPartitionCoordinator(settings, implementations)
		coordinator.Run()

		layout := builder.Layout()

		// After 30 steps with heuristic agent, at least one company should be parred.
		// Check market partition: at least one company should have row >= 0.
		mktState := coordinator.Shared.StateHistories[layout.MarketPartition].Values.RawRowView(0)
		parred := false
		for i := 0; i < len(builder.Config.Companies); i++ {
			if mktState[engine.MarketRowIdx(i)] >= 0 {
				parred = true
				break
			}
		}
		if !parred {
			t.Error("expected at least one company to be parred after 30 steps")
		}
	})
}
