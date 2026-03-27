package engine

import (
	"github.com/umbralcalc/18xxdesigner/pkg/gamedata"
	"github.com/umbralcalc/stochadex/pkg/simulator"
)

// GameContext provides everything an agent needs to make a decision.
type GameContext struct {
	TurnState        []float64
	StateHistories   []*simulator.StateHistory
	TimestepsHistory *simulator.CumulativeTimestepsHistory
	Layout           *PartitionLayout
	Config           *gamedata.GameConfig
	MarketGrid       *gamedata.MarketGrid
	NumPlayers       int
}

// Agent is the interface that gameplay policies implement to choose actions.
type Agent interface {
	// ChooseAction selects an action given the current game state.
	// Returns the action as a float64 slice of length ActionStateWidth.
	ChooseAction(ctx *GameContext) []float64
}

// PassAgent always passes. Used for skeleton testing.
type PassAgent struct{}

func (p *PassAgent) ChooseAction(ctx *GameContext) []float64 {
	action := make([]float64, ActionStateWidth)
	action[ActionType] = ActionPass
	return action
}

// ActionIteration reads the current turn state and delegates to an Agent
// to choose the action for this step.
//
// Its output state is the chosen action vector.
type ActionIteration struct {
	Agent         Agent
	TurnPartition int // partition index of the turn controller
	Layout        *PartitionLayout
	Config        *gamedata.GameConfig
	MarketGrid    *gamedata.MarketGrid
	NumPlayers    int
}

func (a *ActionIteration) Configure(partitionIndex int, settings *simulator.Settings) {}

func (a *ActionIteration) Iterate(
	params *simulator.Params,
	partitionIndex int,
	stateHistories []*simulator.StateHistory,
	timestepsHistory *simulator.CumulativeTimestepsHistory,
) []float64 {
	turnState := stateHistories[a.TurnPartition].Values.RawRowView(0)

	ctx := &GameContext{
		TurnState:        turnState,
		StateHistories:   stateHistories,
		TimestepsHistory: timestepsHistory,
		Layout:           a.Layout,
		Config:           a.Config,
		MarketGrid:       a.MarketGrid,
		NumPlayers:       a.NumPlayers,
	}

	return a.Agent.ChooseAction(ctx)
}
