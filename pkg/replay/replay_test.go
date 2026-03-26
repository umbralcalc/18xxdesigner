package replay

import (
	"testing"

	"github.com/umbralcalc/stochadex/pkg/simulator"
	"github.com/umbralcalc/ttdesigner/pkg/engine"
)

func TestReplayTranscript247490(t *testing.T) {
	events, err := ParseTranscript("../../dat/transcript_247490.log")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	playerNames := ExtractPlayerNames(events)
	t.Logf("Players: %v", playerNames)

	if len(playerNames) != 2 {
		t.Fatalf("expected 2 players, got %d", len(playerNames))
	}

	config := engine.NewGameBuilder(2, nil).Config
	agent := NewReplayAgent(events, config, playerNames)

	// Skip private auction — our engine has a simplified version.
	agent.SkipToStockRound()
	t.Logf("Skipped to SR, cursor at event %d", agent.Cursor())

	builder := engine.NewGameBuilder(2, agent)
	settings, implementations := builder.Build()
	layout := builder.Layout()

	// Run with a generous step limit.
	implementations.TerminationCondition = &engine.OrTerminationCondition{
		Conditions: []simulator.TerminationCondition{
			&engine.BankBrokenTerminationCondition{BankPartitionIndex: layout.BankPartition},
			&simulator.NumberOfStepsTerminationCondition{MaxNumberOfSteps: 5000},
		},
	}

	coordinator := simulator.NewPartitionCoordinator(settings, implementations)
	coordinator.Run()

	steps := coordinator.Shared.TimestepsHistory.CurrentStepNumber
	bankState := coordinator.Shared.StateHistories[layout.BankPartition].Values.RawRowView(0)

	t.Logf("Game ended after %d steps, bank cash: %.0f", steps, bankState[engine.BankCash])
	t.Logf("Transcript events consumed: %d/%d", agent.Cursor(), len(events))
	t.Logf("Remaining events: %d", agent.RemainingEvents())

	// Log all errors from the replay.
	errorCount := 0
	for _, step := range agent.Log {
		if step.Error != "" {
			errorCount++
			if errorCount <= 50 {
				evStr := ""
				if step.Event != nil {
					evStr = step.Event.Raw
				}
				t.Logf("MISMATCH step %d: %s (event: %s)", step.Step, step.Error, evStr)
			}
		}
	}
	t.Logf("Total steps: %d, errors: %d", len(agent.Log), errorCount)

	// Log final state.
	for i, c := range config.Companies {
		cs := coordinator.Shared.StateHistories[layout.CompanyPartitions[i]].Values.RawRowView(0)
		t.Logf("Company %s: floated=%.0f treasury=%.0f trains=[%.0f,%.0f,%.0f,%.0f,%.0f,%.0f] par=%.0f",
			c.Sym, cs[engine.CompFloated], cs[engine.CompTreasury],
			cs[engine.CompTrainsBase], cs[engine.CompTrainsBase+1],
			cs[engine.CompTrainsBase+2], cs[engine.CompTrainsBase+3],
			cs[engine.CompTrainsBase+4], cs[engine.CompTrainsBase+5],
			cs[engine.CompParPrice])
	}

	for i, name := range playerNames {
		ps := coordinator.Shared.StateHistories[layout.PlayerPartitions[i]].Values.RawRowView(0)
		t.Logf("Player %s: cash=%.0f certs=%.0f", name, ps[engine.PlayerCash], ps[engine.PlayerCertCount])
	}

	// The replay should consume most events. Log coverage.
	consumed := float64(agent.Cursor()) / float64(len(events)) * 100
	t.Logf("Event coverage: %.1f%%", consumed)
}
