package replay

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// EventType classifies a parsed transcript line.
type EventType int

const (
	EventIgnored EventType = iota
	EventPhaseHeader
	EventStockRoundHeader
	EventOperatingRoundHeader
	EventPrivateBuy
	EventPrivateBid
	EventPrivateAuctionWin
	EventPriorityDeal
	EventPar
	EventBuyShareIPO
	EventBuyShareMarket
	EventSellShares
	EventTileLay
	EventPlaceToken
	EventRunRoute
	EventPayDividends
	EventWithhold
	EventDoesNotRun
	EventBuyTrainDepot
	EventBuyTrainCompany
	EventBuyPrivateFromPlayer
	EventPass
	EventSkip
	EventFloat
	EventReceives
	EventSharePriceMove
	EventBankBroken
	EventGameOver
	EventPresidentChange
	EventPrivateRevenue
	EventCompanyPrivateRevenue
	EventDeclineSell
	EventNoValidActionsPass
	EventOperates
	EventTrainExchange
	EventTrainDiscard
	EventContributes
	EventTrainRustNotice
	EventPrivateCloseNotice
	EventDeclineBuyShares
	EventSellSingleShare
)

// Event is a single parsed transcript line.
type Event struct {
	Type     EventType
	Line     int    // 1-based line number
	Raw      string // original line text
	Player   string // player name (if applicable)
	Company  string // company symbol (if applicable)
	Private  string // private name (if applicable)

	// Numeric fields (meaning varies by event type).
	Amount      int     // price, cost, revenue, etc.
	Amount2     int     // secondary amount (e.g., number of shares)
	SharePct    int     // share percentage (10, 20)
	TileID      int     // tile number
	Rotation    int     // tile rotation
	HexID       string  // hex coordinate
	HexName     string  // hex name (if present)
	TrainType   string  // "2", "3", "4", "5", "6", "D"
	RouteStops  []string // hex IDs in the route
	RouteRev    int     // route revenue
	FromPrice   int     // share price before move
	ToPrice     int     // share price after move
	Direction   string  // "right", "left", "up", "down"
	FromCompany string  // company selling a train
	Scores      map[string]int // player → score (game over)
	PhaseNum    int     // phase number
	ORsPerSR    int     // operating rounds per SR
	TrainLimit  int     // train limit in phase
	SRNumber    int     // stock round number
	ORNumber    int     // operating round number (e.g., 7 in "7.1")
	ORSub       int     // operating round sub-number (e.g., 1 in "7.1")
	ORTotal     int     // total ORs in set (e.g., 2 in "of 2")
}

// ParseTranscript reads a transcript file and returns all parsed events.
func ParseTranscript(path string) ([]Event, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var events []Event
	scanner := bufio.NewScanner(f)
	lineNum := 0
	prevLine := ""
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		// Skip consecutive duplicate lines (18xx.games sometimes logs actions twice).
		if line == prevLine {
			continue
		}
		prevLine = line
		ev := parseLine(line, lineNum)
		events = append(events, ev)
	}
	return events, scanner.Err()
}

// Regex patterns for transcript lines.
var (
	reTimestamp = regexp.MustCompile(`^\[[\d:]+\]\s*`)
	reMasterMode = regexp.MustCompile(`^•\s*Action`)
	reOptionalRules = regexp.MustCompile(`^Optional rules`)

	rePhaseHeader = regexp.MustCompile(`-- Phase (\w+) \(Operating Rounds: (\d+) \| Train Limit: (\d+)`)
	reSRHeader = regexp.MustCompile(`-- Stock Round (\d+) --`)
	reORHeader = regexp.MustCompile(`-- Operating Round (\d+)\.(\d+) \(of (\d+)\) --`)
	reBankBroken = regexp.MustCompile(`-- The bank has broken --`)
	reGameOver = regexp.MustCompile(`-- Game over: (.+) --`)

	rePar = regexp.MustCompile(`^(.+?) pars (\w+) at ¥(\d+)`)
	reBuyShareIPO = regexp.MustCompile(`^(.+?) buys a (\d+)% share of (\w+) from the IPO for ¥(\d+)`)
	reBuyShareMarket = regexp.MustCompile(`^(.+?) buys a (\d+)% share of (\w+) from the (?:open )?market for ¥(\d+)`)
	reSellShares = regexp.MustCompile(`^(.+?) sells (\d+) shares? of (\w+) and receives ¥(\d+)`)

	rePrivateBuy = regexp.MustCompile(`^(.+?) buys (.+?) for ¥(\d+)$`)
	rePrivateBid = regexp.MustCompile(`^(.+?) bids ¥(\d+) for (.+)`)
	rePrivateAuctionWin = regexp.MustCompile(`^(.+?) wins the auction for (.+?) with`)
	rePriorityDeal = regexp.MustCompile(`^(.+?) has priority deal`)

	reTileLay = regexp.MustCompile(`^(\w+) lays tile #(\d+) with rotation (\d+) on (\w+)(?:\s+\((.+?)\))?`)
	reTileLayCost = regexp.MustCompile(`^(\w+) spends ¥(\d+) and lays tile #(\d+) with rotation (\d+) on (\w+)(?:\s+\((.+?)\))?`)
	rePlayerTileLay = regexp.MustCompile(`^(.+?) \((\w+)\) lays tile #(\d+) with rotation (\d+) on (\w+)(?:\s+\((.+?)\))?`)
	rePlaceToken = regexp.MustCompile(`^(\w+) places a token on (\w+)(?:\s+\((.+?)\))?(?:\s+for ¥(\d+))?`)

	reRunRoute = regexp.MustCompile(`^(\w+) runs a (\w+) train for ¥(\d+): (.+)`)
	rePayDividends = regexp.MustCompile(`^(\w+) pays out ¥(\d+)`)
	reWithhold = regexp.MustCompile(`^(\w+) withholds ¥(\d+)`)
	reDoesNotRun = regexp.MustCompile(`^(\w+) does not run`)

	reBuyTrainDepot   = regexp.MustCompile(`^(\w+) buys a (\w+) train for ¥(\d+) from The Depot`)
	reBuyTrainDiscard = regexp.MustCompile(`^(\w+) buys a (\w+) train for ¥(\d+) from The Discard`)
	reBuyTrainCompany = regexp.MustCompile(`^(\w+) buys a (\w+) train for ¥(\d+) from (\w+)`)

	reBuyPrivateFromPlayer = regexp.MustCompile(`^(\w+) buys (.+?) from (.+?) for ¥(\d+)`)

	reFloat = regexp.MustCompile(`^(\w+) floats`)
	reReceives = regexp.MustCompile(`^(\w+) receives ¥(\d+)`)
	reSharePriceMove = regexp.MustCompile(`^(\w+)'s share price moves (\w+) from ¥(\d+) to ¥(\d+)`)
	rePresidentChange = regexp.MustCompile(`^(.+?) becomes the president of (\w+)`)

	rePrivateRevenue = regexp.MustCompile(`^(.+?) collects ¥(\d+) from (.+)`)
	reDeclineSell = regexp.MustCompile(`^(.+?) declines to sell shares`)
	reNoValidActions = regexp.MustCompile(`^(.+?) has no valid actions and passes`)

	reOperates = regexp.MustCompile(`^(.+?) operates (\w+)`)
	reTrainExchange = regexp.MustCompile(`^(\w+) exchanges a (\w+) for a (\w+) train for ¥(\d+) from The Depot`)
	reTrainDiscard = regexp.MustCompile(`^(\w+) discards (\w+)`)
	reContributes = regexp.MustCompile(`^(.+?) contributes ¥(\d+)`)
	reEventNotice = regexp.MustCompile(`^-- Event: (.+) --`)
	reDeclineBuyShares = regexp.MustCompile(`^(.+?) declines to buy shares`)
	reSellSingleShare = regexp.MustCompile(`^(.+?) sells a (\d+)% share of (\w+) and receives ¥(\d+)`)

	rePass = regexp.MustCompile(`^(\w+) (?:passes|skips)`)
	rePlayerPass = regexp.MustCompile(`^(.+?) passes`)
)

func parseLine(line string, lineNum int) Event {
	ev := Event{Type: EventIgnored, Line: lineNum, Raw: line}

	// Strip timestamp prefix.
	line = reTimestamp.ReplaceAllString(line, "")
	line = strings.TrimSpace(line)

	// Skip metadata lines.
	if reMasterMode.MatchString(line) || reOptionalRules.MatchString(line) ||
		strings.HasPrefix(line, "* ") {
		return ev
	}

	// --- Headers ---
	if m := rePhaseHeader.FindStringSubmatch(line); m != nil {
		ev.Type = EventPhaseHeader
		ev.TrainType = m[1] // "2", "3", ..., "D"
		ev.PhaseNum, _ = strconv.Atoi(m[1]) // 0 for "D"
		ev.ORsPerSR, _ = strconv.Atoi(m[2])
		ev.TrainLimit, _ = strconv.Atoi(m[3])
		return ev
	}
	if m := reSRHeader.FindStringSubmatch(line); m != nil {
		ev.Type = EventStockRoundHeader
		ev.SRNumber, _ = strconv.Atoi(m[1])
		return ev
	}
	if m := reORHeader.FindStringSubmatch(line); m != nil {
		ev.Type = EventOperatingRoundHeader
		ev.ORNumber, _ = strconv.Atoi(m[1])
		ev.ORSub, _ = strconv.Atoi(m[2])
		ev.ORTotal, _ = strconv.Atoi(m[3])
		return ev
	}
	if reBankBroken.MatchString(line) {
		ev.Type = EventBankBroken
		return ev
	}
	if m := reGameOver.FindStringSubmatch(line); m != nil {
		ev.Type = EventGameOver
		ev.Scores = parseScores(m[1])
		return ev
	}

	// --- Stock Round actions ---
	if m := rePar.FindStringSubmatch(line); m != nil {
		ev.Type = EventPar
		ev.Player = m[1]
		ev.Company = m[2]
		ev.Amount, _ = strconv.Atoi(m[3])
		return ev
	}
	if m := reBuyShareIPO.FindStringSubmatch(line); m != nil {
		ev.Type = EventBuyShareIPO
		ev.Player = m[1]
		ev.SharePct, _ = strconv.Atoi(m[2])
		ev.Company = m[3]
		ev.Amount, _ = strconv.Atoi(m[4])
		return ev
	}
	if m := reBuyShareMarket.FindStringSubmatch(line); m != nil {
		ev.Type = EventBuyShareMarket
		ev.Player = m[1]
		ev.SharePct, _ = strconv.Atoi(m[2])
		ev.Company = m[3]
		ev.Amount, _ = strconv.Atoi(m[4])
		return ev
	}
	if m := reSellShares.FindStringSubmatch(line); m != nil {
		ev.Type = EventSellShares
		ev.Player = m[1]
		ev.Amount2, _ = strconv.Atoi(m[2]) // num shares
		ev.Company = m[3]
		ev.Amount, _ = strconv.Atoi(m[4]) // revenue
		return ev
	}
	if m := reDeclineSell.FindStringSubmatch(line); m != nil {
		ev.Type = EventDeclineSell
		ev.Player = m[1]
		return ev
	}
	if m := reNoValidActions.FindStringSubmatch(line); m != nil {
		ev.Type = EventNoValidActionsPass
		ev.Player = m[1]
		return ev
	}

	// --- Operating Round actions ---
	if m := reTileLayCost.FindStringSubmatch(line); m != nil {
		ev.Type = EventTileLay
		ev.Company = m[1]
		ev.Amount, _ = strconv.Atoi(m[2]) // cost
		ev.TileID, _ = strconv.Atoi(m[3])
		ev.Rotation, _ = strconv.Atoi(m[4])
		ev.HexID = m[5]
		if len(m) > 6 {
			ev.HexName = m[6]
		}
		return ev
	}
	if m := rePlayerTileLay.FindStringSubmatch(line); m != nil {
		ev.Type = EventTileLay
		ev.Player = m[1]
		ev.Private = m[2]
		ev.TileID, _ = strconv.Atoi(m[3])
		ev.Rotation, _ = strconv.Atoi(m[4])
		ev.HexID = m[5]
		if len(m) > 6 {
			ev.HexName = m[6]
		}
		return ev
	}
	if m := reTileLay.FindStringSubmatch(line); m != nil {
		ev.Type = EventTileLay
		ev.Company = m[1]
		ev.TileID, _ = strconv.Atoi(m[2])
		ev.Rotation, _ = strconv.Atoi(m[3])
		ev.HexID = m[4]
		if len(m) > 5 {
			ev.HexName = m[5]
		}
		return ev
	}
	if m := rePlaceToken.FindStringSubmatch(line); m != nil {
		ev.Type = EventPlaceToken
		ev.Company = m[1]
		ev.HexID = m[2]
		if m[3] != "" {
			ev.HexName = m[3]
		}
		if m[4] != "" {
			ev.Amount, _ = strconv.Atoi(m[4])
		}
		return ev
	}
	if m := reRunRoute.FindStringSubmatch(line); m != nil {
		ev.Type = EventRunRoute
		ev.Company = m[1]
		ev.TrainType = m[2]
		ev.RouteRev, _ = strconv.Atoi(m[3])
		ev.RouteStops = strings.Split(m[4], "-")
		return ev
	}
	if m := rePayDividends.FindStringSubmatch(line); m != nil {
		ev.Type = EventPayDividends
		ev.Company = m[1]
		ev.Amount, _ = strconv.Atoi(m[2])
		return ev
	}
	if m := reWithhold.FindStringSubmatch(line); m != nil {
		ev.Type = EventWithhold
		ev.Company = m[1]
		ev.Amount, _ = strconv.Atoi(m[2])
		return ev
	}
	if m := reDoesNotRun.FindStringSubmatch(line); m != nil {
		ev.Type = EventDoesNotRun
		ev.Company = m[1]
		return ev
	}

	// --- Train purchases ---
	if m := reBuyTrainDepot.FindStringSubmatch(line); m != nil {
		ev.Type = EventBuyTrainDepot
		ev.Company = m[1]
		ev.TrainType = m[2]
		ev.Amount, _ = strconv.Atoi(m[3])
		return ev
	}
	if m := reBuyTrainDiscard.FindStringSubmatch(line); m != nil {
		ev.Type = EventBuyTrainDepot // treat discard pool same as depot for engine
		ev.Company = m[1]
		ev.TrainType = m[2]
		ev.Amount, _ = strconv.Atoi(m[3])
		return ev
	}
	if m := reBuyTrainCompany.FindStringSubmatch(line); m != nil {
		ev.Type = EventBuyTrainCompany
		ev.Company = m[1]
		ev.TrainType = m[2]
		ev.Amount, _ = strconv.Atoi(m[3])
		ev.FromCompany = m[4]
		return ev
	}

	// --- Company buys private from player ---
	if m := reBuyPrivateFromPlayer.FindStringSubmatch(line); m != nil {
		ev.Type = EventBuyPrivateFromPlayer
		ev.Company = m[1]
		ev.Private = m[2]
		ev.Player = m[3]
		ev.Amount, _ = strconv.Atoi(m[4])
		return ev
	}

	// --- State changes ---
	if m := reFloat.FindStringSubmatch(line); m != nil {
		ev.Type = EventFloat
		ev.Company = m[1]
		return ev
	}
	if m := reReceives.FindStringSubmatch(line); m != nil {
		ev.Type = EventReceives
		ev.Company = m[1]
		ev.Amount, _ = strconv.Atoi(m[2])
		return ev
	}
	if m := reSharePriceMove.FindStringSubmatch(line); m != nil {
		ev.Type = EventSharePriceMove
		ev.Company = m[1]
		ev.Direction = m[2]
		ev.FromPrice, _ = strconv.Atoi(m[3])
		ev.ToPrice, _ = strconv.Atoi(m[4])
		return ev
	}
	if m := rePresidentChange.FindStringSubmatch(line); m != nil {
		ev.Type = EventPresidentChange
		ev.Player = m[1]
		ev.Company = m[2]
		return ev
	}

	// --- Private revenue ---
	if m := rePrivateRevenue.FindStringSubmatch(line); m != nil {
		ev.Type = EventPrivateRevenue
		ev.Player = m[1]
		ev.Amount, _ = strconv.Atoi(m[2])
		ev.Private = m[3]
		// Check if "player" is actually a company symbol (2-3 uppercase letters).
		if isCompanySymbol(m[1]) {
			ev.Type = EventCompanyPrivateRevenue
			ev.Company = m[1]
			ev.Player = ""
		}
		return ev
	}

	// --- Private auction events ---
	if m := rePrivateBid.FindStringSubmatch(line); m != nil {
		ev.Type = EventPrivateBid
		ev.Player = m[1]
		ev.Amount, _ = strconv.Atoi(m[2])
		ev.Private = m[3]
		return ev
	}
	if m := rePrivateAuctionWin.FindStringSubmatch(line); m != nil {
		ev.Type = EventPrivateAuctionWin
		ev.Player = m[1]
		ev.Private = m[2]
		return ev
	}
	if m := rePriorityDeal.FindStringSubmatch(line); m != nil {
		ev.Type = EventPriorityDeal
		ev.Player = m[1]
		return ev
	}

	// --- Private buy (must come after more specific patterns) ---
	if m := rePrivateBuy.FindStringSubmatch(line); m != nil {
		// Distinguish from share buy: private names have spaces.
		if strings.Contains(m[2], " ") {
			ev.Type = EventPrivateBuy
			ev.Player = m[1]
			ev.Private = m[2]
			ev.Amount, _ = strconv.Atoi(m[3])
			return ev
		}
	}

	// --- Operates (informational) ---
	if m := reOperates.FindStringSubmatch(line); m != nil {
		ev.Type = EventOperates
		ev.Player = m[1]
		ev.Company = m[2]
		return ev
	}

	// --- Train exchange (trade-in) ---
	if m := reTrainExchange.FindStringSubmatch(line); m != nil {
		ev.Type = EventTrainExchange
		ev.Company = m[1]
		ev.TrainType = m[3] // new train type
		ev.Amount, _ = strconv.Atoi(m[4])
		ev.FromCompany = m[2] // old train type (reusing field)
		return ev
	}

	// --- Train discard ---
	if m := reTrainDiscard.FindStringSubmatch(line); m != nil {
		ev.Type = EventTrainDiscard
		ev.Company = m[1]
		ev.TrainType = m[2]
		return ev
	}

	// --- Emergency contribution ---
	if m := reContributes.FindStringSubmatch(line); m != nil {
		ev.Type = EventContributes
		ev.Player = m[1]
		ev.Amount, _ = strconv.Atoi(m[2])
		return ev
	}

	// --- Event notices ---
	if m := reEventNotice.FindStringSubmatch(line); m != nil {
		notice := m[1]
		if strings.Contains(notice, "rust") {
			ev.Type = EventTrainRustNotice
		} else if strings.Contains(notice, "close") {
			ev.Type = EventPrivateCloseNotice
		} else {
			ev.Type = EventIgnored
		}
		return ev
	}

	// --- Decline to buy shares ---
	if m := reDeclineBuyShares.FindStringSubmatch(line); m != nil {
		ev.Type = EventDeclineBuyShares
		ev.Player = m[1]
		return ev
	}

	// --- Sell single share ---
	if m := reSellSingleShare.FindStringSubmatch(line); m != nil {
		ev.Type = EventSellSingleShare
		ev.Player = m[1]
		ev.SharePct, _ = strconv.Atoi(m[2])
		ev.Company = m[3]
		ev.Amount, _ = strconv.Atoi(m[4])
		return ev
	}

	// --- Pass/skip (catch-all, must come last) ---
	// Check company pass/skip first (short uppercase symbols like "IR", "KU").
	if m := rePass.FindStringSubmatch(line); m != nil {
		ev.Type = EventPass
		if isCompanySymbol(m[1]) {
			ev.Company = m[1]
		} else {
			ev.Player = m[1]
		}
		return ev
	}
	if m := rePlayerPass.FindStringSubmatch(line); m != nil {
		ev.Type = EventPass
		ev.Player = m[1]
		return ev
	}

	return ev
}

// isCompanySymbol returns true if s looks like a company symbol (2-3 uppercase letters).
func isCompanySymbol(s string) bool {
	if len(s) < 2 || len(s) > 3 {
		return false
	}
	for _, c := range s {
		if c < 'A' || c > 'Z' {
			return false
		}
	}
	return true
}

// parseScores extracts player scores from the game over line.
// Format: "Player1 (¥1234), Player2 (¥5678)"
func parseScores(s string) map[string]int {
	scores := make(map[string]int)
	re := regexp.MustCompile(`(.+?) \(¥(\d+)\)`)
	parts := strings.Split(s, "), ")
	for _, part := range parts {
		part = strings.TrimSuffix(part, ")")
		if m := re.FindStringSubmatch(part + ")"); m != nil {
			score, _ := strconv.Atoi(m[2])
			scores[strings.TrimSpace(m[1])] = score
		}
	}
	return scores
}

// String returns a human-readable description of the event.
func (e Event) String() string {
	switch e.Type {
	case EventPar:
		return fmt.Sprintf("PAR: %s pars %s at %d", e.Player, e.Company, e.Amount)
	case EventBuyShareIPO:
		return fmt.Sprintf("BUY_IPO: %s buys %d%% of %s for %d", e.Player, e.SharePct, e.Company, e.Amount)
	case EventSellShares:
		return fmt.Sprintf("SELL: %s sells %d of %s for %d", e.Player, e.Amount2, e.Company, e.Amount)
	case EventTileLay:
		return fmt.Sprintf("TILE: %s lays #%d rot %d on %s", e.Company, e.TileID, e.Rotation, e.HexID)
	case EventRunRoute:
		return fmt.Sprintf("ROUTE: %s %s-train for %d: %s", e.Company, e.TrainType, e.RouteRev, strings.Join(e.RouteStops, "-"))
	case EventPayDividends:
		return fmt.Sprintf("PAY: %s pays %d", e.Company, e.Amount)
	case EventWithhold:
		return fmt.Sprintf("WITHHOLD: %s withholds %d", e.Company, e.Amount)
	case EventBuyTrainDepot:
		return fmt.Sprintf("BUY_TRAIN: %s buys %s for %d from depot", e.Company, e.TrainType, e.Amount)
	default:
		return fmt.Sprintf("[%d] %s", e.Type, e.Raw)
	}
}
