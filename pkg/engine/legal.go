package engine

import (
	"github.com/umbralcalc/stochadex/pkg/simulator"
	"github.com/umbralcalc/18xxdesigner/pkg/gamedata"
)

// StockRoundContext holds pre-extracted state needed for legal move generation.
type StockRoundContext struct {
	PlayerIndex    int
	PlayerCash     float64
	PlayerCertCount float64
	PlayerShares   []float64 // shares held per company (len = numCompanies)
	PlayerPassed   bool

	// Company state (indexed by company ID).
	CompanyFloated   []bool
	CompanyParPrice  []float64
	CompanySharesIPO []float64
	CompanySharesMkt []float64
	CompanyPresident []int

	// Market state.
	MarketRow []int
	MarketCol []int

	// Game rules.
	CertLimit    int
	NumCompanies int
	NumPlayers   int
	MarketGrid   *gamedata.MarketGrid
	Config       *gamedata.GameConfig

	// Per-SR tracking: which companies the player sold this SR turn.
	// Encoded in action args — for now we track via a sold-this-turn bitfield.
	SoldThisTurn []bool
}

// ExtractSRContext reads all partition states to build a StockRoundContext.
func ExtractSRContext(
	playerIndex int,
	stateHistories []*simulator.StateHistory,
	cfg *gamedata.GameConfig,
	grid *gamedata.MarketGrid,
	numPlayers int,
	partitionLayout *PartitionLayout,
) *StockRoundContext {
	numCompanies := len(cfg.Companies)

	playerState := stateHistories[partitionLayout.PlayerPartitions[playerIndex]].Values.RawRowView(0)

	ctx := &StockRoundContext{
		PlayerIndex:     playerIndex,
		PlayerCash:      playerState[PlayerCash],
		PlayerCertCount: playerState[PlayerCertCount],
		PlayerShares:    make([]float64, numCompanies),
		PlayerPassed:    playerState[PlayerPassed] != 0,

		CompanyFloated:   make([]bool, numCompanies),
		CompanyParPrice:  make([]float64, numCompanies),
		CompanySharesIPO: make([]float64, numCompanies),
		CompanySharesMkt: make([]float64, numCompanies),
		CompanyPresident: make([]int, numCompanies),
		MarketRow:        make([]int, numCompanies),
		MarketCol:        make([]int, numCompanies),
		SoldThisTurn:     make([]bool, numCompanies),

		CertLimit:    cfg.CertLimits[numPlayers],
		NumCompanies: numCompanies,
		NumPlayers:   numPlayers,
		MarketGrid:   grid,
		Config:       cfg,
	}

	for i := 0; i < numCompanies; i++ {
		ctx.PlayerShares[i] = playerState[PlayerShareIdx(i)]
	}

	mktState := stateHistories[partitionLayout.MarketPartition].Values.RawRowView(0)
	for i := 0; i < numCompanies; i++ {
		compState := stateHistories[partitionLayout.CompanyPartitions[i]].Values.RawRowView(0)
		ctx.CompanyFloated[i] = compState[CompFloated] != 0
		ctx.CompanyParPrice[i] = compState[CompParPrice]
		ctx.CompanySharesIPO[i] = compState[CompSharesIPO]
		ctx.CompanySharesMkt[i] = compState[CompSharesMarket]
		ctx.CompanyPresident[i] = int(compState[CompPresident])
		ctx.MarketRow[i] = int(mktState[MarketRowIdx(i)])
		ctx.MarketCol[i] = int(mktState[MarketColIdx(i)])
	}

	return ctx
}

// Action represents a legal action with its encoded float64 vector.
type Action struct {
	Values [ActionStateWidth]float64
}

// LegalStockRoundActions returns all legal actions for the active player.
func LegalStockRoundActions(ctx *StockRoundContext) []Action {
	var actions []Action

	// Pass is always legal.
	actions = append(actions, passAction())

	// Buy actions.
	actions = append(actions, legalBuyActions(ctx)...)

	// Sell actions.
	actions = append(actions, legalSellActions(ctx)...)

	// Par actions (start a new company).
	actions = append(actions, legalParActions(ctx)...)

	return actions
}

func passAction() Action {
	var a Action
	a.Values[ActionType] = ActionPass
	return a
}

// legalBuyActions returns all legal share purchases.
func legalBuyActions(ctx *StockRoundContext) []Action {
	var actions []Action

	if int(ctx.PlayerCertCount) >= ctx.CertLimit {
		return nil // at cert limit
	}

	for i := 0; i < ctx.NumCompanies; i++ {
		// Can't buy a company you sold this turn.
		if ctx.SoldThisTurn[i] {
			continue
		}

		// Company must be parred (has a market position).
		if ctx.MarketRow[i] < 0 {
			continue
		}

		// Check if shares are available (IPO first, then market).
		price := float64(0)
		fromIPO := false
		if ctx.CompanySharesIPO[i] > 0 {
			price = ctx.CompanyParPrice[i]
			fromIPO = true
		} else if ctx.CompanySharesMkt[i] > 0 {
			price = float64(ctx.MarketGrid.Price(ctx.MarketRow[i], ctx.MarketCol[i]))
		} else {
			continue // no shares available
		}

		if ctx.PlayerCash < price {
			continue
		}

		// Can't exceed 60% of a single company (6 shares of 10).
		if ctx.PlayerShares[i] >= 6 {
			continue
		}

		var a Action
		a.Values[ActionType] = ActionBuyShare
		a.Values[ActionArg0] = float64(i)     // company ID
		a.Values[ActionArg0+1] = price         // cost
		if fromIPO {
			a.Values[ActionArg0+2] = 1 // 1=IPO, 0=market
		}
		actions = append(actions, a)
	}

	return actions
}

// legalSellActions returns all legal share sales.
func legalSellActions(ctx *StockRoundContext) []Action {
	var actions []Action

	for i := 0; i < ctx.NumCompanies; i++ {
		held := int(ctx.PlayerShares[i])
		if held <= 0 {
			continue
		}

		// Can't sell if this is the only share and you're president
		// (must maintain presidency or dump it).
		// For now: can't sell below presidency threshold if president.
		isPresident := ctx.CompanyPresident[i] == ctx.PlayerIndex

		// In 1889, must sell in blocks (MUST_SELL_IN_BLOCKS = true).
		// For simplicity, allow selling 1 share at a time.
		// President can't sell if it would make them not president and nobody
		// else has enough to take over, unless they're dumping.
		maxSellable := held
		if isPresident {
			// President must retain at least as many shares as the next-largest holder,
			// unless someone else can take the presidency.
			// Simplified: president can sell down to 1 share minimum, or sell all
			// if another player has enough to become president (2+ shares).
			maxSellable = held - 1 // keep at least 1 as president
			if maxSellable <= 0 {
				continue
			}
		}

		price := float64(ctx.MarketGrid.Price(ctx.MarketRow[i], ctx.MarketCol[i]))

		// Generate sell actions for 1..maxSellable shares.
		for n := 1; n <= maxSellable; n++ {
			var a Action
			a.Values[ActionType] = ActionSellShares
			a.Values[ActionArg0] = float64(i)         // company ID
			a.Values[ActionArg0+1] = float64(n)       // num shares
			a.Values[ActionArg0+2] = float64(n) * price // total revenue
			actions = append(actions, a)
		}
	}

	return actions
}

// legalParActions returns all legal company-starting actions.
func legalParActions(ctx *StockRoundContext) []Action {
	var actions []Action

	// Need room for 2 certs (president's share = 2 certs).
	if int(ctx.PlayerCertCount)+2 > ctx.CertLimit {
		return nil
	}

	parValues := ctx.MarketGrid.ParValues()

	for i := 0; i < ctx.NumCompanies; i++ {
		// Can only par a company that hasn't been parred yet.
		if ctx.MarketRow[i] >= 0 {
			continue
		}

		// Can't par a company you sold this turn.
		if ctx.SoldThisTurn[i] {
			continue
		}

		for _, pv := range parValues {
			cost := float64(pv.Price) * 2 // president's share = 2 shares
			if ctx.PlayerCash < cost {
				continue
			}

			var a Action
			a.Values[ActionType] = ActionParCompany
			a.Values[ActionArg0] = float64(i)          // company ID
			a.Values[ActionArg0+1] = float64(pv.Price) // par price
			a.Values[ActionArg0+2] = 2                   // shares bought (president's share)
			a.Values[ActionArg0+3] = float64(pv.Row)   // market row
			a.Values[ActionArg0+4] = float64(pv.Col)   // market col
			actions = append(actions, a)
		}
	}

	return actions
}

// PartitionLayout maps partition names to their indices.
// Built by the GameBuilder so the agent/legal-move code can find partitions.
type PartitionLayout struct {
	TurnPartition      int
	ActionPartition    int
	BankPartition      int
	MarketPartition    int
	MapPartition       int
	CompanyPartitions  []int // indexed by company ID
	PlayerPartitions   []int // indexed by player index
}
