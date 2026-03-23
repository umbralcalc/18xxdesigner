## Project Plan: Board Game Balance Auditor (codename: **Compass**)

### The Core Insight

An 18xx game is a discrete-time, deterministic, multi-agent state-transition system — which is exactly what stochadex's `Iteration` and `Partition` abstractions were designed for. The "stochasticity" enters via the policy layer (unknown opponent intent), which maps cleanly onto your existing decision science framework where you'd normally have environmental noise.

---

### Phase 0: Domain Modelling (1–2 weeks)

Define the state partitions for 1889 specifically. This is where you make the key architectural decisions.

**Partitions:**
- **Bank partition** — cash pool, available trains, available tiles, certificates remaining
- **Market partition** — share price grid positions for each of the 6 companies, par prices
- **Company partitions (×6)** — treasury, trains held, tokens placed, route graph (adjacency list of laid tiles with revenue values)
- **Player partitions (×2–6)** — cash, certificates held (by company), priority deal marker
- **Map partition** — hex grid as a graph; each node holds tile ID + orientation + token slots; edges encode connectivity and terrain cost

The map partition is the trickiest. I'd represent it as a sparse adjacency structure rather than a dense grid, since route-finding performance matters downstream.

**Key decision:** Do you encode the game phase (Stock Round / Operating Round / forced train buy etc.) as a separate partition or as metadata on a shared "phase" partition? I'd lean toward a dedicated **Phase partition** that acts as a finite state machine controlling which `UpdateFunction` gets dispatched.

---

### Phase 1: Deterministic Engine (3–5 weeks)

This is the bulk of the work. You need three things:

**1a. Legal Move Generator**
For each game phase, enumerate all legal actions. This is where 18xx complexity lives:
- Stock Round: buy/sell/pass, constrained by certificate limits, presidency rules, can't-sell-then-buy-same-company
- Tile laying: valid upgrades per hex, orientation constraints, connectivity rules
- Token placement: available slots, home token rules
- Route running: this is the hard one — find revenue-maximising assignment of trains to non-overlapping routes on the current track network. It's a variant of the maximum weight independent set problem on paths in a graph. For 1889's small map, a branch-and-bound or even brute-force enumeration of train-to-route assignments is feasible.

**1b. Update Functions**
Each phase transition becomes an `UpdateFunction` implementation. The state update is fully deterministic given (state, action), so no noise terms — just pure game logic.

**1c. Validation**
Replay at least one known 1889 game log move-by-move and verify your engine produces identical intermediate states. The 18xx community has archived game logs (especially from 18xx.games, the open-source online platform) that you can use as ground truth.

---

### Phase 2: MCTS Policy Layer (2–3 weeks)

This slots into stochadex's existing decision/control framework:

- **Playout policy:** Start with a simple heuristic agent — "buy the cheapest available share that isn't tanking; run the highest-revenue route; withhold dividends if treasury is low." This doesn't need to be good, it just needs to be fast.
- **MCTS proper:** At each decision point, use the legal move generator to expand nodes, run playouts with the heuristic agents to terminal state, backpropagate end-game portfolio value (cash + share value at final market positions).
- **Parallelism:** This is where Go shines. Each playout is an independent stochadex simulation run — you can fan out across goroutines trivially. Root parallelisation (multiple MCTS trees merged) is simplest and works well enough for this.

**Success criterion for this phase:** MCTS agent consistently beats the heuristic agent in head-to-head 4-player games (MCTS + 3 heuristic). If it doesn't, either your engine has a bug or your heuristic is accidentally brilliant.

---

### Phase 3: The Designer Interface (2–3 weeks)

This is where it becomes a *product* rather than a research project.

**3a. Configuration schema**
Define a YAML/JSON spec for game parameters that a designer can tweak. For 1889 this includes train costs, train quantities, tile quantities, par price options, certificate limits, revenue values per city tier, starting capital, etc. The engine reads from this config rather than hardcoding values.

**3b. Comparative simulation runner**
Given two configs (baseline vs. variant), run N simulations of each (say 1,000) and produce a **balance report** containing:
- **Wealth Gini coefficient** at game end (are outcomes equitable or does one seat dominate?)
- **Game length distribution** (did you accidentally make the game 2 hours longer?)
- **Company survival rates** (is one company always dead on arrival?)
- **Hex utilisation heatmap** (which parts of the board are strategically irrelevant?)
- **"Comeback potential"** — variance in lead changes in the second half of the game (a proxy for whether trailing players have agency)
- **Dominant strategy detection** — does the MCTS agent converge on a single opening book?

**3c. Output format**
Start with a CLI tool that writes a Markdown or HTML report. Don't build a GUI yet — the value is in the analysis, not the interface.

---

### Phase 4: Generalisation (future)

Once 1889 works, the question is how much of this generalises. The honest answer: the *framework* generalises beautifully (partitions, update functions, MCTS playout), but the *legal move generator* and *route-finding* are game-specific and need reimplementing for each title. For other 18xx games, maybe 60–70% of the code carries over. For a completely different genre (worker placement, deck building), you'd reuse the simulation and MCTS infrastructure but rewrite the game logic.

The long-term product play would be to build a library of composable game mechanic primitives (auction, route-network, stock-market, resource-conversion) that designers wire together — but that's a Phase 4+ aspiration, not an MVP requirement.

---

### Viability Assessment (my honest take)

**What's strongly in your favour:**
- Stochadex already handles the simulation loop, partitioned state, and concurrent execution — you're not building infrastructure from scratch
- 1889 is small enough to be tractable but complex enough to be impressive
- The 18xx community is passionate, technical, and underserved by tools
- 18xx.games is open source (Ruby) and has existing game logic you can reference for rule validation

**The hard parts:**
- Route-finding with non-overlapping train assignments is genuinely fiddly to implement correctly. Budget extra time here.
- The legal move generator for stock rounds has many edge cases (emergency share issues, forced train purchases triggering bankruptcy, receivership). You'll spend more time on rules lawyering than you expect.
- MCTS with a branching factor in the hundreds needs careful tuning of exploration constants and playout depth limits to converge in reasonable time.

**The risk I'd watch for:**
The biggest risk isn't technical — it's scope. The temptation will be to generalise too early. Keep it locked to 1889 until you have a working balance report that tells you something non-obvious. That's your proof of concept.