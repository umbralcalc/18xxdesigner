package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/umbralcalc/18xxdesigner/pkg/analysis"
	"github.com/umbralcalc/18xxdesigner/pkg/engine"
	"github.com/umbralcalc/18xxdesigner/pkg/gamedata"
	"github.com/umbralcalc/18xxdesigner/pkg/policy"
	"github.com/umbralcalc/18xxdesigner/pkg/replay"
	"github.com/umbralcalc/stochadex/pkg/simulator"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "run":
		cmdRun(os.Args[2:])
	case "compare":
		cmdCompare(os.Args[2:])
	case "replay":
		cmdReplay(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "Usage: 18xxdesigner <command> [flags]")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Commands:")
	fmt.Fprintln(os.Stderr, "  run      Run batch simulations and produce a balance report")
	fmt.Fprintln(os.Stderr, "  compare  Compare two configurations side by side")
	fmt.Fprintln(os.Stderr, "  replay   Replay a game transcript")
}

func cmdRun(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	players := fs.Int("players", 4, "number of players (2-6)")
	sims := fs.Int("sims", 100, "number of simulations")
	maxSteps := fs.Int("max-steps", 5000, "maximum steps per game")
	useMCTS := fs.Bool("mcts", false, "use MCTS agent for player 0")
	mctsPlayouts := fs.Int("mcts-playouts", 5, "MCTS playouts per decision")
	output := fs.String("output", "", "output file (default: stdout)")
	fs.Parse(args)

	cfg := analysis.RunConfig{
		NumGames:       *sims,
		NumPlayers:     *players,
		MaxSteps:       *maxSteps,
		MCTSPlayouts:   *mctsPlayouts,
		MCTSMaxPlayout: 1000,
	}
	if *useMCTS {
		cfg.Agent = analysis.AgentMCTS
	}

	fmt.Fprintf(os.Stderr, "Running %d simulations (%d players)...\n", cfg.NumGames, cfg.NumPlayers)
	results := analysis.RunBatch(cfg)

	gameCfg := gamedata.Default1889Config()
	hexes := gamedata.Default1889Map()
	metrics := analysis.ComputeMetrics(results, cfg.NumPlayers,
		len(gameCfg.Companies), len(hexes))

	w := os.Stdout
	if *output != "" {
		f, err := os.Create(*output)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		defer f.Close()
		w = f
	}

	analysis.WriteReport(w, metrics, gameCfg, hexes)
	fmt.Fprintf(os.Stderr, "Done. %d games completed.\n", len(results))
}

func cmdCompare(args []string) {
	fs := flag.NewFlagSet("compare", flag.ExitOnError)
	players := fs.Int("players", 4, "number of players")
	sims := fs.Int("sims", 50, "simulations per config")
	maxSteps := fs.Int("max-steps", 5000, "maximum steps per game")
	configPath := fs.String("variant", "", "path to variant YAML config")
	output := fs.String("output", "", "output file (default: stdout)")
	fs.Parse(args)

	baseCfg := analysis.RunConfig{
		NumGames:   *sims,
		NumPlayers: *players,
		MaxSteps:   *maxSteps,
	}

	fmt.Fprintf(os.Stderr, "Running baseline (%d sims)...\n", *sims)
	baseResults := analysis.RunBatch(baseCfg)

	gameCfg := gamedata.Default1889Config()
	hexes := gamedata.Default1889Map()
	numCompanies := len(gameCfg.Companies)

	baseMetrics := analysis.ComputeMetrics(baseResults, *players, numCompanies, len(hexes))

	// Variant: if a config path is provided, load it; otherwise just re-run baseline.
	variantLabel := "variant"
	var variantMetrics analysis.BatchMetrics
	if *configPath != "" {
		variantCfg, err := gamedata.LoadConfig(*configPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error loading variant config: %v\n", err)
			os.Exit(1)
		}
		variantLabel = *configPath
		fmt.Fprintf(os.Stderr, "Running variant %s (%d sims)...\n", variantLabel, *sims)
		variantResults := analysis.RunBatchWithConfig(baseCfg, variantCfg)
		variantMetrics = analysis.ComputeMetrics(variantResults, *players,
			len(variantCfg.Companies), len(hexes))
	} else {
		fmt.Fprintln(os.Stderr, "No --variant specified; comparing baseline against itself.")
		variantMetrics = baseMetrics
	}

	w := os.Stdout
	if *output != "" {
		f, err := os.Create(*output)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		defer f.Close()
		w = f
	}

	analysis.WriteComparisonReport(w, "baseline", baseMetrics, variantLabel, variantMetrics, gameCfg)
}

func cmdReplay(args []string) {
	fs := flag.NewFlagSet("replay", flag.ExitOnError)
	transcript := fs.String("transcript", "", "path to transcript log file")
	fs.Parse(args)

	if *transcript == "" {
		fmt.Fprintln(os.Stderr, "error: --transcript is required")
		os.Exit(1)
	}

	events, err := replay.ParseTranscript(*transcript)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error parsing transcript: %v\n", err)
		os.Exit(1)
	}

	playerNames := replay.ExtractPlayerNames(events)
	numPlayers := len(playerNames)
	fmt.Fprintf(os.Stderr, "Players: %v\n", playerNames)

	config := engine.NewGameBuilder(numPlayers, nil).Config
	agent := replay.NewReplayAgent(events, config, playerNames)
	agent.SkipToStockRound()

	builder := engine.NewGameBuilder(numPlayers, agent)
	settings, impls := builder.Build()
	layout := builder.Layout()

	impls.TerminationCondition = &engine.OrTerminationCondition{
		Conditions: []simulator.TerminationCondition{
			&engine.BankBrokenTerminationCondition{
				BankPartitionIndex: layout.BankPartition,
			},
			&simulator.NumberOfStepsTerminationCondition{
				MaxNumberOfSteps: 5000,
			},
		},
	}

	coordinator := simulator.NewPartitionCoordinator(settings, impls)
	coordinator.Run()

	steps := coordinator.Shared.TimestepsHistory.CurrentStepNumber
	bankState := coordinator.Shared.StateHistories[layout.BankPartition].Values.RawRowView(0)

	fmt.Printf("Game ended after %d steps, bank cash: %.0f\n", steps, bankState[engine.BankCash])
	fmt.Printf("Transcript events consumed: %d/%d\n", agent.Cursor(), len(events))

	// Print final player state.
	numCompanies := len(builder.Config.Companies)
	for i, name := range playerNames {
		val := policy.PortfolioValue(
			coordinator.Shared.StateHistories, layout, i, builder.Market, numCompanies)
		ps := coordinator.Shared.StateHistories[layout.PlayerPartitions[i]].Values.RawRowView(0)
		fmt.Printf("Player %s: cash=%.0f portfolio=%.0f\n", name, ps[engine.PlayerCash], val)
	}

	// Print errors.
	errorCount := 0
	for _, step := range agent.Log {
		if step.Error != "" {
			errorCount++
			if errorCount <= 20 {
				fmt.Printf("MISMATCH step %d: %s\n", step.Step, step.Error)
			}
		}
	}
	if errorCount > 0 {
		fmt.Printf("Total mismatches: %d\n", errorCount)
	}
}
