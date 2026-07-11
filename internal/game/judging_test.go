package game

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"
)

// judgingGame builds a game with the given players joined, ready for rounds.
func judgingGame(t *testing.T, players ...PlayerId) *Manager {
	t.Helper()
	r := newTestRegistry()
	g, _ := r.CreateGame(false)
	for _, id := range players {
		if err := g.AddPlayer(id); err != nil {
			t.Fatalf("AddPlayer %s: %v", id, err)
		}
	}
	return g
}

// submitOneTile draws a single tile for the player and submits it as a note.
func submitOneTile(t *testing.T, g *Manager, id PlayerId) {
	t.Helper()
	drawn, err := g.DrawWordTiles(1, id)
	if err != nil {
		t.Fatalf("DrawWordTiles %s: %v", id, err)
	}
	if err := g.Submit(drawn[len(drawn)-1:], id); err != nil {
		t.Fatalf("Submit %s: %v", id, err)
	}
}

// drainEvents empties the test client's queue, returning every parsed event.
func drainEvents(t *testing.T, c *wsClient) []event {
	t.Helper()
	events := make([]event, 0)
	for {
		select {
		case payload := <-c.send:
			var e event
			if err := json.Unmarshal(payload, &e); err != nil {
				t.Fatalf("bad payload: %v", err)
			}
			events = append(events, e)
		default:
			return events
		}
	}
}

// eventsOfType filters drained events by type.
func eventsOfType(events []event, typ string) []event {
	out := make([]event, 0)
	for _, e := range events {
		if e.Type == typ {
			out = append(out, e)
		}
	}
	return out
}

func TestJudgeRotationEveryoneJudgesOncePerCycle(t *testing.T) {
	g := judgingGame(t, "alice", "bob", "carol")

	// Two full cycles: within each, every player judges exactly once.
	for cycle := 0; cycle < 2; cycle++ {
		judged := make([]PlayerId, 0, 3)
		for i := 0; i < 3; i++ {
			state, err := g.StartRound()
			if err != nil {
				t.Fatalf("StartRound: %v", err)
			}
			if state.JudgeId == "" {
				t.Fatalf("cycle %d round %d: expected a judge with 3 players", cycle, i+1)
			}
			if slices.Contains(judged, state.JudgeId) {
				t.Fatalf("cycle %d: %s judged twice before everyone had a turn", cycle, state.JudgeId)
			}
			judged = append(judged, state.JudgeId)
		}
	}
}

func TestNoJudgeWithFewerThanTwoPlayers(t *testing.T) {
	g := judgingGame(t, "solo")

	state, err := g.StartRound()
	if err != nil {
		t.Fatalf("StartRound: %v", err)
	}
	if state.JudgeId != "" {
		t.Fatalf("expected no judge with a single player, got %q", state.JudgeId)
	}
	// The lone player may submit as before.
	submitOneTile(t, g, "solo")
}

func TestJudgeCannotSubmit(t *testing.T) {
	g := judgingGame(t, "alice", "bob")
	state, _ := g.StartRound()

	drawn, _ := g.DrawWordTiles(1, state.JudgeId)
	if err := g.Submit(drawn, state.JudgeId); !errors.Is(err, ErrJudgeCannotSubmit) {
		t.Fatalf("expected ErrJudgeCannotSubmit for the judge, got %v", err)
	}
}

func TestSubmissionTotalsExcludeJudge(t *testing.T) {
	g := judgingGame(t, "alice", "bob", "carol")
	state, _ := g.StartRound()
	c := newTestClient(g.hub)

	// One of the two non-judges answers.
	nonJudges := make([]PlayerId, 0, 2)
	for _, id := range g.GetPlayers() {
		if id != state.JudgeId {
			nonJudges = append(nonJudges, id)
		}
	}
	submitOneTile(t, g, nonJudges[0])

	subs := eventsOfType(drainEvents(t, c), "submission")
	if len(subs) != 1 {
		t.Fatalf("expected 1 submission event, got %d", len(subs))
	}
	if subs[0].Count != 1 || subs[0].Total != 2 {
		t.Fatalf("expected count 1 / total 2 (judge excluded), got %d/%d", subs[0].Count, subs[0].Total)
	}
}

func TestJudgingAutoOpensWhenAllEligibleSubmitted(t *testing.T) {
	g := judgingGame(t, "alice", "bob", "carol")
	state, _ := g.StartRound()
	c := newTestClient(g.hub)

	for _, id := range g.GetPlayers() {
		if id != state.JudgeId {
			submitOneTile(t, g, id)
		}
	}

	if got := g.CurrentRoundState(); !got.JudgingOpen {
		t.Fatal("expected judging to open once every non-judge player submitted")
	}
	ready := eventsOfType(drainEvents(t, c), "judging_ready")
	if len(ready) != 1 || ready[0].Round != state.Round {
		t.Fatalf("expected exactly one judging_ready for round %d, got %+v", state.Round, ready)
	}

	// Once judging is open, submissions are closed — even for a late joiner.
	if err := g.AddPlayer("dave"); err != nil {
		t.Fatalf("AddPlayer dave: %v", err)
	}
	drawn, _ := g.DrawWordTiles(1, "dave")
	if err := g.Submit(drawn, "dave"); !errors.Is(err, ErrJudgingStarted) {
		t.Fatalf("expected ErrJudgingStarted after judging opened, got %v", err)
	}
}

func TestOpenJudgingForce(t *testing.T) {
	g := judgingGame(t, "alice", "bob", "carol")

	// Before any round.
	if err := g.OpenJudging(); !errors.Is(err, ErrNoActiveRound) {
		t.Fatalf("expected ErrNoActiveRound, got %v", err)
	}

	state, _ := g.StartRound()

	// No notes yet: nothing to judge.
	if err := g.OpenJudging(); !errors.Is(err, ErrNoNotesYet) {
		t.Fatalf("expected ErrNoNotesYet, got %v", err)
	}

	// One of two eligible players answers; the judge forces judging open.
	var straggler PlayerId
	answered := false
	for _, id := range g.GetPlayers() {
		if id == state.JudgeId {
			continue
		}
		if !answered {
			submitOneTile(t, g, id)
			answered = true
		} else {
			straggler = id
		}
	}
	if err := g.OpenJudging(); err != nil {
		t.Fatalf("OpenJudging: %v", err)
	}
	if err := g.OpenJudging(); !errors.Is(err, ErrJudgingAlreadyOpen) {
		t.Fatalf("expected ErrJudgingAlreadyOpen, got %v", err)
	}

	// The straggler is locked out for the rest of the round.
	drawn, _ := g.DrawWordTiles(1, straggler)
	if err := g.Submit(drawn, straggler); !errors.Is(err, ErrJudgingStarted) {
		t.Fatalf("expected ErrJudgingStarted for the straggler, got %v", err)
	}
}

func TestOpenJudgingRequiresAJudge(t *testing.T) {
	g := judgingGame(t, "solo")
	g.StartRound()
	submitOneTile(t, g, "solo")

	if err := g.OpenJudging(); !errors.Is(err, ErrNoJudge) {
		t.Fatalf("expected ErrNoJudge in a judge-less round, got %v", err)
	}
}

func TestFlipNoteLifecycle(t *testing.T) {
	g := judgingGame(t, "alice", "bob")
	state, _ := g.StartRound()
	for _, id := range g.GetPlayers() {
		if id != state.JudgeId {
			submitOneTile(t, g, id)
		}
	}
	// The only eligible player answered, so judging auto-opened.
	c := newTestClient(g.hub)

	notes := g.GetSubmittedNotes()
	if len(notes) != 1 || notes[0].Id != 1 || notes[0].Flipped {
		t.Fatalf("expected one face-down note with id 1, got %+v", notes)
	}

	if err := g.FlipNote(99); !errors.Is(err, ErrUnknownNote) {
		t.Fatalf("expected ErrUnknownNote, got %v", err)
	}
	if err := g.FlipNote(1); err != nil {
		t.Fatalf("FlipNote: %v", err)
	}
	if !g.GetSubmittedNotes()[0].Flipped {
		t.Fatal("expected the note to be face-up after FlipNote")
	}
	// Idempotent: a second flip succeeds but broadcasts nothing new.
	if err := g.FlipNote(1); err != nil {
		t.Fatalf("second FlipNote: %v", err)
	}
	flips := eventsOfType(drainEvents(t, c), "note_flipped")
	if len(flips) != 1 || flips[0].NoteId != 1 {
		t.Fatalf("expected exactly one note_flipped for note 1, got %+v", flips)
	}
}

func TestFlipLockedUntilJudgingOpens(t *testing.T) {
	g := judgingGame(t, "alice", "bob", "carol")
	state, _ := g.StartRound()

	// One of two eligible players answers — judging is not open yet.
	for _, id := range g.GetPlayers() {
		if id != state.JudgeId {
			submitOneTile(t, g, id)
			break
		}
	}
	if err := g.FlipNote(1); !errors.Is(err, ErrJudgingNotOpen) {
		t.Fatalf("expected ErrJudgingNotOpen before judging opens, got %v", err)
	}
}

func TestFlipFreeInJudgelessRound(t *testing.T) {
	g := judgingGame(t, "solo")
	g.StartRound()
	submitOneTile(t, g, "solo")

	// No judge (<2 players): the host may flip freely, as before judging existed.
	if err := g.FlipNote(1); err != nil {
		t.Fatalf("FlipNote in a judge-less round: %v", err)
	}
}

func TestPickFavoriteScoresAuthor(t *testing.T) {
	g := judgingGame(t, "alice", "bob")
	state, _ := g.StartRound()

	var author PlayerId
	for _, id := range g.GetPlayers() {
		if id != state.JudgeId {
			author = id
			submitOneTile(t, g, id)
		}
	}
	c := newTestClient(g.hub)

	// The note must be face-up before it can be picked.
	if _, err := g.PickFavorite(1); !errors.Is(err, ErrNoteNotFlipped) {
		t.Fatalf("expected ErrNoteNotFlipped, got %v", err)
	}
	if err := g.FlipNote(1); err != nil {
		t.Fatalf("FlipNote: %v", err)
	}

	winner, err := g.PickFavorite(1)
	if err != nil {
		t.Fatalf("PickFavorite: %v", err)
	}
	if winner != author {
		t.Fatalf("expected winner %q, got %q", author, winner)
	}

	// The author scored a point, visible on the roster.
	for _, p := range g.Roster() {
		want := 0
		if p.Id == author {
			want = 1
		}
		if p.Score != want {
			t.Fatalf("expected %s score %d, got %d", p.Id, want, p.Score)
		}
	}

	// The round state records the reveal for polls/reconnects.
	got := g.CurrentRoundState()
	if got.FavoriteNote != 1 || got.WinnerId != author {
		t.Fatalf("expected round state favorite 1 / winner %q, got %+v", author, got)
	}

	// One favorite per round.
	if _, err := g.PickFavorite(1); !errors.Is(err, ErrFavoritePicked) {
		t.Fatalf("expected ErrFavoritePicked on a second pick, got %v", err)
	}

	// The reveal reached the room: favorite_picked plus a fresh scored roster.
	events := drainEvents(t, c)
	picked := eventsOfType(events, "favorite_picked")
	if len(picked) != 1 || picked[0].NoteId != 1 || picked[0].WinnerId != author {
		t.Fatalf("expected one favorite_picked naming %q, got %+v", author, picked)
	}
	rosters := eventsOfType(events, "players")
	if len(rosters) != 1 {
		t.Fatalf("expected a players roster broadcast after the pick, got %+v", rosters)
	}

	// Scores carry across rounds.
	if _, err := g.StartRound(); err != nil {
		t.Fatalf("StartRound 2: %v", err)
	}
	for _, p := range g.Roster() {
		if p.Id == author && p.Score != 1 {
			t.Fatalf("expected %s to keep score 1 into the next round, got %d", author, p.Score)
		}
	}
}

func TestPickFavoriteRequiresOpenJudging(t *testing.T) {
	g := judgingGame(t, "alice", "bob", "carol")
	state, _ := g.StartRound()

	for _, id := range g.GetPlayers() {
		if id != state.JudgeId {
			submitOneTile(t, g, id)
			break // one straggler keeps judging closed
		}
	}
	if _, err := g.PickFavorite(1); !errors.Is(err, ErrJudgingNotOpen) {
		t.Fatalf("expected ErrJudgingNotOpen, got %v", err)
	}
}

func TestJudgeLeavingReassignsJudge(t *testing.T) {
	g := judgingGame(t, "alice", "bob", "carol")
	state, _ := g.StartRound()
	c := newTestClient(g.hub)

	if err := g.RemovePlayer(state.JudgeId); err != nil {
		t.Fatalf("RemovePlayer: %v", err)
	}

	got := g.CurrentRoundState()
	if got.JudgeId == "" || got.JudgeId == state.JudgeId {
		t.Fatalf("expected a replacement judge, got %q (was %q)", got.JudgeId, state.JudgeId)
	}
	// The replacement is announced by re-broadcasting the round.
	restarts := eventsOfType(drainEvents(t, c), "round_started")
	if len(restarts) != 1 || restarts[0].JudgeId != got.JudgeId || restarts[0].Round != state.Round {
		t.Fatalf("expected round_started re-announcing judge %q, got %+v", got.JudgeId, restarts)
	}
}

func TestStragglerLeavingOpensJudging(t *testing.T) {
	g := judgingGame(t, "alice", "bob", "carol")
	state, _ := g.StartRound()

	var straggler PlayerId
	answered := false
	for _, id := range g.GetPlayers() {
		if id == state.JudgeId {
			continue
		}
		if !answered {
			submitOneTile(t, g, id)
			answered = true
		} else {
			straggler = id
		}
	}
	c := newTestClient(g.hub)

	// The unanswered player walks away — everyone left has answered.
	if err := g.RemovePlayer(straggler); err != nil {
		t.Fatalf("RemovePlayer: %v", err)
	}
	if got := g.CurrentRoundState(); !got.JudgingOpen {
		t.Fatal("expected judging to open when the last holdout left")
	}
	ready := eventsOfType(drainEvents(t, c), "judging_ready")
	if len(ready) != 1 {
		t.Fatalf("expected one judging_ready, got %+v", ready)
	}
}

func TestNotesKeepStableIdsInShuffledOrder(t *testing.T) {
	g := judgingGame(t, "alice", "bob", "carol", "dave")
	state, _ := g.StartRound()

	for _, id := range g.GetPlayers() {
		if id != state.JudgeId {
			submitOneTile(t, g, id)
		}
	}

	first := g.GetSubmittedNotes()
	if len(first) != 3 {
		t.Fatalf("expected 3 notes, got %d", len(first))
	}
	ids := make([]int, len(first))
	for i, n := range first {
		ids[i] = n.Id
	}
	slices.Sort(ids)
	if !slices.Equal(ids, []int{1, 2, 3}) {
		t.Fatalf("expected 1-based ids 1..3, got %v", ids)
	}

	// The shuffled display order is fixed for the round: every fetch (and so
	// every screen) sees the same order.
	second := g.GetSubmittedNotes()
	for i := range first {
		if first[i].Id != second[i].Id {
			t.Fatalf("note order changed between fetches: %+v vs %+v", first, second)
		}
	}
}

// --- Player-driven round advancing (AdvanceRound) ---

func TestAdvanceRoundAnyPlayerStartsFirstRound(t *testing.T) {
	g := judgingGame(t, "alice", "bob", "carol")
	c := newTestClient(g.hub)

	// No round yet — any joined player may kick the game off (the host only
	// created the game).
	state, err := g.AdvanceRound("bob", 0)
	if err != nil {
		t.Fatalf("AdvanceRound from round 0: %v", err)
	}
	if state.Round != 1 || state.Prompt == "" || state.JudgeId == "" {
		t.Fatalf("expected a started round 1 with prompt and judge, got %+v", state)
	}

	// The round reached the room like a host draw would.
	starts := eventsOfType(drainEvents(t, c), "round_started")
	if len(starts) != 1 || starts[0].Round != 1 {
		t.Fatalf("expected one round_started for round 1, got %+v", starts)
	}
}

func TestAdvanceRoundOnlyJudgeMayAdvance(t *testing.T) {
	g := judgingGame(t, "alice", "bob", "carol")
	state, _ := g.StartRound()

	var nonJudge PlayerId
	for _, id := range g.GetPlayers() {
		if id != state.JudgeId {
			nonJudge = id
			break
		}
	}

	if _, err := g.AdvanceRound(nonJudge, state.Round); !errors.Is(err, ErrNotTheJudge) {
		t.Fatalf("expected ErrNotTheJudge for %q, got %v", nonJudge, err)
	}

	next, err := g.AdvanceRound(state.JudgeId, state.Round)
	if err != nil {
		t.Fatalf("AdvanceRound by judge: %v", err)
	}
	if next.Round != state.Round+1 {
		t.Fatalf("expected round %d, got %d", state.Round+1, next.Round)
	}
}

func TestAdvanceRoundRejectsStaleRound(t *testing.T) {
	g := judgingGame(t, "alice", "bob")
	state, _ := g.StartRound()

	// The judge advances; a second request still citing the old round loses
	// the race and must not skip a prompt.
	if _, err := g.AdvanceRound(state.JudgeId, state.Round); err != nil {
		t.Fatalf("AdvanceRound: %v", err)
	}
	if _, err := g.AdvanceRound(state.JudgeId, state.Round); !errors.Is(err, ErrRoundAdvanced) {
		t.Fatalf("expected ErrRoundAdvanced on a stale round, got %v", err)
	}
	if got := g.CurrentRoundState().Round; got != state.Round+1 {
		t.Fatalf("stale advance must not move the round: expected %d, got %d", state.Round+1, got)
	}
}

func TestAdvanceRoundAnyPlayerInJudgelessRound(t *testing.T) {
	g := judgingGame(t, "solo")
	state, _ := g.StartRound()
	if state.JudgeId != "" {
		t.Fatalf("expected a judge-less round, got judge %q", state.JudgeId)
	}

	next, err := g.AdvanceRound("solo", state.Round)
	if err != nil {
		t.Fatalf("AdvanceRound in judge-less round: %v", err)
	}
	if next.Round != state.Round+1 {
		t.Fatalf("expected round %d, got %d", state.Round+1, next.Round)
	}
}

func TestAdvanceRoundRequiresJoinedPlayer(t *testing.T) {
	g := judgingGame(t, "alice", "bob")
	state, _ := g.StartRound()

	if _, err := g.AdvanceRound("mallory", state.Round); err == nil {
		t.Fatal("expected an error for an unknown player")
	}
}

func TestAdvanceRoundClearsNotesLikeHostDraw(t *testing.T) {
	g := judgingGame(t, "alice", "bob")
	state, _ := g.StartRound()

	for _, id := range g.GetPlayers() {
		if id != state.JudgeId {
			submitOneTile(t, g, id)
		}
	}
	if len(g.GetSubmittedNotes()) != 1 {
		t.Fatalf("expected 1 note on the board, got %d", len(g.GetSubmittedNotes()))
	}

	if _, err := g.AdvanceRound(state.JudgeId, state.Round); err != nil {
		t.Fatalf("AdvanceRound: %v", err)
	}
	if len(g.GetSubmittedNotes()) != 0 {
		t.Fatal("expected the board to clear when the judge advances the round")
	}
}
