package engine

// State index constants for the turn partition.
// The turn partition is the master FSM controlling game flow.
const (
	TurnGamePhase      = iota // 0=phase2, 1=phase3, 2=phase4, 3=phase5, 4=phase6, 5=phaseD
	TurnRoundType             // 0=private_auction, 1=stock_round, 2=operating_round
	TurnORNumber              // current OR number within this set (1-based)
	TurnORsThisSet            // how many ORs in this SR→OR set
	TurnActiveType            // 0=player, 1=company
	TurnActiveID              // index of the active player or company
	TurnActionStep            // sub-step within current action (0=awaiting, 1+=processing)
	TurnPriorityDeal          // player index holding priority deal
	TurnStateWidth            // total width of turn state
)

// RoundType constants.
const (
	RoundPrivateAuction = 0.0
	RoundStockRound     = 1.0
	RoundOperatingRound = 2.0
)

// ActiveEntityType constants.
const (
	ActivePlayer  = 0.0
	ActiveCompany = 1.0
)

// OR sub-step constants. During an operating round, each company goes through
// these sub-steps in order. TurnActionStep holds the current sub-step.
const (
	ORStepTileLay   = 0.0
	ORStepToken     = 1.0
	ORStepRoutes    = 2.0 // route running + dividend/withhold decision
	ORStepBuyTrain  = 3.0
	ORStepDone      = 4.0 // sentinel: company's turn is complete
)

// State index constants for the action partition.
// The action partition carries the chosen action each step.
const (
	ActionType = 0 // action type code
	ActionArg0 = 1 // first argument (meaning varies by action type)
	// ActionArg1 through ActionArg18 follow sequentially.
	ActionStateWidth = 20
)

// Action type codes.
const (
	ActionPass           = 0.0
	ActionBuyShare       = 1.0
	ActionSellShares     = 2.0
	ActionParCompany     = 3.0
	ActionLayTile        = 4.0
	ActionPlaceToken     = 5.0
	ActionRunRoutes      = 6.0
	ActionPayDividends   = 7.0
	ActionWithhold       = 8.0
	ActionBuyTrain       = 9.0
	ActionBuyPrivate     = 10.0
	ActionBidPrivate         = 11.0
	ActionPrivateAuctionPass = 12.0
	ActionBuyTrainFromCompany = 13.0 // buy train from another company
	ActionExchangeTrain      = 14.0 // exchange old train + cash for new from depot
)

// State index constants for bank partition.
// Layout: [cash, train_phase, trains_available[6], tiles_available[N]]
const (
	BankCash       = 0
	BankTrainPhase = 1
	BankTrainsBase = 2 // indices 2-7: available count for each train type
)

// BankTilesBase returns the starting index for tile availability in bank state.
// Follows after the 6 train type slots.
func BankTilesBase() int { return BankTrainsBase + 6 }

// State index constants for market partition.
// Layout: 7 companies x 2 (row, col).
const (
	MarketCompanyStride = 2 // (row, col) per company
)

// MarketRowIdx returns the state index for a company's market row.
func MarketRowIdx(companyID int) int { return companyID * MarketCompanyStride }

// MarketColIdx returns the state index for a company's market column.
func MarketColIdx(companyID int) int { return companyID*MarketCompanyStride + 1 }

// State index constants for company partitions.
// Each company has its own partition.
const (
	CompTreasury       = 0
	CompFloated        = 1  // 0=not floated, 1=floated
	CompTrainsBase     = 2  // indices 2-7: count of each train type held
	CompTokensRemain   = 8
	CompParPrice       = 9
	CompPresident      = 10 // player index of president
	CompSharesIPO      = 11 // shares remaining in IPO
	CompSharesMarket   = 12 // shares in the open market (sold by players)
	CompLastRevenue    = 13
	CompReceivership   = 14 // 0=normal, 1=receivership
	CompOperatedThisOR = 15 // 0=not yet, 1=operated
	CompStateWidth     = 16
)

// State index constants for player partitions.
// Each player has its own partition.
const (
	PlayerCash         = 0
	PlayerSharesBase   = 1  // indices 1-7: shares held per company (7 companies)
	PlayerPrivatesBase = 8  // indices 8-14: 1.0 if holding private, else 0.0 (7 privates)
	PlayerCertCount    = 15 // current certificate count
	PlayerPassed       = 16 // 1.0 if passed this SR round
	PlayerStateWidth   = 17
)

// PlayerShareIdx returns the state index for a player's share count of a company.
func PlayerShareIdx(companyID int) int { return PlayerSharesBase + companyID }

// PlayerPrivateIdx returns the state index for whether a player holds a private.
func PlayerPrivateIdx(privateID int) int { return PlayerPrivatesBase + privateID }

// State index constants for map partition.
// Layout: numHexes x 3 (tile_id, orientation, token_bitfield).
const (
	MapHexStride = 3
)

// MapTileIdx returns the state index for a hex's placed tile ID.
func MapTileIdx(hexIdx int) int { return hexIdx * MapHexStride }

// MapOrientIdx returns the state index for a hex's tile orientation (0-5).
func MapOrientIdx(hexIdx int) int { return hexIdx*MapHexStride + 1 }

// MapTokenIdx returns the state index for a hex's token bitfield.
// Each bit represents a company token in that hex's slots.
func MapTokenIdx(hexIdx int) int { return hexIdx*MapHexStride + 2 }
