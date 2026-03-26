package replay

import (
	"fmt"
	"strings"

	"github.com/umbralcalc/ttdesigner/pkg/engine"
	"github.com/umbralcalc/ttdesigner/pkg/gamedata"
)

// ReplayAgent replays a parsed transcript as engine actions.
type ReplayAgent struct {
	Events            []Event
	cursor            int
	playerNames       []string
	playerIndex       map[string]int
	companyIndex      map[string]int
	config            *gamedata.GameConfig
	Log               []ReplayStep
	lastPriorityDeal  int // last known priority deal holder from transcript
}

// ReplayStep records what happened at each engine step.
type ReplayStep struct {
	Step      int
	Event     *Event
	Action    []float64
	TurnState []float64
	Error     string
}

// NewReplayAgent creates a replay agent from parsed events and game config.
func NewReplayAgent(events []Event, config *gamedata.GameConfig, playerNames []string) *ReplayAgent {
	ra := &ReplayAgent{
		Events:       events,
		playerNames:  playerNames,
		playerIndex:  make(map[string]int),
		companyIndex: make(map[string]int),
		config:       config,
	}
	for i, name := range playerNames {
		ra.playerIndex[name] = i
	}
	for i, c := range config.Companies {
		ra.companyIndex[c.Sym] = i
	}
	return ra
}

// ChooseAction implements engine.Agent.
func (ra *ReplayAgent) ChooseAction(ctx *engine.GameContext) []float64 {
	turnState := ctx.TurnState
	roundType := turnState[engine.TurnRoundType]
	step := int(ctx.TimestepsHistory.CurrentStepNumber)

	var action []float64
	var ev *Event
	var errMsg string

	switch roundType {
	case engine.RoundPrivateAuction:
		action = passAction()
	case engine.RoundStockRound:
		action, ev, errMsg = ra.handleStockRound(ctx)
	case engine.RoundOperatingRound:
		action, ev, errMsg = ra.handleOperatingRound(ctx)
	default:
		action = passAction()
	}

	ra.Log = append(ra.Log, ReplayStep{
		Step:      step,
		Event:     ev,
		Action:    action,
		TurnState: append([]float64{}, turnState...),
		Error:     errMsg,
	})

	return action
}

// skipNonActions advances the cursor past info, consequence, and home token events.
func (ra *ReplayAgent) skipNonActions() {
	for ra.cursor < len(ra.Events) {
		ev := &ra.Events[ra.cursor]
		if isInfoEvent(ev.Type) || isConsequenceEvent(ev.Type) {
			// Track priority deal changes from transcript.
			if ev.Type == EventPriorityDeal {
				if idx, ok := ra.playerIndex[ev.Player]; ok {
					ra.lastPriorityDeal = idx
				}
			}
			ra.cursor++
			continue
		}
		// Home token placement: PlaceToken before "operates" with no cost.
		// These appear at the start of OR before any actions.
		if ev.Type == EventPlaceToken && ra.isHomeToken(ev) {
			ra.cursor++
			continue
		}
		break
	}
}

// isHomeToken checks if a PlaceToken event is a home station token
// (placed at company's home hex, no cost).
func (ra *ReplayAgent) isHomeToken(ev *Event) bool {
	if ev.Amount > 0 {
		return false
	}
	compIdx, ok := ra.companyIndex[ev.Company]
	if !ok {
		return false
	}
	return ra.config.Companies[compIdx].HomeHex == ev.HexID
}

// handleStockRound finds the next SR event for the active player.
func (ra *ReplayAgent) handleStockRound(ctx *engine.GameContext) ([]float64, *Event, string) {
	playerID := int(ctx.TurnState[engine.TurnActiveID])
	playerName := ra.playerNames[playerID]

	for ra.cursor < len(ra.Events) {
		ra.skipNonActions()
		if ra.cursor >= len(ra.Events) {
			break
		}
		ev := &ra.Events[ra.cursor]

		// Skip non-action SR events.
		if ev.Type == EventDeclineSell || ev.Type == EventDeclineBuyShares ||
			ev.Type == EventContributes || ev.Type == EventBuyPrivateFromPlayer {
			ra.cursor++
			continue
		}

		// Match player.
		if !matchPlayer(ev.Player, playerName) {
			// If this is a non-player event (company action) we've drifted into
			// OR territory. Pass without consuming — the OR handler will process it.
			if ev.Player == "" {
				return passAction(), nil, ""
			}
			// If player order is mismatched but the event player is valid,
			// pass silently to let engine cycle to the correct player.
			if _, ok := ra.playerIndex[ev.Player]; ok {
				return passAction(), nil, ""
			}
			return passAction(), nil, fmt.Sprintf(
				"cursor %d: SR expected %s but got %q: %s",
				ra.cursor, playerName, ev.Player, ev.Raw)
		}

		ra.cursor++

		switch ev.Type {
		case EventPar:
			ra.skipParConsequences(ev.Company)
			return ra.convertPar(ev), ev, ""
		case EventBuyShareIPO:
			return ra.convertBuyShareIPO(ev), ev, ""
		case EventBuyShareMarket:
			return ra.convertBuyShareMarket(ev), ev, ""
		case EventSellShares:
			return ra.convertSellShares(ev), ev, ""
		case EventSellSingleShare:
			return ra.convertSellSingleShare(ev), ev, ""
		case EventPass, EventNoValidActionsPass:
			return passAction(), ev, ""
		default:
			return passAction(), ev, fmt.Sprintf("unhandled SR event: %s", ev.Raw)
		}
	}

	return passAction(), nil, "ran out of events in SR"
}

// handleOperatingRound finds the next OR event for the active company.
func (ra *ReplayAgent) handleOperatingRound(ctx *engine.GameContext) ([]float64, *Event, string) {
	companyID := int(ctx.TurnState[engine.TurnActiveID])
	orStep := ctx.TurnState[engine.TurnActionStep]

	companySym := ""
	if companyID < len(ra.config.Companies) {
		companySym = ra.config.Companies[companyID].Sym
	}

	// Unfloated companies pass all steps.
	compState := ctx.StateHistories[ctx.Layout.CompanyPartitions[companyID]].Values.RawRowView(0)
	if compState[engine.CompFloated] == 0 {
		return passAction(), nil, ""
	}

	ra.skipNonActions()

	switch orStep {
	case engine.ORStepTileLay:
		return ra.orTileLay(companySym, companyID)
	case engine.ORStepToken:
		return ra.orToken(companySym, companyID)
	case engine.ORStepRoutes:
		return ra.orRoutes(companySym, companyID)
	case engine.ORStepBuyTrain:
		return ra.orBuyTrain(companySym, companyID)
	}

	return passAction(), nil, ""
}

func (ra *ReplayAgent) orTileLay(companySym string, companyID int) ([]float64, *Event, string) {
	if ra.cursor >= len(ra.Events) {
		return passAction(), nil, ""
	}
	ev := &ra.Events[ra.cursor]

	// Match tile lay for this company (or player-triggered via private).
	if ev.Type == EventTileLay && (ev.Company == companySym || ev.Private != "") {
		ra.cursor++
		// Skip a "passes lay track" that sometimes follows the tile lay.
		ra.skipCompanyPassSkip(companySym)
		return ra.convertTileLay(ev, companyID), ev, ""
	}

	// Pass/skip for this company.
	if ra.isCompanyPassOrSkip(ev, companySym) {
		ra.cursor++
		return passAction(), ev, ""
	}

	// Event doesn't match — company skips tile lay.
	return passAction(), nil, ""
}

func (ra *ReplayAgent) orToken(companySym string, companyID int) ([]float64, *Event, string) {
	ra.skipNonActions()
	if ra.cursor >= len(ra.Events) {
		return passAction(), nil, ""
	}
	ev := &ra.Events[ra.cursor]

	if ev.Type == EventPlaceToken && ev.Company == companySym {
		ra.cursor++
		return ra.convertPlaceToken(ev, companyID), ev, ""
	}

	if ra.isCompanyPassOrSkip(ev, companySym) {
		ra.cursor++
		return passAction(), ev, ""
	}

	return passAction(), nil, ""
}

func (ra *ReplayAgent) orRoutes(companySym string, companyID int) ([]float64, *Event, string) {
	ra.skipNonActions()

	// Collect route-run events, then find the dividend/withhold decision.
	for ra.cursor < len(ra.Events) {
		ev := &ra.Events[ra.cursor]

		if isInfoEvent(ev.Type) || isConsequenceEvent(ev.Type) {
			ra.cursor++
			continue
		}

		if ev.Type == EventRunRoute && ev.Company == companySym {
			ra.cursor++
			continue
		}

		if ev.Type == EventPayDividends && ev.Company == companySym {
			ra.cursor++
			action := make([]float64, engine.ActionStateWidth)
			action[engine.ActionType] = engine.ActionPayDividends
			action[engine.ActionArg0] = float64(companyID)
			action[engine.ActionArg0+1] = float64(ev.Amount)
			return action, ev, ""
		}

		if ev.Type == EventWithhold && ev.Company == companySym {
			ra.cursor++
			action := make([]float64, engine.ActionStateWidth)
			action[engine.ActionType] = engine.ActionWithhold
			action[engine.ActionArg0] = float64(companyID)
			action[engine.ActionArg0+1] = float64(ev.Amount)
			return action, ev, ""
		}

		if ev.Type == EventDoesNotRun && ev.Company == companySym {
			ra.cursor++
			action := make([]float64, engine.ActionStateWidth)
			action[engine.ActionType] = engine.ActionWithhold
			action[engine.ActionArg0] = float64(companyID)
			return action, ev, ""
		}

		if ra.isCompanyPassOrSkip(ev, companySym) &&
			strings.Contains(ev.Raw, "run") {
			ra.cursor++
			action := make([]float64, engine.ActionStateWidth)
			action[engine.ActionType] = engine.ActionWithhold
			action[engine.ActionArg0] = float64(companyID)
			return action, ev, ""
		}

		break
	}

	// No revenue events found — withhold 0.
	action := make([]float64, engine.ActionStateWidth)
	action[engine.ActionType] = engine.ActionWithhold
	action[engine.ActionArg0] = float64(companyID)
	return action, nil, ""
}

func (ra *ReplayAgent) orBuyTrain(companySym string, companyID int) ([]float64, *Event, string) {
	ra.skipNonActions()

	// Look for the next train purchase. If found, return it (engine will call
	// us again since it stays at BuyTrain step until we pass).
	for ra.cursor < len(ra.Events) {
		ev := &ra.Events[ra.cursor]

		if isInfoEvent(ev.Type) || isConsequenceEvent(ev.Type) {
			ra.cursor++
			continue
		}

		if ev.Type == EventBuyTrainDepot && ev.Company == companySym {
			ra.cursor++
			return ra.convertBuyTrainDepot(ev, companyID), ev, ""
		}

		if ev.Type == EventBuyTrainCompany && ev.Company == companySym {
			ra.cursor++
			return ra.convertBuyTrainFromCompany(ev, companyID), ev, ""
		}

		if ev.Type == EventTrainExchange && ev.Company == companySym {
			ra.cursor++
			return ra.convertExchangeTrain(ev, companyID), ev, ""
		}

		// Non-train events for this company: buy private, contributions, discards, passes.
		// Consume them all and pass (done buying trains).
		if ev.Type == EventBuyPrivateFromPlayer ||
			ev.Type == EventContributes ||
			ev.Type == EventTrainDiscard ||
			ra.isCompanyPassOrSkip(ev, companySym) {
			ra.consumeRemainingOREvents(companySym)
			return passAction(), nil, ""
		}

		// Event for a different company or next round — done.
		break
	}

	return passAction(), nil, ""
}

// consumeRemainingOREvents advances past all remaining events for a company's OR turn
// (pass/skip train, pass/skip buy companies, etc).
func (ra *ReplayAgent) consumeRemainingOREvents(companySym string) {
	for ra.cursor < len(ra.Events) {
		ev := &ra.Events[ra.cursor]
		if isInfoEvent(ev.Type) || isConsequenceEvent(ev.Type) {
			ra.cursor++
			continue
		}
		if ev.Type == EventBuyPrivateFromPlayer ||
			ev.Type == EventContributes ||
			ev.Type == EventTrainDiscard {
			ra.cursor++
			continue
		}
		// Private-triggered tile lays during buy-companies phase.
		if ev.Type == EventTileLay && ev.Private != "" {
			ra.cursor++
			continue
		}
		if ra.isCompanyPassOrSkip(ev, companySym) {
			ra.cursor++
			continue
		}
		break
	}
}

// --- Helpers ---

func (ra *ReplayAgent) isCompanyPassOrSkip(ev *Event, companySym string) bool {
	if ev.Type != EventPass && ev.Type != EventSkip {
		return false
	}
	return ev.Company == companySym || strings.Contains(ev.Raw, companySym)
}

func (ra *ReplayAgent) skipCompanyPassSkip(companySym string) {
	for ra.cursor < len(ra.Events) {
		ev := &ra.Events[ra.cursor]
		if isInfoEvent(ev.Type) || isConsequenceEvent(ev.Type) {
			ra.cursor++
			continue
		}
		if ra.isCompanyPassOrSkip(ev, companySym) &&
			(strings.Contains(ev.Raw, "lay") || strings.Contains(ev.Raw, "track")) {
			ra.cursor++
			continue
		}
		break
	}
}

// --- Action converters ---

func (ra *ReplayAgent) convertPar(ev *Event) []float64 {
	companyID, ok := ra.companyIndex[ev.Company]
	if !ok {
		return passAction()
	}
	action := make([]float64, engine.ActionStateWidth)
	action[engine.ActionType] = engine.ActionParCompany
	action[engine.ActionArg0] = float64(companyID)
	action[engine.ActionArg0+1] = float64(ev.Amount)
	action[engine.ActionArg0+2] = 2
	return action
}

func (ra *ReplayAgent) convertBuyShareIPO(ev *Event) []float64 {
	companyID, ok := ra.companyIndex[ev.Company]
	if !ok {
		return passAction()
	}
	action := make([]float64, engine.ActionStateWidth)
	action[engine.ActionType] = engine.ActionBuyShare
	action[engine.ActionArg0] = float64(companyID)
	action[engine.ActionArg0+1] = float64(ev.Amount)
	return action
}

func (ra *ReplayAgent) convertBuyShareMarket(ev *Event) []float64 {
	companyID, ok := ra.companyIndex[ev.Company]
	if !ok {
		return passAction()
	}
	action := make([]float64, engine.ActionStateWidth)
	action[engine.ActionType] = engine.ActionBuyShare
	action[engine.ActionArg0] = float64(companyID)
	action[engine.ActionArg0+1] = float64(ev.Amount)
	return action
}

func (ra *ReplayAgent) convertSellShares(ev *Event) []float64 {
	companyID, ok := ra.companyIndex[ev.Company]
	if !ok {
		return passAction()
	}
	action := make([]float64, engine.ActionStateWidth)
	action[engine.ActionType] = engine.ActionSellShares
	action[engine.ActionArg0] = float64(companyID)
	action[engine.ActionArg0+1] = float64(ev.Amount2)
	action[engine.ActionArg0+2] = float64(ev.Amount)
	return action
}

func (ra *ReplayAgent) convertSellSingleShare(ev *Event) []float64 {
	companyID, ok := ra.companyIndex[ev.Company]
	if !ok {
		return passAction()
	}
	action := make([]float64, engine.ActionStateWidth)
	action[engine.ActionType] = engine.ActionSellShares
	action[engine.ActionArg0] = float64(companyID)
	action[engine.ActionArg0+1] = 1
	action[engine.ActionArg0+2] = float64(ev.Amount)
	return action
}

func (ra *ReplayAgent) convertTileLay(ev *Event, companyID int) []float64 {
	hexes := gamedata.Default1889Map()
	hexIdx := -1
	for i, h := range hexes {
		if h.ID == ev.HexID {
			hexIdx = i
			break
		}
	}
	if hexIdx < 0 {
		return passAction()
	}
	action := make([]float64, engine.ActionStateWidth)
	action[engine.ActionType] = engine.ActionLayTile
	action[engine.ActionArg0] = float64(hexIdx)
	action[engine.ActionArg0+1] = float64(ev.TileID)
	action[engine.ActionArg0+2] = float64(ev.Rotation)
	return action
}

func (ra *ReplayAgent) convertPlaceToken(ev *Event, companyID int) []float64 {
	hexes := gamedata.Default1889Map()
	hexIdx := -1
	for i, h := range hexes {
		if h.ID == ev.HexID {
			hexIdx = i
			break
		}
	}
	if hexIdx < 0 {
		return passAction()
	}
	companyBit := float64(int(1) << companyID)
	action := make([]float64, engine.ActionStateWidth)
	action[engine.ActionType] = engine.ActionPlaceToken
	action[engine.ActionArg0] = float64(hexIdx)
	action[engine.ActionArg0+1] = companyBit
	action[engine.ActionArg0+2] = float64(ev.Amount)
	return action
}

func (ra *ReplayAgent) convertBuyTrainDepot(ev *Event, companyID int) []float64 {
	trainIdx := -1
	for i, tr := range ra.config.Trains {
		if tr.Name == ev.TrainType {
			trainIdx = i
			break
		}
	}
	if trainIdx < 0 {
		return passAction()
	}
	action := make([]float64, engine.ActionStateWidth)
	action[engine.ActionType] = engine.ActionBuyTrain
	action[engine.ActionArg0] = float64(trainIdx)
	action[engine.ActionArg0+1] = float64(ev.Amount)
	return action
}

func (ra *ReplayAgent) convertBuyTrainFromCompany(ev *Event, buyerCompanyID int) []float64 {
	trainIdx := -1
	for i, tr := range ra.config.Trains {
		if tr.Name == ev.TrainType {
			trainIdx = i
			break
		}
	}
	if trainIdx < 0 {
		return passAction()
	}
	sellerIdx, ok := ra.companyIndex[ev.FromCompany]
	if !ok {
		return passAction()
	}
	action := make([]float64, engine.ActionStateWidth)
	action[engine.ActionType] = engine.ActionBuyTrainFromCompany
	action[engine.ActionArg0] = float64(trainIdx)
	action[engine.ActionArg0+1] = float64(ev.Amount)
	action[engine.ActionArg0+2] = float64(sellerIdx)
	return action
}

func (ra *ReplayAgent) convertExchangeTrain(ev *Event, companyID int) []float64 {
	newTrainIdx := -1
	oldTrainIdx := -1
	for i, tr := range ra.config.Trains {
		if tr.Name == ev.TrainType {
			newTrainIdx = i
		}
		if tr.Name == ev.FromCompany {
			// FromCompany is reused for the old train type in exchange events
			oldTrainIdx = i
		}
	}
	if newTrainIdx < 0 || oldTrainIdx < 0 {
		return passAction()
	}
	action := make([]float64, engine.ActionStateWidth)
	action[engine.ActionType] = engine.ActionExchangeTrain
	action[engine.ActionArg0] = float64(newTrainIdx)
	action[engine.ActionArg0+1] = float64(ev.Amount)
	action[engine.ActionArg0+2] = float64(oldTrainIdx)
	return action
}

// --- Skip / classify helpers ---

func passAction() []float64 {
	action := make([]float64, engine.ActionStateWidth)
	action[engine.ActionType] = engine.ActionPass
	return action
}

func isInfoEvent(t EventType) bool {
	switch t {
	case EventPhaseHeader, EventStockRoundHeader, EventOperatingRoundHeader,
		EventOperates, EventTrainRustNotice, EventPrivateCloseNotice,
		EventPriorityDeal, EventPrivateRevenue, EventCompanyPrivateRevenue,
		EventIgnored, EventDeclineBuyShares:
		return true
	}
	return false
}

func isConsequenceEvent(t EventType) bool {
	switch t {
	case EventFloat, EventReceives, EventSharePriceMove, EventPresidentChange,
		EventBankBroken, EventGameOver:
		return true
	}
	return false
}

func matchPlayer(eventName, engineName string) bool {
	return eventName != "" && eventName == engineName
}

// skipParConsequences skips the "buys a 20% share" and "becomes president" lines
// that follow a par action in the transcript.
func (ra *ReplayAgent) skipParConsequences(companySym string) {
	for ra.cursor < len(ra.Events) {
		ev := &ra.Events[ra.cursor]
		if ev.Type == EventBuyShareIPO && ev.Company == companySym && ev.SharePct == 20 {
			ra.cursor++
			continue
		}
		if ev.Type == EventPresidentChange && ev.Company == companySym {
			ra.cursor++
			continue
		}
		break
	}
}

// ExtractPlayerNames finds player names from the transcript events.
// The priority deal holder is placed first (index 0).
func ExtractPlayerNames(events []Event) []string {
	seen := make(map[string]bool)
	var names []string
	for _, ev := range events {
		if ev.Player == "" {
			continue
		}
		if !seen[ev.Player] &&
			(ev.Type == EventPrivateBuy || ev.Type == EventPrivateBid || ev.Type == EventPar) {
			seen[ev.Player] = true
			names = append(names, ev.Player)
		}
	}

	for _, ev := range events {
		if ev.Type == EventPriorityDeal && ev.Player != "" {
			for i, name := range names {
				if name == ev.Player && i != 0 {
					names[0], names[i] = names[i], names[0]
				}
			}
			break
		}
	}

	return names
}

// SkipToStockRound advances the cursor past the private auction.
func (ra *ReplayAgent) SkipToStockRound() {
	for ra.cursor < len(ra.Events) {
		if ra.Events[ra.cursor].Type == EventStockRoundHeader {
			ra.cursor++
			return
		}
		ra.cursor++
	}
}

func (ra *ReplayAgent) Cursor() int          { return ra.cursor }
func (ra *ReplayAgent) RemainingEvents() int  { return len(ra.Events) - ra.cursor }
