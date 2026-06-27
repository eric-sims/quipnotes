package game

import (
	"regexp"
	"testing"
)

// sampleTileKeys builds a small base tile list for tests.
func sampleTileKeys() []string {
	return []string{"0|alpha", "1|beta", "2|gamma", "3|delta", "4|epsilon"}
}

func TestCreateGameReturnsFourDigitCode(t *testing.T) {
	r := NewRegistry(sampleTileKeys())

	g, err := r.CreateGame()
	if err != nil {
		t.Fatalf("CreateGame returned error: %v", err)
	}

	matched, _ := regexp.MatchString(`^\d{4}$`, g.Code())
	if !matched {
		t.Fatalf("expected a 4-digit code, got %q", g.Code())
	}

	if _, err := r.GetGame(g.Code()); err != nil {
		t.Fatalf("created game not retrievable by code: %v", err)
	}
}

func TestCreateGameCodesAreUnique(t *testing.T) {
	r := NewRegistry(sampleTileKeys())
	seen := make(map[string]bool)

	for i := 0; i < 50; i++ {
		g, err := r.CreateGame()
		if err != nil {
			t.Fatalf("CreateGame returned error on iteration %d: %v", i, err)
		}
		if seen[g.Code()] {
			t.Fatalf("duplicate game code generated: %s", g.Code())
		}
		seen[g.Code()] = true
	}
}

func TestGamesAreIsolated(t *testing.T) {
	r := NewRegistry(sampleTileKeys())

	gameA, _ := r.CreateGame()
	gameB, _ := r.CreateGame()

	if err := gameA.AddPlayer("alice"); err != nil {
		t.Fatalf("AddPlayer A: %v", err)
	}
	if err := gameB.AddPlayer("bob"); err != nil {
		t.Fatalf("AddPlayer B: %v", err)
	}

	// Alice draws every tile in game A.
	drawn, err := gameA.DrawWordTiles(len(sampleTileKeys()), "alice")
	if err != nil {
		t.Fatalf("DrawWordTiles A: %v", err)
	}
	if len(drawn) != len(sampleTileKeys()) {
		t.Fatalf("expected alice to hold all %d tiles, got %d", len(sampleTileKeys()), len(drawn))
	}

	// Game B's pool must be untouched — bob can still draw the full set.
	drawnB, err := gameB.DrawWordTiles(len(sampleTileKeys()), "bob")
	if err != nil {
		t.Fatalf("DrawWordTiles B: %v", err)
	}
	if len(drawnB) != len(sampleTileKeys()) {
		t.Fatalf("game B pool was affected by game A: bob drew %d of %d", len(drawnB), len(sampleTileKeys()))
	}
}

func TestGetGameMissingCode(t *testing.T) {
	r := NewRegistry(sampleTileKeys())
	if _, err := r.GetGame("9999"); err == nil {
		t.Fatal("expected error for unknown game code, got nil")
	}
}

func TestCloseGameRemovesIt(t *testing.T) {
	r := NewRegistry(sampleTileKeys())
	g, _ := r.CreateGame()

	if err := r.CloseGame(g.Code()); err != nil {
		t.Fatalf("CloseGame: %v", err)
	}
	if _, err := r.GetGame(g.Code()); err == nil {
		t.Fatal("expected game to be gone after close")
	}
	// Closing again is an error (already removed).
	if err := r.CloseGame(g.Code()); err == nil {
		t.Fatal("expected error closing an already-closed game")
	}
}

func TestSubmitRoundTrip(t *testing.T) {
	r := NewRegistry(sampleTileKeys())
	g, _ := r.CreateGame()
	if err := g.AddPlayer("alice"); err != nil {
		t.Fatalf("AddPlayer: %v", err)
	}

	drawn, err := g.DrawWordTiles(3, "alice")
	if err != nil {
		t.Fatalf("DrawWordTiles: %v", err)
	}

	if err := g.Submit(drawn, "alice"); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	notes := g.GetSubmittedNotes()
	if len(notes) != 1 {
		t.Fatalf("expected 1 submitted note, got %d", len(notes))
	}

	// Submitted tiles return to the pool, so alice holds nothing now.
	held, _ := g.GetDrawnWordTiles("alice")
	if len(held) != 0 {
		t.Fatalf("expected alice to hold 0 tiles after submit, got %d", len(held))
	}
}
