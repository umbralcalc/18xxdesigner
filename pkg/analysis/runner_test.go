package analysis

import (
	"math"
	"testing"
)

func TestRunBatch(t *testing.T) {
	t.Run("produces_valid_results", func(t *testing.T) {
		cfg := DefaultRunConfig()
		cfg.NumGames = 3
		cfg.MaxSteps = 2000

		results := RunBatch(cfg)
		if len(results) != 3 {
			t.Fatalf("expected 3 results, got %d", len(results))
		}

		for i, r := range results {
			if r.Steps <= 0 {
				t.Errorf("game %d: steps=%d, expected positive", i, r.Steps)
			}
			if len(r.PortfolioValues) != cfg.NumPlayers {
				t.Errorf("game %d: expected %d portfolio values, got %d",
					i, cfg.NumPlayers, len(r.PortfolioValues))
			}
			if r.WinnerIndex < 0 || r.WinnerIndex >= cfg.NumPlayers {
				t.Errorf("game %d: invalid winner index %d", i, r.WinnerIndex)
			}
		}
	})
}

func TestComputeMetrics(t *testing.T) {
	t.Run("no_panic_on_valid_input", func(t *testing.T) {
		cfg := DefaultRunConfig()
		cfg.NumGames = 2
		cfg.MaxSteps = 2000

		results := RunBatch(cfg)
		m := ComputeMetrics(results, cfg.NumPlayers, 7, 20)

		if m.NumGames != 2 {
			t.Errorf("expected 2 games, got %d", m.NumGames)
		}
		if m.GameLengthMean <= 0 {
			t.Error("expected positive mean game length")
		}
		if m.GiniCoefficient < 0 || m.GiniCoefficient > 1 {
			t.Errorf("gini coefficient %.3f out of range [0,1]", m.GiniCoefficient)
		}
		for i, v := range m.PortfolioMean {
			if math.IsNaN(v) {
				t.Errorf("NaN in portfolio mean for player %d", i)
			}
		}
	})

	t.Run("empty_results", func(t *testing.T) {
		m := ComputeMetrics(nil, 4, 7, 20)
		if m.NumGames != 0 {
			t.Errorf("expected 0 games, got %d", m.NumGames)
		}
	})
}

func TestGini(t *testing.T) {
	t.Run("equal_values", func(t *testing.T) {
		g := gini([]float64{100, 100, 100, 100})
		if g > 0.001 {
			t.Errorf("expected ~0 for equal values, got %.3f", g)
		}
	})

	t.Run("unequal_values", func(t *testing.T) {
		g := gini([]float64{0, 0, 0, 1000})
		if g < 0.5 {
			t.Errorf("expected high gini for unequal values, got %.3f", g)
		}
	})

	t.Run("single_value", func(t *testing.T) {
		g := gini([]float64{42})
		if g != 0 {
			t.Errorf("expected 0 for single value, got %.3f", g)
		}
	})
}
