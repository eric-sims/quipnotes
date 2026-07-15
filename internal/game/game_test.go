package game

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"testing"
)

// sampleTileKeys builds a small base tile list for tests.
func sampleTileKeys() []string {
	return []string{"0|alpha", "1|beta", "2|gamma", "3|delta", "4|epsilon"}
}

// samplePrompts builds a small base prompt list for tests (all family-friendly).
func samplePrompts() []Prompt {
	return []Prompt{
		{Text: "prompt one", FamilyFriendly: true},
		{Text: "prompt two", FamilyFriendly: true},
		{Text: "prompt three", FamilyFriendly: true},
	}
}

// newTestRegistry wires a registry with the sample tile + prompt lists.
func newTestRegistry() *Registry {
	return NewRegistry(sampleTileKeys(), nil, samplePrompts())
}

func TestCreateGameReturnsFourDigitCode(t *testing.T) {
	r := newTestRegistry()

	g, err := r.CreateGame(false)
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
	r := newTestRegistry()
	seen := make(map[string]bool)

	for i := 0; i < 50; i++ {
		g, err := r.CreateGame(false)
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
	r := newTestRegistry()

	gameA, _ := r.CreateGame(false)
	gameB, _ := r.CreateGame(false)

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

func TestRosterReflectsPlayers(t *testing.T) {
	r := newTestRegistry()
	g, _ := r.CreateGame(false)

	if roster := g.Roster(); len(roster) != 0 {
		t.Fatalf("expected empty roster on a fresh game, got %d", len(roster))
	}

	if err := g.AddPlayer("alice"); err != nil {
		t.Fatalf("AddPlayer alice: %v", err)
	}
	if err := g.AddPlayer("bob"); err != nil {
		t.Fatalf("AddPlayer bob: %v", err)
	}

	roster := g.Roster()
	if len(roster) != 2 || roster[0].Id != "alice" || roster[1].Id != "bob" {
		t.Fatalf("unexpected roster: %+v", roster)
	}

	if err := g.RemovePlayer("alice"); err != nil {
		t.Fatalf("RemovePlayer alice: %v", err)
	}
	if roster := g.Roster(); len(roster) != 1 || roster[0].Id != "bob" {
		t.Fatalf("expected only bob after removal, got %+v", roster)
	}
}

// nextPlayersEvent drains the client's channel for the next "players" event,
// failing if none is queued.
func nextPlayersEvent(t *testing.T, c *wsClient) event {
	t.Helper()
	select {
	case payload := <-c.send:
		var e event
		if err := json.Unmarshal(payload, &e); err != nil {
			t.Fatalf("bad payload: %v", err)
		}
		if e.Type != "players" {
			t.Fatalf("expected a players event, got %q", e.Type)
		}
		return e
	default:
		t.Fatal("expected a players event but the channel was empty")
		return event{}
	}
}

func TestAddRemovePlayerBroadcastsRoster(t *testing.T) {
	r := newTestRegistry()
	g, _ := r.CreateGame(false)
	c := newTestClient(g.hub)

	if err := g.AddPlayer("alice"); err != nil {
		t.Fatalf("AddPlayer: %v", err)
	}
	if e := nextPlayersEvent(t, c); len(e.Players) != 1 || e.Players[0].Id != "alice" {
		t.Fatalf("unexpected join roster: %+v", e.Players)
	}

	if err := g.RemovePlayer("alice"); err != nil {
		t.Fatalf("RemovePlayer: %v", err)
	}
	if e := nextPlayersEvent(t, c); len(e.Players) != 0 {
		t.Fatalf("expected empty roster after leave, got %+v", e.Players)
	}
}

func TestGetGameMissingCode(t *testing.T) {
	r := newTestRegistry()
	if _, err := r.GetGame("9999"); err == nil {
		t.Fatal("expected error for unknown game code, got nil")
	}
}

func TestCloseGameRemovesIt(t *testing.T) {
	r := newTestRegistry()
	g, _ := r.CreateGame(false)

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
	r := newTestRegistry()
	g, _ := r.CreateGame(false)
	if err := g.AddPlayer("alice"); err != nil {
		t.Fatalf("AddPlayer: %v", err)
	}

	drawn, err := g.DrawWordTiles(3, "alice")
	if err != nil {
		t.Fatalf("DrawWordTiles: %v", err)
	}

	// A note only counts against an active round.
	if _, err := g.StartRound(); err != nil {
		t.Fatalf("StartRound: %v", err)
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

func TestSubmitRequiresActiveRound(t *testing.T) {
	r := newTestRegistry()
	g, _ := r.CreateGame(false)
	if err := g.AddPlayer("alice"); err != nil {
		t.Fatalf("AddPlayer: %v", err)
	}
	drawn, err := g.DrawWordTiles(3, "alice")
	if err != nil {
		t.Fatalf("DrawWordTiles: %v", err)
	}

	// No round has started yet.
	if err := g.Submit(drawn, "alice"); !errors.Is(err, ErrNoActiveRound) {
		t.Fatalf("expected ErrNoActiveRound, got %v", err)
	}
}

func TestSubmitOncePerRound(t *testing.T) {
	r := newTestRegistry()
	g, _ := r.CreateGame(false)
	if err := g.AddPlayer("alice"); err != nil {
		t.Fatalf("AddPlayer: %v", err)
	}
	if _, err := g.StartRound(); err != nil {
		t.Fatalf("StartRound: %v", err)
	}

	// First submission of the round succeeds.
	first, _ := g.DrawWordTiles(2, "alice")
	if err := g.Submit(first, "alice"); err != nil {
		t.Fatalf("first Submit: %v", err)
	}

	// A second submission in the same round is rejected.
	second, _ := g.DrawWordTiles(2, "alice")
	if err := g.Submit(second, "alice"); !errors.Is(err, ErrAlreadySubmitted) {
		t.Fatalf("expected ErrAlreadySubmitted on second submit, got %v", err)
	}

	// A new round re-enables submission and clears the previous notes.
	if _, err := g.StartRound(); err != nil {
		t.Fatalf("StartRound 2: %v", err)
	}
	if notes := g.GetSubmittedNotes(); len(notes) != 0 {
		t.Fatalf("expected notes cleared at round start, got %d", len(notes))
	}
	if err := g.Submit(second, "alice"); err != nil {
		t.Fatalf("Submit after new round: %v", err)
	}
}

func TestSubmitPreservesBreaks(t *testing.T) {
	r := newTestRegistry()
	g, _ := r.CreateGame(false)
	if err := g.AddPlayer("alice"); err != nil {
		t.Fatalf("AddPlayer: %v", err)
	}
	if _, err := g.StartRound(); err != nil {
		t.Fatalf("StartRound: %v", err)
	}
	drawn, err := g.DrawWordTiles(2, "alice")
	if err != nil {
		t.Fatalf("DrawWordTiles: %v", err)
	}

	// A break between the two tiles must survive into the stored note.
	note := []string{drawn[0], BreakToken, drawn[1]}
	if err := g.Submit(note, "alice"); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	notes := g.GetSubmittedNotes()
	if len(notes) != 1 {
		t.Fatalf("expected 1 note, got %d", len(notes))
	}
	if !slices.Equal(notes[0].Tokens, note) {
		t.Fatalf("expected stored note %v, got %v", note, notes[0].Tokens)
	}
	// Breaks release no tile, but both real tiles return to the pool.
	if held, _ := g.GetDrawnWordTiles("alice"); len(held) != 0 {
		t.Fatalf("expected alice to hold 0 tiles after submit, got %d", len(held))
	}
}

func TestSubmitNormalizesBreaks(t *testing.T) {
	r := newTestRegistry()
	g, _ := r.CreateGame(false)
	if err := g.AddPlayer("alice"); err != nil {
		t.Fatalf("AddPlayer: %v", err)
	}
	if _, err := g.StartRound(); err != nil {
		t.Fatalf("StartRound: %v", err)
	}
	drawn, _ := g.DrawWordTiles(2, "alice")

	// Leading, trailing, and doubled breaks all collapse away.
	note := []string{BreakToken, drawn[0], BreakToken, BreakToken, drawn[1], BreakToken}
	if err := g.Submit(note, "alice"); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	want := []string{drawn[0], BreakToken, drawn[1]}
	got := g.GetSubmittedNotes()[0].Tokens
	if !slices.Equal(got, want) {
		t.Fatalf("expected normalized note %v, got %v", want, got)
	}
}

func TestSubmitRejectsBreaksOnlyNote(t *testing.T) {
	r := newTestRegistry()
	g, _ := r.CreateGame(false)
	if err := g.AddPlayer("alice"); err != nil {
		t.Fatalf("AddPlayer: %v", err)
	}
	if _, err := g.StartRound(); err != nil {
		t.Fatalf("StartRound: %v", err)
	}
	// Hold a tile so we can confirm the rejected submit consumes nothing.
	drawn, _ := g.DrawWordTiles(1, "alice")

	if err := g.Submit([]string{BreakToken, BreakToken}, "alice"); err == nil {
		t.Fatal("expected an error submitting a note with no tiles")
	}
	if held, _ := g.GetDrawnWordTiles("alice"); len(held) != len(drawn) {
		t.Fatalf("expected alice to keep %d tiles after a rejected submit, got %d", len(drawn), len(held))
	}
}

func TestStartRoundAdvancesAndWraps(t *testing.T) {
	r := newTestRegistry()
	g, _ := r.CreateGame(false)

	n := len(samplePrompts())
	seen := make([]string, 0, n)
	for i := 1; i <= n; i++ {
		state, err := g.StartRound()
		if err != nil {
			t.Fatalf("StartRound %d: %v", i, err)
		}
		if state.Round != i {
			t.Fatalf("expected round %d, got %d", i, state.Round)
		}
		seen = append(seen, state.Prompt)
	}

	// The deck is a full copy: every prompt appears exactly once before wrap.
	if len(seen) != n {
		t.Fatalf("expected %d prompts drawn, got %d", n, len(seen))
	}
	for _, want := range samplePrompts() {
		if !slices.Contains(seen, want.Text) {
			t.Fatalf("prompt %q was never drawn: deck is not a full copy", want.Text)
		}
	}

	// Exhausting the deck reshuffles rather than erroring.
	state, err := g.StartRound()
	if err != nil {
		t.Fatalf("StartRound after exhaustion: %v", err)
	}
	if state.Round != n+1 || state.Prompt == "" {
		t.Fatalf("expected wrap to round %d with a prompt, got round %d prompt %q", n+1, state.Round, state.Prompt)
	}

	if got := g.CurrentRoundState(); got.Round != state.Round || got.Prompt != state.Prompt {
		t.Fatalf("CurrentRoundState mismatch: got (%d,%q) want (%d,%q)", got.Round, got.Prompt, state.Round, state.Prompt)
	}
}

func TestPromptDeckOrderVariesBetweenGames(t *testing.T) {
	// Use a larger deck so a matching shuffle is astronomically unlikely.
	prompts := make([]Prompt, 30)
	for i := range prompts {
		prompts[i] = Prompt{Text: fmt.Sprintf("prompt-%02d", i), FamilyFriendly: true}
	}
	r := NewRegistry(sampleTileKeys(), nil, prompts)

	drawAll := func() []string {
		g, _ := r.CreateGame(false)
		out := make([]string, len(prompts))
		for i := range prompts {
			st, _ := g.StartRound()
			out[i] = st.Prompt
		}
		return out
	}

	if slices.Equal(drawAll(), drawAll()) {
		t.Fatal("two games drew prompts in identical order; deck is not shuffled per game")
	}
}
