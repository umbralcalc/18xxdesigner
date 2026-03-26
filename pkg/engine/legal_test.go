package engine

import (
	"testing"

	"github.com/umbralcalc/18xxdesigner/pkg/gamedata"
)

func TestLegalStockRoundActions(t *testing.T) {
	cfg := gamedata.Default1889Config()
	grid := gamedata.Default1889Market()

	t.Run("initial_state_has_par_and_pass", func(t *testing.T) {
		// At game start: no companies parred, player has 420 cash.
		ctx := &StockRoundContext{
			PlayerIndex:     0,
			PlayerCash:      420,
			PlayerCertCount: 0,
			PlayerShares:    make([]float64, 7),
			NumCompanies:    7,
			NumPlayers:      4,
			CertLimit:       14,
			MarketGrid:      grid,
			Config:          cfg,
			CompanyFloated:   make([]bool, 7),
			CompanyParPrice:  make([]float64, 7),
			CompanySharesIPO: make([]float64, 7),
			CompanySharesMkt: make([]float64, 7),
			CompanyPresident: make([]int, 7),
			MarketRow:        make([]int, 7),
			MarketCol:        make([]int, 7),
			SoldThisTurn:     make([]bool, 7),
		}
		// All companies unparred (MarketRow = -1).
		for i := range ctx.MarketRow {
			ctx.MarketRow[i] = -1
			ctx.CompanySharesIPO[i] = 10
		}

		actions := LegalStockRoundActions(ctx)

		// Should have: pass + 7 companies × 6 par values = 43 actions.
		parCount := 0
		passCount := 0
		for _, a := range actions {
			switch a.Values[ActionType] {
			case ActionPass:
				passCount++
			case ActionParCompany:
				parCount++
			}
		}

		if passCount != 1 {
			t.Errorf("expected 1 pass action, got %d", passCount)
		}
		if parCount != 7*6 {
			t.Errorf("expected %d par actions (7 companies × 6 par values), got %d", 7*6, parCount)
		}
	})

	t.Run("cert_limit_blocks_buy", func(t *testing.T) {
		ctx := &StockRoundContext{
			PlayerIndex:     0,
			PlayerCash:      1000,
			PlayerCertCount: 14, // at limit
			PlayerShares:    make([]float64, 7),
			NumCompanies:    7,
			NumPlayers:      4,
			CertLimit:       14,
			MarketGrid:      grid,
			Config:          cfg,
			CompanyFloated:   make([]bool, 7),
			CompanyParPrice:  make([]float64, 7),
			CompanySharesIPO: make([]float64, 7),
			CompanySharesMkt: make([]float64, 7),
			CompanyPresident: make([]int, 7),
			MarketRow:        make([]int, 7),
			MarketCol:        make([]int, 7),
			SoldThisTurn:     make([]bool, 7),
		}
		// Company 0 is parred and has IPO shares.
		ctx.MarketRow[0] = 0
		ctx.MarketCol[0] = 3
		ctx.CompanyParPrice[0] = 100
		ctx.CompanySharesIPO[0] = 8
		ctx.CompanyFloated[0] = true
		// Rest unparred.
		for i := 1; i < 7; i++ {
			ctx.MarketRow[i] = -1
			ctx.CompanySharesIPO[i] = 10
		}

		actions := LegalStockRoundActions(ctx)

		// At cert limit: no buy or par actions, only pass and sell.
		for _, a := range actions {
			if a.Values[ActionType] == ActionBuyShare {
				t.Error("should not be able to buy at cert limit")
			}
			if a.Values[ActionType] == ActionParCompany {
				t.Error("should not be able to par at cert limit")
			}
		}
	})

	t.Run("cant_buy_sold_this_turn", func(t *testing.T) {
		ctx := &StockRoundContext{
			PlayerIndex:     0,
			PlayerCash:      1000,
			PlayerCertCount: 2,
			PlayerShares:    make([]float64, 7),
			NumCompanies:    7,
			NumPlayers:      4,
			CertLimit:       14,
			MarketGrid:      grid,
			Config:          cfg,
			CompanyFloated:   make([]bool, 7),
			CompanyParPrice:  make([]float64, 7),
			CompanySharesIPO: make([]float64, 7),
			CompanySharesMkt: make([]float64, 7),
			CompanyPresident: make([]int, 7),
			MarketRow:        make([]int, 7),
			MarketCol:        make([]int, 7),
			SoldThisTurn:     make([]bool, 7),
		}
		// Company 0 parred with IPO shares.
		ctx.MarketRow[0] = 0
		ctx.MarketCol[0] = 3
		ctx.CompanyParPrice[0] = 100
		ctx.CompanySharesIPO[0] = 5
		ctx.CompanyFloated[0] = true
		// Mark as sold this turn.
		ctx.SoldThisTurn[0] = true

		for i := 1; i < 7; i++ {
			ctx.MarketRow[i] = -1
			ctx.CompanySharesIPO[i] = 10
		}

		actions := LegalStockRoundActions(ctx)

		for _, a := range actions {
			if a.Values[ActionType] == ActionBuyShare && int(a.Values[ActionArg0]) == 0 {
				t.Error("should not be able to buy company sold this turn")
			}
		}
	})

	t.Run("60_percent_limit", func(t *testing.T) {
		ctx := &StockRoundContext{
			PlayerIndex:     0,
			PlayerCash:      1000,
			PlayerCertCount: 6,
			PlayerShares:    make([]float64, 7),
			NumCompanies:    7,
			NumPlayers:      4,
			CertLimit:       14,
			MarketGrid:      grid,
			Config:          cfg,
			CompanyFloated:   make([]bool, 7),
			CompanyParPrice:  make([]float64, 7),
			CompanySharesIPO: make([]float64, 7),
			CompanySharesMkt: make([]float64, 7),
			CompanyPresident: make([]int, 7),
			MarketRow:        make([]int, 7),
			MarketCol:        make([]int, 7),
			SoldThisTurn:     make([]bool, 7),
		}
		ctx.PlayerShares[0] = 6 // already at 60%
		ctx.MarketRow[0] = 0
		ctx.MarketCol[0] = 3
		ctx.CompanyParPrice[0] = 100
		ctx.CompanySharesIPO[0] = 2
		ctx.CompanyFloated[0] = true

		for i := 1; i < 7; i++ {
			ctx.MarketRow[i] = -1
			ctx.CompanySharesIPO[i] = 10
		}

		actions := LegalStockRoundActions(ctx)

		for _, a := range actions {
			if a.Values[ActionType] == ActionBuyShare && int(a.Values[ActionArg0]) == 0 {
				t.Error("should not be able to buy beyond 60% limit")
			}
		}
	})

	t.Run("sell_generates_actions", func(t *testing.T) {
		ctx := &StockRoundContext{
			PlayerIndex:     0,
			PlayerCash:      100,
			PlayerCertCount: 3,
			PlayerShares:    make([]float64, 7),
			NumCompanies:    7,
			NumPlayers:      4,
			CertLimit:       14,
			MarketGrid:      grid,
			Config:          cfg,
			CompanyFloated:   make([]bool, 7),
			CompanyParPrice:  make([]float64, 7),
			CompanySharesIPO: make([]float64, 7),
			CompanySharesMkt: make([]float64, 7),
			CompanyPresident: make([]int, 7),
			MarketRow:        make([]int, 7),
			MarketCol:        make([]int, 7),
			SoldThisTurn:     make([]bool, 7),
		}
		ctx.PlayerShares[0] = 3
		ctx.CompanyPresident[0] = 1 // someone else is president
		ctx.MarketRow[0] = 0
		ctx.MarketCol[0] = 3
		ctx.CompanyParPrice[0] = 100
		ctx.CompanySharesIPO[0] = 5
		ctx.CompanyFloated[0] = true

		for i := 1; i < 7; i++ {
			ctx.MarketRow[i] = -1
			ctx.CompanySharesIPO[i] = 10
		}

		actions := LegalStockRoundActions(ctx)

		sellCount := 0
		for _, a := range actions {
			if a.Values[ActionType] == ActionSellShares && int(a.Values[ActionArg0]) == 0 {
				sellCount++
			}
		}
		// Can sell 1, 2, or 3 shares (not president).
		if sellCount != 3 {
			t.Errorf("expected 3 sell actions, got %d", sellCount)
		}
	})

	t.Run("president_cant_sell_last_share", func(t *testing.T) {
		ctx := &StockRoundContext{
			PlayerIndex:     0,
			PlayerCash:      100,
			PlayerCertCount: 2,
			PlayerShares:    make([]float64, 7),
			NumCompanies:    7,
			NumPlayers:      4,
			CertLimit:       14,
			MarketGrid:      grid,
			Config:          cfg,
			CompanyFloated:   make([]bool, 7),
			CompanyParPrice:  make([]float64, 7),
			CompanySharesIPO: make([]float64, 7),
			CompanySharesMkt: make([]float64, 7),
			CompanyPresident: make([]int, 7),
			MarketRow:        make([]int, 7),
			MarketCol:        make([]int, 7),
			SoldThisTurn:     make([]bool, 7),
		}
		ctx.PlayerShares[0] = 2 // president holds 2 shares
		ctx.CompanyPresident[0] = 0 // this player is president
		ctx.MarketRow[0] = 0
		ctx.MarketCol[0] = 3
		ctx.CompanyParPrice[0] = 100
		ctx.CompanyFloated[0] = true

		for i := 1; i < 7; i++ {
			ctx.MarketRow[i] = -1
			ctx.CompanySharesIPO[i] = 10
		}

		actions := LegalStockRoundActions(ctx)

		sellCount := 0
		for _, a := range actions {
			if a.Values[ActionType] == ActionSellShares && int(a.Values[ActionArg0]) == 0 {
				sellCount++
				numShares := int(a.Values[ActionArg0+1])
				if numShares >= 2 {
					t.Errorf("president should not be able to sell down to 0, but got sell %d", numShares)
				}
			}
		}
		// Can only sell 1 (keep at least 1 as president).
		if sellCount != 1 {
			t.Errorf("expected 1 sell action for president, got %d", sellCount)
		}
	})
}

func TestParValues(t *testing.T) {
	grid := gamedata.Default1889Market()
	pars := grid.ParValues()

	expectedPrices := map[int]bool{100: true, 90: true, 80: true, 75: true, 70: true, 65: true}
	for _, pv := range pars {
		if !expectedPrices[pv.Price] {
			t.Errorf("unexpected par value %d", pv.Price)
		}
	}
	if len(pars) != 6 {
		t.Errorf("expected 6 par values, got %d", len(pars))
	}
}
