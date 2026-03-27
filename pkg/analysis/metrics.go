package analysis

import (
	"math"
	"sort"
)

// BatchMetrics holds aggregate statistics computed from a batch of simulation results.
type BatchMetrics struct {
	NumGames   int
	NumPlayers int

	// Game length.
	GameLengthMean   float64
	GameLengthStdDev float64
	GameLengthMin    int
	GameLengthMax    int

	// Player wealth distribution.
	GiniCoefficient float64 // 0 = perfectly equal, 1 = one player has everything
	WinsByPlayer    []int   // how many times each player index won

	// Portfolio statistics.
	PortfolioMean   []float64 // mean portfolio per player position
	PortfolioStdDev []float64

	// Company statistics.
	CompanyFloatRate    []float64 // fraction of games where company floated
	CompanySurvivalRate []float64 // fraction of games where company survived to end
	CompanyMeanRevenue  []float64 // mean last revenue across games

	// Hex utilization: fraction of games where each hex had a tile placed.
	HexUtilization []float64

	// Comeback potential: fraction of games where the initial-round
	// leader (highest portfolio at step ~30) did NOT win.
	ComebackRate float64
}

// ComputeMetrics calculates aggregate metrics from a batch of results.
func ComputeMetrics(results []RunResult, numPlayers, numCompanies, numHexes int) BatchMetrics {
	n := len(results)
	if n == 0 {
		return BatchMetrics{}
	}

	m := BatchMetrics{
		NumGames:            n,
		NumPlayers:          numPlayers,
		WinsByPlayer:        make([]int, numPlayers),
		PortfolioMean:       make([]float64, numPlayers),
		PortfolioStdDev:     make([]float64, numPlayers),
		CompanyFloatRate:    make([]float64, numCompanies),
		CompanySurvivalRate: make([]float64, numCompanies),
		CompanyMeanRevenue:  make([]float64, numCompanies),
		HexUtilization:      make([]float64, numHexes),
		GameLengthMin:       math.MaxInt64,
	}

	// Game length.
	lengths := make([]float64, n)
	for i, r := range results {
		lengths[i] = float64(r.Steps)
		if r.Steps < m.GameLengthMin {
			m.GameLengthMin = r.Steps
		}
		if r.Steps > m.GameLengthMax {
			m.GameLengthMax = r.Steps
		}
	}
	m.GameLengthMean, m.GameLengthStdDev = meanStdDev(lengths)

	// Player portfolios and wins.
	allPortfolios := make([]float64, 0, n*numPlayers)
	for _, r := range results {
		m.WinsByPlayer[r.WinnerIndex]++
		for p := 0; p < numPlayers; p++ {
			if p < len(r.PortfolioValues) {
				m.PortfolioMean[p] += r.PortfolioValues[p]
				allPortfolios = append(allPortfolios, r.PortfolioValues[p])
			}
		}
	}
	for p := range m.PortfolioMean {
		m.PortfolioMean[p] /= float64(n)
	}

	// Portfolio stddev.
	for _, r := range results {
		for p := 0; p < numPlayers; p++ {
			if p < len(r.PortfolioValues) {
				d := r.PortfolioValues[p] - m.PortfolioMean[p]
				m.PortfolioStdDev[p] += d * d
			}
		}
	}
	for p := range m.PortfolioStdDev {
		m.PortfolioStdDev[p] = math.Sqrt(m.PortfolioStdDev[p] / float64(n))
	}

	// Gini coefficient across all player portfolios.
	m.GiniCoefficient = gini(allPortfolios)

	// Company stats.
	for _, r := range results {
		for c := 0; c < numCompanies; c++ {
			if c < len(r.CompanyFloated) && r.CompanyFloated[c] {
				m.CompanyFloatRate[c]++
			}
			if c < len(r.CompanySurvived) && r.CompanySurvived[c] {
				m.CompanySurvivalRate[c]++
			}
			if c < len(r.CompanyRevenue) {
				m.CompanyMeanRevenue[c] += r.CompanyRevenue[c]
			}
		}
	}
	for c := range m.CompanyFloatRate {
		m.CompanyFloatRate[c] /= float64(n)
		m.CompanySurvivalRate[c] /= float64(n)
		m.CompanyMeanRevenue[c] /= float64(n)
	}

	// Hex utilization.
	for _, r := range results {
		for h := 0; h < numHexes; h++ {
			if h < len(r.HexTileIDs) && r.HexTileIDs[h] >= 0 {
				m.HexUtilization[h]++
			}
		}
	}
	for h := range m.HexUtilization {
		m.HexUtilization[h] /= float64(n)
	}

	// Comeback rate: fraction where winner differs from early leader.
	// We approximate "early leader" as the player with highest portfolio
	// value in the result (since we don't have mid-game snapshots,
	// we use winner vs. seat position advantage).
	// For a proper implementation we'd need to track mid-game state.
	// For now: measure how often player 0 (first mover) does NOT win.
	firstMoverWins := 0
	for _, r := range results {
		if r.WinnerIndex == 0 {
			firstMoverWins++
		}
	}
	m.ComebackRate = 1.0 - float64(firstMoverWins)/float64(n)

	return m
}

// gini computes the Gini coefficient for a set of values.
// Returns 0 for perfectly equal, approaching 1 for maximum inequality.
func gini(values []float64) float64 {
	n := len(values)
	if n <= 1 {
		return 0
	}

	sorted := make([]float64, n)
	copy(sorted, values)
	sort.Float64s(sorted)

	sumOfAbsDiffs := 0.0
	sumOfValues := 0.0
	for _, xi := range sorted {
		sumOfValues += xi
		for j, xj := range sorted {
			_ = j
			sumOfAbsDiffs += math.Abs(xi - xj)
		}
	}

	if sumOfValues == 0 {
		return 0
	}
	return sumOfAbsDiffs / (2.0 * float64(n) * sumOfValues)
}

// meanStdDev computes mean and sample standard deviation.
func meanStdDev(xs []float64) (float64, float64) {
	n := float64(len(xs))
	if n == 0 {
		return 0, 0
	}
	sum := 0.0
	for _, x := range xs {
		sum += x
	}
	mean := sum / n

	variance := 0.0
	for _, x := range xs {
		d := x - mean
		variance += d * d
	}
	return mean, math.Sqrt(variance / n)
}
