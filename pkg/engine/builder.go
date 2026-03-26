package engine

import (
	"fmt"

	"github.com/umbralcalc/stochadex/pkg/simulator"
	"github.com/umbralcalc/18xxdesigner/pkg/gamedata"
)

// GameBuilder wires all partitions together via the stochadex ConfigGenerator.
type GameBuilder struct {
	Config     *gamedata.GameConfig
	NumPlayers int
	Agent      Agent
	Market     *gamedata.MarketGrid
	Hexes      []gamedata.HexDef
}

// NewGameBuilder creates a builder with default 1889 configuration.
func NewGameBuilder(numPlayers int, agent Agent) *GameBuilder {
	return &GameBuilder{
		Config:     gamedata.Default1889Config(),
		NumPlayers: numPlayers,
		Agent:      agent,
		Market:     gamedata.Default1889Market(),
		Hexes:      gamedata.Default1889Map(),
	}
}

// Partition names.
const (
	PartTurn   = "turn"
	PartAction = "action"
	PartBank   = "bank"
	PartMarket = "market"
	PartMap    = "map"
)

func companyPartName(i int) string { return fmt.Sprintf("company_%d", i) }
func playerPartName(i int) string  { return fmt.Sprintf("player_%d", i) }

// Layout returns the partition index layout for the game.
func (b *GameBuilder) Layout() *PartitionLayout {
	numCompanies := len(b.Config.Companies)
	layout := &PartitionLayout{
		TurnPartition:     0,
		ActionPartition:   1,
		BankPartition:     2,
		MarketPartition:   3,
		MapPartition:      4,
		CompanyPartitions: make([]int, numCompanies),
		PlayerPartitions:  make([]int, b.NumPlayers),
	}
	for i := 0; i < numCompanies; i++ {
		layout.CompanyPartitions[i] = 5 + i
	}
	for i := 0; i < b.NumPlayers; i++ {
		layout.PlayerPartitions[i] = 5 + numCompanies + i
	}
	return layout
}

// Build generates the stochadex Settings and Implementations for a full game.
func (b *GameBuilder) Build() (*simulator.Settings, *simulator.Implementations) {
	gen := simulator.NewConfigGenerator()

	numCompanies := len(b.Config.Companies)

	// ORs per phase from config.
	orsPerPhase := make([]int, len(b.Config.Phases))
	for i, p := range b.Config.Phases {
		orsPerPhase[i] = p.ORsPerSR
	}

	// --- Turn partition ---
	turnInit := make([]float64, TurnStateWidth)
	turnInit[TurnRoundType] = RoundPrivateAuction
	turnInit[TurnActiveType] = ActivePlayer
	turnInit[TurnActiveID] = 0

	gen.SetPartition(&simulator.PartitionConfig{
		Name: PartTurn,
		Iteration: &TurnControllerIteration{
			NumPlayers:   b.NumPlayers,
			NumCompanies: numCompanies,
			ORsPerPhase:  orsPerPhase,
		},
		Params:          simulator.Params{Map: map[string][]float64{}},
		ParamsFromUpstream: map[string]simulator.NamedUpstreamConfig{
			"action_values": {Upstream: PartAction},
		},
		InitStateValues:   turnInit,
		StateHistoryDepth: 1,
	})

	layout := b.Layout()

	// --- Action partition ---
	gen.SetPartition(&simulator.PartitionConfig{
		Name: PartAction,
		Iteration: &ActionIteration{
			Agent:         b.Agent,
			TurnPartition: layout.TurnPartition,
			Layout:        layout,
			Config:        b.Config,
			MarketGrid:    b.Market,
			NumPlayers:    b.NumPlayers,
		},
		Params:          simulator.Params{Map: map[string][]float64{}},
		InitStateValues: make([]float64, ActionStateWidth),
		StateHistoryDepth: 1,
	})

	// --- Bank partition ---
	gen.SetPartition(&simulator.PartitionConfig{
		Name:      PartBank,
		Iteration: &BankIteration{Config: b.Config},
		Params:    simulator.Params{Map: map[string][]float64{}},
		ParamsFromUpstream: map[string]simulator.NamedUpstreamConfig{
			"action_values": {Upstream: PartAction},
		},
		InitStateValues:   InitBankState(b.Config),
		StateHistoryDepth: 1,
	})

	// --- Market partition ---
	gen.SetPartition(&simulator.PartitionConfig{
		Name: PartMarket,
		Iteration: &MarketIteration{
			Config: b.Config,
			Grid:   b.Market,
		},
		Params: simulator.Params{Map: map[string][]float64{}},
		ParamsFromUpstream: map[string]simulator.NamedUpstreamConfig{
			"action_values": {Upstream: PartAction},
		},
		InitStateValues:   InitMarketState(b.Config),
		StateHistoryDepth: 1,
	})

	// --- Map partition ---
	gen.SetPartition(&simulator.PartitionConfig{
		Name: PartMap,
		Iteration: &MapIteration{
			Config: b.Config,
			Hexes:  b.Hexes,
		},
		Params: simulator.Params{Map: map[string][]float64{}},
		ParamsFromUpstream: map[string]simulator.NamedUpstreamConfig{
			"action_values": {Upstream: PartAction},
		},
		InitStateValues:   InitMapState(b.Hexes),
		StateHistoryDepth: 1,
	})

	// --- Company partitions ---
	for i := 0; i < numCompanies; i++ {
		gen.SetPartition(&simulator.PartitionConfig{
			Name: companyPartName(i),
			Iteration: &CompanyIteration{
				CompanyIndex: i,
				Config:       b.Config,
			},
			Params: simulator.Params{Map: map[string][]float64{}},
			ParamsFromUpstream: map[string]simulator.NamedUpstreamConfig{
				"action_values": {Upstream: PartAction},
				"turn_values":   {Upstream: PartTurn},
				"bank_values":   {Upstream: PartBank},
			},
			InitStateValues:   InitCompanyState(),
			StateHistoryDepth: 1,
		})
	}

	// --- Player partitions ---
	for i := 0; i < b.NumPlayers; i++ {
		gen.SetPartition(&simulator.PartitionConfig{
			Name: playerPartName(i),
			Iteration: &PlayerIteration{
				PlayerIndex: i,
				Config:      b.Config,
			},
			Params: simulator.Params{Map: map[string][]float64{}},
			ParamsFromUpstream: map[string]simulator.NamedUpstreamConfig{
				"action_values": {Upstream: PartAction},
				"turn_values":   {Upstream: PartTurn},
				"bank_values":   {Upstream: PartBank},
			},
			InitStateValues:   InitPlayerState(b.Config, b.NumPlayers),
			StateHistoryDepth: 1,
		})
	}

	// --- Simulation config ---
	gen.SetSimulation(&simulator.SimulationConfig{
		OutputCondition:      &simulator.NilOutputCondition{},
		OutputFunction:       &simulator.NilOutputFunction{},
		TerminationCondition: &simulator.NumberOfStepsTerminationCondition{MaxNumberOfSteps: 1000},
		TimestepFunction:     &simulator.ConstantTimestepFunction{Stepsize: 1.0},
		InitTimeValue:        0.0,
	})

	return gen.GenerateConfigs()
}

// BuildAndRun creates the full simulation and runs it to completion.
func (b *GameBuilder) BuildAndRun() {
	settings, implementations := b.Build()
	coordinator := simulator.NewPartitionCoordinator(settings, implementations)
	coordinator.Run()
}
