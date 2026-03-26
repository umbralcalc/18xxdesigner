package replay

import (
	"testing"
)

func TestParseTranscript247490(t *testing.T) {
	events, err := ParseTranscript("../../dat/transcript_247490.log")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if len(events) == 0 {
		t.Fatal("no events parsed")
	}

	// Count event types.
	counts := make(map[EventType]int)
	ignored := 0
	for _, ev := range events {
		counts[ev.Type]++
		if ev.Type == EventIgnored {
			ignored++
			if testing.Verbose() {
				t.Logf("IGNORED line %d: %s", ev.Line, ev.Raw)
			}
		}
	}

	t.Logf("Total events: %d", len(events))
	t.Logf("Ignored: %d", ignored)
	t.Logf("Phase headers: %d", counts[EventPhaseHeader])
	t.Logf("Stock rounds: %d", counts[EventStockRoundHeader])
	t.Logf("Operating rounds: %d", counts[EventOperatingRoundHeader])
	t.Logf("Pars: %d", counts[EventPar])
	t.Logf("Buy share IPO: %d", counts[EventBuyShareIPO])
	t.Logf("Sell shares: %d", counts[EventSellShares])
	t.Logf("Tile lays: %d", counts[EventTileLay])
	t.Logf("Place tokens: %d", counts[EventPlaceToken])
	t.Logf("Run routes: %d", counts[EventRunRoute])
	t.Logf("Pay dividends: %d", counts[EventPayDividends])
	t.Logf("Withhold: %d", counts[EventWithhold])
	t.Logf("Buy train depot: %d", counts[EventBuyTrainDepot])
	t.Logf("Buy train company: %d", counts[EventBuyTrainCompany])
	t.Logf("Game over: %d", counts[EventGameOver])
	t.Logf("Bank broken: %d", counts[EventBankBroken])

	// Verify key facts about this game.
	// First event should be a phase header.
	if events[0].Type != EventPhaseHeader {
		t.Errorf("first event: expected PhaseHeader, got %d: %s", events[0].Type, events[0].Raw)
	}

	// Should have exactly one game over event.
	if counts[EventGameOver] != 1 {
		t.Errorf("expected 1 game over event, got %d", counts[EventGameOver])
	}

	// Find game over and verify scores.
	for _, ev := range events {
		if ev.Type == EventGameOver {
			if ev.Scores["0500Rayquaza"] != 6494 {
				t.Errorf("0500Rayquaza score: expected 6494, got %d", ev.Scores["0500Rayquaza"])
			}
			if ev.Scores["0y4h1l2k0y6"] != 5661 {
				t.Errorf("0y4h1l2k0y6 score: expected 5661, got %d", ev.Scores["0y4h1l2k0y6"])
			}
		}
	}

	// Should have at least 1 bank broken event.
	if counts[EventBankBroken] < 1 {
		t.Error("expected at least 1 bank broken event")
	}

	// Verify the first par event.
	for _, ev := range events {
		if ev.Type == EventPar {
			if ev.Player != "0y4h1l2k0y6" {
				t.Errorf("first par player: expected 0y4h1l2k0y6, got %s", ev.Player)
			}
			if ev.Company != "IR" {
				t.Errorf("first par company: expected IR, got %s", ev.Company)
			}
			if ev.Amount != 65 {
				t.Errorf("first par price: expected 65, got %d", ev.Amount)
			}
			break
		}
	}

	// Ignored events should be minimal.
	if ignored > len(events)/10 {
		t.Errorf("too many ignored events: %d/%d (>10%%)", ignored, len(events))
	}
}

func TestParseSpecificLines(t *testing.T) {
	tests := []struct {
		line     string
		wantType EventType
		check    func(t *testing.T, ev Event)
	}{
		{
			line:     "[16:27] 0y4h1l2k0y6 pars IR at ¥65",
			wantType: EventPar,
			check: func(t *testing.T, ev Event) {
				if ev.Player != "0y4h1l2k0y6" || ev.Company != "IR" || ev.Amount != 65 {
					t.Errorf("par: %+v", ev)
				}
			},
		},
		{
			line:     "[16:27] 0y4h1l2k0y6 buys a 10% share of IR from the IPO for ¥65",
			wantType: EventBuyShareIPO,
			check: func(t *testing.T, ev Event) {
				if ev.SharePct != 10 || ev.Company != "IR" || ev.Amount != 65 {
					t.Errorf("buy share: %+v", ev)
				}
			},
		},
		{
			line:     "[16:28] IR lays tile #57 with rotation 1 on E2 (Matsuyama)",
			wantType: EventTileLay,
			check: func(t *testing.T, ev Event) {
				if ev.Company != "IR" || ev.TileID != 57 || ev.Rotation != 1 || ev.HexID != "E2" {
					t.Errorf("tile lay: %+v", ev)
				}
			},
		},
		{
			line:     "[16:36] IR spends ¥80 and lays tile #8 with rotation 2 on E4",
			wantType: EventTileLay,
			check: func(t *testing.T, ev Event) {
				if ev.Company != "IR" || ev.Amount != 80 || ev.TileID != 8 || ev.HexID != "E4" {
					t.Errorf("tile lay cost: %+v", ev)
				}
			},
		},
		{
			line:     "[16:35] IR runs a 2 train for ¥50: E2-F1",
			wantType: EventRunRoute,
			check: func(t *testing.T, ev Event) {
				if ev.Company != "IR" || ev.TrainType != "2" || ev.RouteRev != 50 {
					t.Errorf("route: %+v", ev)
				}
				if len(ev.RouteStops) != 2 || ev.RouteStops[0] != "E2" || ev.RouteStops[1] != "F1" {
					t.Errorf("route stops: %v", ev.RouteStops)
				}
			},
		},
		{
			line:     "[16:47] 0500Rayquaza sells 2 shares of IR and receives ¥180",
			wantType: EventSellShares,
			check: func(t *testing.T, ev Event) {
				if ev.Player != "0500Rayquaza" || ev.Amount2 != 2 || ev.Company != "IR" || ev.Amount != 180 {
					t.Errorf("sell: %+v", ev)
				}
			},
		},
		{
			line:     "[16:51] UR buys a 2 train for ¥110 from TR",
			wantType: EventBuyTrainCompany,
			check: func(t *testing.T, ev Event) {
				if ev.Company != "UR" || ev.TrainType != "2" || ev.Amount != 110 || ev.FromCompany != "TR" {
					t.Errorf("buy train company: %+v", ev)
				}
			},
		},
		{
			line:     "[16:46] IR buys Ehime Railway from 0y4h1l2k0y6 for ¥80",
			wantType: EventBuyPrivateFromPlayer,
			check: func(t *testing.T, ev Event) {
				if ev.Company != "IR" || ev.Private != "Ehime Railway" || ev.Player != "0y4h1l2k0y6" || ev.Amount != 80 {
					t.Errorf("buy private: %+v", ev)
				}
			},
		},
		{
			line:     "[16:46] 0y4h1l2k0y6 (ER) lays tile #205 with rotation 1 on C4 (Ohzu)",
			wantType: EventTileLay,
			check: func(t *testing.T, ev Event) {
				if ev.Private != "ER" || ev.TileID != 205 || ev.HexID != "C4" {
					t.Errorf("player tile lay: %+v", ev)
				}
			},
		},
		{
			line:     "-- Game over: 0500Rayquaza (¥6494), 0y4h1l2k0y6 (¥5661) --",
			wantType: EventGameOver,
			check: func(t *testing.T, ev Event) {
				if ev.Scores["0500Rayquaza"] != 6494 || ev.Scores["0y4h1l2k0y6"] != 5661 {
					t.Errorf("scores: %v", ev.Scores)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.line[:30], func(t *testing.T) {
			ev := parseLine(tt.line, 1)
			if ev.Type != tt.wantType {
				t.Errorf("type: expected %d, got %d for: %s", tt.wantType, ev.Type, tt.line)
			}
			if tt.check != nil {
				tt.check(t, ev)
			}
		})
	}
}
