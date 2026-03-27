# 18xxdesigner: 1889 Board Game Balance Auditor

A simulation-based balance auditor for the 18xx train game **1889** (Shikoku), built on the [stochadex](https://github.com/umbralcalc/stochadex) SDK. The goal is to model the full game as a set of stochadex partitions, run thousands of games with AI agents, and produce balance reports that tell a game designer whether rule/parameter changes improve or degrade the game.

---

## Core Architecture Decision

**One simulation step = one game action.** The stochadex coordinator runs all partitions in parallel each tick, but 1889 is strictly sequential. We reconcile this by using `params_from_upstream` wiring to create a dependency chain: `turn → action → all state partitions`. The coordinator's channel-based blocking ensures sequential execution within each step. Parallelism benefits come at the MCTS layer (many independent game simulations).

---

## Package Structure

```
pkg/
  gamedata/        # Pure data types, constants, YAML config loader. No simulator dep.
    companies.go   # CompanyDef, PrivateDef, TrainDef structs + 1889 defaults
    map.go         # HexDef, adjacency graph, terrain/city types
    tiles.go       # TileDef, track segments, upgrade paths, tile manifest
    market.go      # MarketCell, stock market grid, movement functions
    config.go      # GameConfig struct, YAML loader, validation
  engine/          # Iteration implementations — the game engine
    turn.go        # TurnControllerIteration — master FSM dispatcher
    bank.go        # BankIteration — cash pool, train/tile/cert availability
    market.go      # MarketIteration — share price positions
    company.go     # CompanyIteration — per-company treasury/trains/tokens
    player.go      # PlayerIteration — per-player cash/shares/privates
    mapstate.go    # MapIteration — hex grid tile/token placements
    action.go      # ActionIteration — action injection via agent
    route.go       # Route-finding: track graph, enumeration, optimal assignment
    legal.go       # Legal move generation (pure functions)
    builder.go     # Wires partitions via ConfigGenerator → Settings+Implementations
    game_test.go   # Full integration test replaying a known 1889 game
  policy/          # AI agents
    agent.go       # Agent interface
    heuristic.go   # Rule-based fast playout agent
    mcts.go        # MCTS tree search using parallel game simulations
  analysis/        # Balance metrics and reporting
    metrics.go     # Gini, game length, company survival, heatmap, comeback
    runner.go      # Comparative simulation runner (baseline vs variant)
    report.go      # Markdown report generator
cmd/
  18xxdesigner/
    main.go        # CLI: run, compare, replay subcommands
```

---

## Partition Layout (4-player game example)

| Partition | StateWidth | Key state indices |
|-----------|-----------|-------------------|
| `turn` | 8 | game_phase, phase_number, round_type, or_number, active_entity_type, active_entity_id, action_type, action_step |
| `action` | 20 | action_type + up to 19 args (meaning varies by action type) |
| `bank` | ~30 | cash, trains_available[6 types], train_phase, tiles_available[~22 types] |
| `market` | 14 | 7 companies x (row, col) on stock grid |
| `map` | 72 | 24 hexes x (tile_id, orientation, token_bitfield) |
| `company_0`..`company_6` | 16 each | treasury, floated, trains_held[6], tokens_remaining, par, president, shares_ipo, shares_market, last_revenue, receivership |
| `player_0`..`player_3` | 18 each | cash, shares_held[7 companies], privates_held[8], priority_deal, cert_count |

**Upstream wiring:** `turn → action → {bank, market, map, company_*, player_*}`

---

## Implementation Steps

### Step 1: Game Data Layer (`pkg/gamedata/`)
- Define all types: `HexDef`, `TileDef`, `CompanyDef`, `PrivateDef`, `TrainDef`, `MarketCell`, `GameConfig`
- Hardcode 1889 defaults (reference: 18xx.games Ruby source for exact values)
- YAML loader + validation for `GameConfig`
- **Test:** config loads, counts match (7 companies, 8 privates, 6 train types, ~24 hexes)
- **No stochadex dependency needed**

### Step 2: Turn Controller + Skeleton Loop
- `TurnControllerIteration`: FSM that advances through Private Auction → SR → OR(s) → SR → ...
- `ActionIteration`: wraps an `Agent` interface, calls `agent.ChooseAction()`
- `BankIteration`: tracks cash and availability
- `builder.go`: wires partitions via `ConfigGenerator`
- Trivial "always pass" agent
- **Test:** 10 steps of SR with all-pass, turn controller advances correctly

### Step 3: Stock Round
- `PlayerIteration` + `MarketIteration` with buy/sell/pass logic
- `LegalStockRoundActions()` — certificate limits, presidency rules, can't-sell-then-buy-same
- Heuristic agent makes simple buy decisions
- **Test:** full SR cycle, verify cert limits, priority deal, price movement

### Step 4: Map + Tile Laying
- `MapIteration` with tile placement logic
- `LegalTileLayActions()` — valid upgrades, orientation, connectivity, terrain costs
- **Test:** tile placement rules verified against known valid/invalid placements

### Step 5: Route Finding + Revenue (hardest algorithmic piece)
- Build track graph from map state (nodes = hex edges/cities, edges = tile segments)
- Enumerate valid routes per train (N stops, must start from company token)
- Optimal non-overlapping assignment via backtracking with pruning
- **Test:** hand-crafted map states with known optimal revenues

### Step 6: Full Operating Round
- `CompanyIteration` with all OR sub-steps: tile → token → routes → dividends → trains
- Dividend/withhold stock price movement
- Token placement logic
- **Test:** complete SR → OR → SR cycle for one company

### Step 7: Phase Transitions + Train Rusting
- Phase advances when new train types purchased (4-train → phase 3, 5-train → phase 5)
- Train rusting (2-trains rust on 4-train purchase, 3-trains on 6-train)
- Private company closure at phase 5
- OR count increases by phase: [1, 1, 2, 2, 3]
- Custom `BankBrokenTerminationCondition`
- **Test:** full game runs to bank-break with heuristic agents

### Step 8: Game Validation
- Parse a known 1889 game log (from 18xx.games JSON export)
- Replay move-by-move, compare intermediate states
- Fix rule bugs until replay matches
- **Test:** at least one complete game replay produces identical states

### Step 9: Heuristic Agent (`pkg/policy/heuristic.go`)
- SR: buy cheapest share of highest-revenue company; sell tanking companies; pass if nothing good
- Tile: extend routes from own tokens
- Routes: always use `OptimalRouteAssignment` (deterministic)
- Dividends: pay if revenue > 2x next train cost needed; else withhold
- Trains: buy cheapest that covers best route
- **Test:** 100 games terminate in 150-300 actions without panics

### Step 10: MCTS Agent (`pkg/policy/mcts.go`)
- At each decision: enumerate legal moves, run N playout simulations per candidate
- Each playout = full game simulation via `NewPartitionCoordinator` + `Run()` with heuristic agents
- UCB1 tree policy, root parallelization across goroutines
- Score = final portfolio value (cash + share holdings at market price)
- **Test:** MCTS beats heuristic in head-to-head 4-player games

### Step 11: Analysis + CLI
- `analysis/metrics.go`: Gini coefficient, game length distribution, company float/survival rates, hex utilization, comeback potential, opening convergence
- `analysis/runner.go`: run N simulations, collect `RunResult` structs
- `analysis/report.go`: generate Markdown balance report
- `cmd/18xxdesigner/main.go`: `run`, `compare`, `replay` subcommands
- **Test:** `18xxdesigner run --config 1889.yaml --players 4 --sims 100` produces valid report

---

## Key Technical Risks

1. **Route finding complexity** — Mitigated by 1889's small map (~24 hexes). Cache track graph, rebuild only on map changes. Backtracking with pruning is sufficient for ≤4 trains.
2. **Legal move generator edge cases** — Stock round has many rules (emergency share issues, forced train purchase, receivership, bankruptcy). Budget extra time. Use 18xx.games Ruby source as reference.
3. **MCTS branching factor** — Hundreds of legal moves in some states. Mitigate with: progressive widening, time budgets per move, heuristic move ordering to try promising moves first.
4. **Harness compatibility** — Every iteration must pass `RunWithHarnesses` (no NaN, no params mutation, correct state width, deterministic). Use `params.GetCopy()` defensively.

---

## Build & Run

```bash
go build ./...                                    # compile
go test -count=1 ./...                            # run all tests
go run github.com/umbralcalc/stochadex/cmd/stochadex --config cfg/builtin_example.yaml
```
