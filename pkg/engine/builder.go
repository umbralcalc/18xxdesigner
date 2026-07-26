package engine

import (
	"fmt"
	"math/rand/v2"

	"github.com/umbralcalc/18xxdesigner/pkg/gamedata"
	"github.com/umbralcalc/stochadex/pkg/simulator"
)

// GameBuilder wires all partitions together via the stochadex ConfigGenerator.
type GameBuilder struct {
	Config     *gamedata.GameConfig
	NumPlayers int
	Agent      Agent
	Market     *gamedata.MarketGrid
	Hexes      []gamedata.HexDef
	Seed       int64 // 0 = deterministic; non-zero = randomise private auction
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
		// action is registered before turn: turn reads the action taken this
		// step (within-step), while action reads turn's previous state (lag-1).
		// Ordering producers before consumers is what lets the engine run under
		// simulator.InlineExecution.
		ActionPartition:   0,
		TurnPartition:     1,
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
		Params:            simulator.Params{Map: map[string][]float64{}},
		InitStateValues:   make([]float64, ActionStateWidth),
		StateHistoryDepth: 1,
	})

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
		Params: simulator.Params{Map: map[string][]float64{}},
		ParamsFromUpstream: map[string]simulator.NamedUpstreamConfig{
			"action_values": {Upstream: PartAction},
		},
		InitStateValues:   turnInit,
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
	playerInits := b.initPlayerStates()
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
			InitStateValues:   playerInits[i],
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

	settings, implementations := gen.GenerateConfigs()
	// Partitions are registered producers-first, so a step can be run straight
	// through without the per-step goroutine round-trip the default strategy
	// pays. InlineExecution panics if that ordering is ever broken, so this
	// cannot silently start reading stale upstream values.
	implementations.ExecutionStrategy = &simulator.InlineExecution{}
	return settings, implementations
}

// initPlayerStates returns per-player init state vectors.
// When Seed is 0, all players get identical starting cash.
// When Seed is non-zero, privates are randomly distributed among players
// at face value, creating asymmetric starting positions.
func (b *GameBuilder) initPlayerStates() [][]float64 {
	states := make([][]float64, b.NumPlayers)
	for i := range states {
		states[i] = InitPlayerState(b.Config, b.NumPlayers)
	}

	if b.Seed == 0 {
		return states
	}

	rng := rand.New(rand.NewPCG(uint64(b.Seed), 0))
	privates := b.Config.PrivatesForPlayerCount(b.NumPlayers)

	// Shuffle assignment order.
	perm := rng.Perm(len(privates))
	for i, idx := range perm {
		player := i % b.NumPlayers
		priv := privates[idx]
		states[player][PlayerCash] -= float64(priv.Value)
		states[player][PlayerPrivateIdx(priv.ID)] = 1.0
	}

	return states
}

// BuildAndRun creates the full simulation and runs it to completion.
func (b *GameBuilder) BuildAndRun() {
	settings, implementations := b.Build()
	coordinator := simulator.NewPartitionCoordinator(settings, implementations)
	coordinator.Run()
}
