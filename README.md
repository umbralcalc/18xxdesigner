# 18xxdesigner

A simulation-based balance auditor for the 18xx train game **1889: History of Shikoku Railways**, built on the [stochadex](https://github.com/umbralcalc/stochadex) simulation SDK.

Run thousands of games with AI agents, then inspect Markdown reports covering wealth distribution, company viability, map utilisation, and more. Tweak game parameters in YAML and compare variants side-by-side to see what changes actually do to balance.

---

## Quick Start

```bash
go build ./...
go run ./cmd/18xxdesigner run --players 4 --sims 100
```

This runs 100 four-player games with heuristic agents and prints a balance report to stdout.

---

## CLI

### `run` — batch simulation

```bash
go run ./cmd/18xxdesigner run [flags]
```

| Flag | Default | Description |
|------|---------|-------------|
| `-players` | 4 | Number of players (2–6) |
| `-sims` | 100 | Number of games to simulate |
| `-max-steps` | 5000 | Step limit per game |
| `-mcts` | false | Use MCTS agent for player 0 |
| `-mcts-playouts` | 5 | Playouts per MCTS decision |
| `-output` | stdout | Write report to file |

### `compare` — variant comparison

```bash
go run ./cmd/18xxdesigner compare --variant tweaked.yaml [flags]
```

Runs the baseline 1889 config and a YAML variant, then produces a side-by-side report with deltas for game length, Gini coefficient, comeback rate, and per-company float rates.

| Flag | Default | Description |
|------|---------|-------------|
| `-players` | 4 | Number of players |
| `-sims` | 50 | Simulations per config |
| `-max-steps` | 5000 | Step limit per game |
| `-variant` | | Path to variant YAML config |
| `-output` | stdout | Write report to file |

### `replay` — transcript playback

```bash
go run ./cmd/18xxdesigner replay --transcript game.log
```

Replays an [18xx.games](https://18xx.games) JSON game log move-by-move through the engine, printing final state and any rule mismatches.

---

## Report Metrics

Reports include:

- **Game length** — mean, std dev, min, max steps
- **Gini coefficient** — wealth inequality across players (0 = equal, 1 = monopoly)
- **Win distribution** — per-player win count and win rate
- **Portfolio values** — mean and std dev of final cash + share holdings per player
- **Comeback rate** — % of games where the first-mover did not win
- **Company statistics** — float rate, survival rate, mean revenue per company
- **Hex utilisation** — % of games each hex received a tile upgrade

---

## Variant Configs

The `cfg/` directory contains example variants you can use with `compare`:

| Config | What it tweaks | Expected effect |
|--------|---------------|-----------------|
| `cfg/baseline.yaml` | Nothing — reference copy of default 1889 | Use as a template for new variants |
| `cfg/cheap_trains.yaml` | All train prices reduced ~25% | Faster phase transitions, shorter games, less punishment for late companies |
| `cfg/rich_start.yaml` | +50% starting cash, bank 10000, higher cert limits | More companies float, bigger portfolios |
| `cfg/slow_rust.yaml` | Trains rust one generation later (2→5, 3→D, 4→never) | Longer useful train life, less receivership |

Try one out:

```bash
go run ./cmd/18xxdesigner compare --variant cfg/cheap_trains.yaml --sims 50
go run ./cmd/18xxdesigner compare --variant cfg/slow_rust.yaml --sims 100 --output report.md
```

To create your own variant, copy `cfg/baseline.yaml` and change the values you want to test. The config must be a complete `GameConfig` — see the baseline for all required fields.

---

## Architecture

The game engine is built as a set of [stochadex](https://github.com/umbralcalc/stochadex) partitions. One simulation step = one game action. Partitions are wired via `params_from_upstream` into a dependency chain:

```
turn → action → { bank, market, map, company_0..6, player_0..N }
```

The stochadex coordinator's channel-based blocking ensures sequential execution within each step. Parallelism comes at the MCTS layer, where many independent game simulations run concurrently.

### Partition layout (4-player game)

| Partition | StateWidth | Contents |
|-----------|-----------|----------|
| `turn` | 8 | FSM: game phase, round type, active entity, action type |
| `action` | 20 | Action vector from AI agent |
| `bank` | ~30 | Cash pool, train/tile availability, phase tracking |
| `market` | 14 | 7 companies × (row, col) on stock price grid |
| `map` | 72 | 24 hexes × (tile ID, orientation, token bitfield) |
| `company_0`..`6` | 16 each | Treasury, trains, tokens, par, president, revenue |
| `player_0`..`3` | 18 each | Cash, shares held, privates, priority deal, cert count |

### Packages

```
pkg/
  gamedata/    Pure data: company/train/tile/market definitions, YAML config loader
  engine/      Game engine: turn FSM, state partitions, legal moves, route finding
  policy/      AI agents: heuristic (fast) and MCTS (strong)
  analysis/    Batch runner, balance metrics, Markdown report generation
  replay/      Transcript parser and replay agent for 18xx.games logs
cmd/
  18xxdesigner/  CLI entry point
```

---

## AI Agents

### Heuristic

Fast rule-based agent used for bulk simulation and MCTS playouts:

- **Stock round:** buy cheapest share of highest-revenue company; sell tanking companies
- **Tile lay:** extend routes from own tokens
- **Routes:** optimal non-overlapping assignment via backtracking
- **Dividends:** pay out if revenue covers upcoming train costs; otherwise withhold
- **Train purchase:** buy cheapest train that covers best route

### MCTS

Monte Carlo Tree Search agent for stronger play. At each decision point:

1. Enumerate legal moves
2. For each candidate, run N full-game playouts (heuristic agents fill other seats)
3. Score by final portfolio value (cash + shares × market price)
4. Select move with highest mean score (UCB1 exploration)

Built as stochadex partitions following the evolution strategies optimiser pattern: action selector → embedded playout simulation → statistics accumulation.

---

## Build & Test

```bash
go build ./...               # compile all packages
go test -count=1 ./...       # run full test suite (~20s)
go test -count=1 -short ./...  # skip long-running benchmarks
```

---

## License

See [LICENSE](LICENSE).
