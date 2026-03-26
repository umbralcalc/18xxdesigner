package engine

import (
	"github.com/umbralcalc/18xxdesigner/pkg/gamedata"
	"github.com/umbralcalc/stochadex/pkg/simulator"
)

// BankIteration tracks the bank's cash pool, available trains, and available tiles.
//
// State layout:
//
//	[0] cash
//	[1] train_phase (0-5, indexes into game phases)
//	[2..7] trains_available (count per train type, 6 types)
//	[8..N] tiles_available (count per tile manifest entry)
//
// Receives action_values via params_from_upstream.
type BankIteration struct {
	Config *gamedata.GameConfig
}

func (b *BankIteration) Configure(partitionIndex int, settings *simulator.Settings) {
	// No-op: config is set at construction time.
}

func (b *BankIteration) Iterate(
	params *simulator.Params,
	partitionIndex int,
	stateHistories []*simulator.StateHistory,
	timestepsHistory *simulator.CumulativeTimestepsHistory,
) []float64 {
	prev := stateHistories[partitionIndex].Values.RawRowView(0)
	state := make([]float64, len(prev))
	copy(state, prev)

	actionValues := params.Get("action_values")
	actionType := actionValues[ActionType]

	switch actionType {
	case ActionBuyTrain:
		b.handleBuyTrain(state, actionValues)
	case ActionLayTile:
		b.handleLayTile(state, actionValues)
	case ActionPayDividends:
		b.handlePayDividends(state, actionValues)
	case ActionExchangeTrain:
		b.handleExchangeTrain(state, actionValues)
	}

	return state
}

func (b *BankIteration) handleBuyTrain(state []float64, action []float64) {
	if len(action) < 3 {
		return
	}
	trainIdx := int(action[ActionArg0]) // which train type (0-5)
	if trainIdx < 0 || trainIdx >= len(b.Config.Trains) {
		return
	}

	cost := float64(b.Config.Trains[trainIdx].Price)

	// Decrease available count (if not unlimited).
	availIdx := BankTrainsBase + trainIdx
	if b.Config.Trains[trainIdx].Quantity >= 0 {
		if state[availIdx] <= 0 {
			return // none available
		}
		state[availIdx] -= 1
	}

	// Bank receives cash.
	state[BankCash] += cost

	// Check for phase advance.
	b.checkPhaseAdvance(state, trainIdx)
}

func (b *BankIteration) checkPhaseAdvance(state []float64, trainIdx int) {
	currentPhase := int(state[BankTrainPhase])
	// Phase advances when a train of the triggering type is first bought.
	for i := currentPhase + 1; i < len(b.Config.Phases); i++ {
		trigger := b.Config.Phases[i].TriggerTrain
		if trigger != "" && trigger == b.Config.Trains[trainIdx].Name {
			state[BankTrainPhase] = float64(i)

			// Handle train rusting: set available count to 0 for rusted types.
			for j, train := range b.Config.Trains {
				if train.RustsOn == b.Config.Trains[trainIdx].Name {
					state[BankTrainsBase+j] = 0
				}
			}
			break
		}
	}
}

// handlePayDividends decreases bank cash by total dividends paid out.
// The bank pays dividends on behalf of the company to all shareholders.
func (b *BankIteration) handlePayDividends(state []float64, action []float64) {
	revenue := action[ActionArg0+1]
	// All revenue is paid out from the bank to shareholders.
	state[BankCash] -= revenue
}

// handleExchangeTrain processes a train exchange: company returns old train to depot,
// pays cost, and gets new train. Bank receives cash and decreases new train availability.
func (b *BankIteration) handleExchangeTrain(state []float64, action []float64) {
	newTrainIdx := int(action[ActionArg0])
	cost := action[ActionArg0+1]

	if newTrainIdx < 0 || newTrainIdx >= len(b.Config.Trains) {
		return
	}

	// Decrease availability of new train.
	availIdx := BankTrainsBase + newTrainIdx
	if b.Config.Trains[newTrainIdx].Quantity >= 0 {
		if state[availIdx] <= 0 {
			return
		}
		state[availIdx] -= 1
	}

	// Bank receives the exchange cost.
	state[BankCash] += cost

	// Check for phase advance.
	b.checkPhaseAdvance(state, newTrainIdx)
}

func (b *BankIteration) handleLayTile(state []float64, action []float64) {
	if len(action) < 3 {
		return
	}
	tileManifestIdx := int(action[ActionArg0+1]) // which manifest entry
	tilesBase := BankTilesBase()
	idx := tilesBase + tileManifestIdx
	if idx < len(state) && state[idx] > 0 {
		state[idx] -= 1
	}
}

// BankStateWidth returns the total state width for the bank partition.
func BankStateWidth(cfg *gamedata.GameConfig) int {
	return BankTilesBase() + len(gamedata.Default1889TileManifest())
}

// InitBankState returns the initial state for the bank partition.
func InitBankState(cfg *gamedata.GameConfig) []float64 {
	manifest := gamedata.Default1889TileManifest()
	width := BankTilesBase() + len(manifest)
	state := make([]float64, width)

	state[BankCash] = float64(cfg.BankSize)
	state[BankTrainPhase] = 0

	// Initialize train availability.
	for i, train := range cfg.Trains {
		if train.Quantity < 0 {
			state[BankTrainsBase+i] = 99 // unlimited
		} else {
			state[BankTrainsBase+i] = float64(train.Quantity)
		}
	}

	// Initialize tile availability.
	tilesBase := BankTilesBase()
	for i, entry := range manifest {
		state[tilesBase+i] = float64(entry.Count)
	}

	return state
}

// BankBrokenTerminationCondition terminates the simulation when the bank's
// cash drops to zero or below.
type BankBrokenTerminationCondition struct {
	BankPartitionIndex int
}

func (b *BankBrokenTerminationCondition) Terminate(
	stateHistories []*simulator.StateHistory,
	timestepsHistory *simulator.CumulativeTimestepsHistory,
) bool {
	bankState := stateHistories[b.BankPartitionIndex].Values.RawRowView(0)
	return bankState[BankCash] <= 0
}

// OrTerminationCondition terminates when ANY of the given conditions is met.
type OrTerminationCondition struct {
	Conditions []simulator.TerminationCondition
}

func (o *OrTerminationCondition) Terminate(
	stateHistories []*simulator.StateHistory,
	timestepsHistory *simulator.CumulativeTimestepsHistory,
) bool {
	for _, c := range o.Conditions {
		if c.Terminate(stateHistories, timestepsHistory) {
			return true
		}
	}
	return false
}
