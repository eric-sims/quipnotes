package game

import (
	"errors"
	"fmt"
	"log"
	"math/rand/v2"
	"slices"
	"strings"
	"sync"
)

// Games is the registry of all live games, keyed by 4-digit code. It replaces
// the former single global game.
var Games *Registry

type PlayerId string

// BreakToken is the reserved token a note may contain to mark a line break
// between clusters of tiles. It carries no "<id>|<word>" tile — it just tells
// the host to start a new line — so it can never collide with a real tile
// (tiles always contain "|", and no word is a bare newline). The clients emit
// and render the same token; see quipnotesclient/src/tiles.js.
const BreakToken = "\n"

// Player is a roster entry as it crosses the wire (GET /players and the
// "players" event): the id plus the player's running score for the game.
type Player struct {
	Id    PlayerId `json:"id"`
	Score int      `json:"score"`
}

// rosterLocked maps the internal players slice to the wire roster. The caller
// must hold gm.mux.
func (gm *Manager) rosterLocked() []Player {
	roster := make([]Player, len(gm.players))
	for i, id := range gm.players {
		roster[i] = Player{Id: id, Score: gm.scores[id]}
	}
	return roster
}

// Round/submit/judging errors the router maps to 409 Conflict (a friendly,
// retryable condition) rather than a hard 500.
var (
	ErrNoActiveRound      = errors.New("no active round yet; wait for the host to draw a prompt")
	ErrAlreadySubmitted   = errors.New("you already submitted a note this round")
	ErrJudgeCannotSubmit  = errors.New("you are the judge this round; wait for the notes to come in")
	ErrJudgingStarted     = errors.New("judging has started; submissions are closed this round")
	ErrNoJudge            = errors.New("this round has no judge")
	ErrJudgingAlreadyOpen = errors.New("judging is already open")
	ErrJudgingNotOpen     = errors.New("judging has not started yet")
	ErrNoNotesYet         = errors.New("no notes have been submitted yet")
	ErrFavoritePicked     = errors.New("a favorite has already been picked this round")
	ErrNoteNotFlipped     = errors.New("flip the note over before picking it")
	ErrUnknownNote        = errors.New("unknown note")
)

// submittedNote is one ransom note on the board: its ordered token list (tiles
// + BreakToken), which player wrote it (for scoring — never sent to clients
// before the reveal), whether the judge has turned it over yet, and a random
// sort key so every screen shows the board in the same shuffled order without
// leaking submission order.
type submittedNote struct {
	tokens  []string
	author  PlayerId
	flipped bool
	sortKey float64
}

// NoteView is a note as it crosses the wire (GET /submitted-notes). Id is
// 1-based and stable for the round; the author stays server-side.
type NoteView struct {
	Id      int      `json:"id"`
	Tokens  []string `json:"tokens"`
	Flipped bool     `json:"flipped"`
}

// RoundState is a full snapshot of the active round, used by GET /round and
// the POST /rounds response so a poll or reconnect can restore judging state.
type RoundState struct {
	Round        int      `json:"round"`
	Prompt       string   `json:"prompt"`
	JudgeId      PlayerId `json:"judgeId"`
	JudgingOpen  bool     `json:"judgingOpen"`
	Count        int      `json:"count"`          // notes submitted this round
	Total        int      `json:"total"`          // players eligible to submit (judge excluded)
	FavoriteNote int      `json:"favoriteNoteId"` // 0 = none picked yet
	WinnerId     PlayerId `json:"winnerId"`
}

// Manager holds the state for a single game. Many games run concurrently, each
// identified by its code and held in the Registry.
type Manager struct {
	code           string
	players        []PlayerId
	wordTiles      map[string]PlayerId
	submittedNotes []submittedNote // this round's notes (cleared each round)

	// Round / prompt state. A "round" is an active prompt: the host draws the
	// next prompt off promptDeck to start one. round is 0 until the first draw.
	promptDeck         []string          // shuffled copy of the registry prompts
	promptCursor       int               // index of the next prompt to draw
	round              int               // 0 = no round started; ++ each draw
	currentPrompt      string            // the active round's prompt ("" if none)
	submittedThisRound map[PlayerId]bool // players who submitted this round

	// Judging / scoring state. Each round one player is the judge: they don't
	// submit, and once every other player has (or the judge forces it), judging
	// opens — the judge flips notes over and picks a favorite, whose author
	// scores a point. hasJudged tracks the rotation cycle: everyone judges once
	// before anyone judges again.
	scores       map[PlayerId]int  // running score per player (survives rounds)
	hasJudged    map[PlayerId]bool // who has judged in the current rotation cycle
	judge        PlayerId          // this round's judge ("" = no judge, <2 players)
	judgingOpen  bool              // submissions closed, judge may flip/pick
	favoriteNote int               // 1-based id of the picked note (0 = none yet)
	winner       PlayerId          // author of the picked note ("" = none yet)

	hub *hub // WebSocket subscribers for this game (push channel)

	mux sync.RWMutex
}

// newGame builds a fresh game seeded with its own copy of the tile pool (every
// tile available) and its own shuffled prompt deck, so each game plays a unique
// prompt order. tileKeys are the "<idx>|<word>" keys loaded once from CSV;
// prompts is the base prompt list held by the Registry.
func newGame(code string, tileKeys, prompts []string) *Manager {
	wordTiles := make(map[string]PlayerId, len(tileKeys))
	for _, key := range tileKeys {
		wordTiles[key] = ""
	}

	promptDeck := slices.Clone(prompts)
	rand.Shuffle(len(promptDeck), func(i, j int) {
		promptDeck[i], promptDeck[j] = promptDeck[j], promptDeck[i]
	})

	return &Manager{
		code:               code,
		players:            make([]PlayerId, 0),
		wordTiles:          wordTiles,
		promptDeck:         promptDeck,
		submittedThisRound: make(map[PlayerId]bool),
		scores:             make(map[PlayerId]int),
		hasJudged:          make(map[PlayerId]bool),
		hub:                newHub(),
		mux:                sync.RWMutex{},
	}
}

// Code returns the game's 4-digit code.
func (gm *Manager) Code() string {
	return gm.code
}

// Registry owns the collection of live games and the shared base tile and
// prompt lists that seed every new game.
type Registry struct {
	games    map[string]*Manager
	tileKeys []string
	prompts  []string
	mux      sync.RWMutex
}

// codeGenAttempts caps how many times we retry to find a free code before
// giving up (effectively "the registry is full").
const codeGenAttempts = 100

// NewRegistry creates an empty registry that seeds every new game from tileKeys
// (the word pool) and prompts (the prompt deck).
func NewRegistry(tileKeys, prompts []string) *Registry {
	return &Registry{
		games:    make(map[string]*Manager),
		tileKeys: tileKeys,
		prompts:  prompts,
		mux:      sync.RWMutex{},
	}
}

// CreateGame allocates a new game under a unique 4-digit code and returns it.
// No players are added at creation — players join later with the code.
func (r *Registry) CreateGame() (*Manager, error) {
	r.mux.Lock()
	defer r.mux.Unlock()

	for i := 0; i < codeGenAttempts; i++ {
		code := fmt.Sprintf("%04d", rand.IntN(10000))
		if _, exists := r.games[code]; exists {
			continue
		}
		game := newGame(code, r.tileKeys, r.prompts)
		r.games[code] = game
		log.Printf("Created game: %s", code)
		return game, nil
	}
	return nil, errors.New("could not allocate a free game code")
}

// GetGame looks up a live game by code.
func (r *Registry) GetGame(code string) (*Manager, error) {
	r.mux.RLock()
	defer r.mux.RUnlock()

	game, ok := r.games[code]
	if !ok {
		return nil, fmt.Errorf("game %s not found", code)
	}
	return game, nil
}

// CloseGame removes a game from the registry. No auth: in practice only the
// manager (host) calls this.
func (r *Registry) CloseGame(code string) error {
	r.mux.Lock()
	defer r.mux.Unlock()

	game, ok := r.games[code]
	if !ok {
		return fmt.Errorf("game %s not found", code)
	}
	delete(r.games, code)
	// Tell every connected client the game is over, then close their sockets so
	// they fall back to the join/lobby screen.
	game.hub.broadcast(event{Type: "game_ended"})
	game.hub.closeAll()
	log.Printf("Closed game: %s", code)
	return nil
}

func (gm *Manager) AddPlayer(id PlayerId) error {
	gm.mux.Lock()

	if strings.TrimSpace(string(id)) == "" {
		gm.mux.Unlock()
		return errors.New("invalid player id")
	}

	if slices.Contains(gm.players, id) {
		gm.mux.Unlock()
		return errors.New("cannot add player. id already exists")
	}
	gm.players = append(gm.players, id)
	roster := gm.rosterLocked()

	gm.mux.Unlock()

	// Broadcast the new roster outside the lock (mirrors StartRound) so hosts
	// see who has joined live — before any note is submitted.
	gm.hub.broadcast(event{Type: "players", Players: roster})
	log.Printf("Added player: %+v\n", id)
	return nil
}

func (gm *Manager) RemovePlayer(id PlayerId) error {
	gm.mux.Lock()

	for word, playerId := range gm.wordTiles {
		if playerId == id {
			gm.wordTiles[word] = ""
		}
	}

	index := slices.Index(gm.players, id)
	if index == -1 {
		gm.mux.Unlock()
		return errors.New("cannot remove player. id does not exist")
	}
	gm.players = slices.Delete(gm.players, index, index+1)
	delete(gm.submittedThisRound, id)
	delete(gm.hasJudged, id)
	// The score is kept: a player who drops and rejoins under the same id gets
	// their points back.

	// If the judge left mid-round, hand the gavel to the next player in the
	// rotation so the round isn't stranded without anyone able to pick.
	judgeChanged := false
	if gm.judge == id && gm.round > 0 && gm.winner == "" {
		gm.judge = gm.pickJudgeLocked()
		judgeChanged = true
	}
	// A straggler leaving can also complete "everyone has answered".
	judgingOpened := gm.maybeOpenJudgingLocked()

	roster := gm.rosterLocked()
	state := gm.roundStateLocked()

	gm.mux.Unlock()

	// Broadcast the updated roster outside the lock (mirrors StartRound / AddPlayer).
	gm.hub.broadcast(event{Type: "players", Players: roster})
	if judgeChanged {
		// Re-announce the round so every client learns the replacement judge.
		gm.hub.broadcast(event{Type: "round_started", Round: state.Round, Prompt: state.Prompt, JudgeId: state.JudgeId})
	}
	if judgingOpened {
		gm.hub.broadcast(event{Type: "judging_ready", Round: state.Round})
	}
	log.Printf("Removed player: %+v\n", id)
	return nil
}

// pickJudgeLocked selects the next judge: the first player (in join order) who
// hasn't judged in the current rotation cycle, starting a fresh cycle once
// everyone has. Judging needs a judge plus at least one submitter, so with
// fewer than 2 players it returns "" and the round runs judge-less (everyone
// may submit; the host can still flip notes). The caller must hold gm.mux.
func (gm *Manager) pickJudgeLocked() PlayerId {
	if len(gm.players) < 2 {
		return ""
	}
	for _, id := range gm.players {
		if !gm.hasJudged[id] {
			gm.hasJudged[id] = true
			return id
		}
	}
	// Everyone has judged — start the next cycle.
	gm.hasJudged = make(map[PlayerId]bool)
	id := gm.players[0]
	gm.hasJudged[id] = true
	return id
}

// eligibleTotalLocked is how many players may submit this round: everyone
// except the judge. The caller must hold gm.mux.
func (gm *Manager) eligibleTotalLocked() int {
	total := len(gm.players)
	if gm.judge != "" && slices.Contains(gm.players, gm.judge) {
		total--
	}
	return total
}

// maybeOpenJudgingLocked opens judging when every eligible player has
// submitted. Returns true only on the transition (so the caller broadcasts
// judging_ready exactly once). Rounds without a judge never auto-open —
// there is nobody to pick a favorite. The caller must hold gm.mux.
func (gm *Manager) maybeOpenJudgingLocked() bool {
	if gm.round == 0 || gm.judge == "" || gm.judgingOpen || gm.winner != "" {
		return false
	}
	total := gm.eligibleTotalLocked()
	if total == 0 || len(gm.submittedThisRound) < total {
		return false
	}
	gm.judgingOpen = true
	return true
}

// roundStateLocked snapshots the active round. The caller must hold gm.mux.
func (gm *Manager) roundStateLocked() RoundState {
	return RoundState{
		Round:        gm.round,
		Prompt:       gm.currentPrompt,
		JudgeId:      gm.judge,
		JudgingOpen:  gm.judgingOpen,
		Count:        len(gm.submittedThisRound),
		Total:        gm.eligibleTotalLocked(),
		FavoriteNote: gm.favoriteNote,
		WinnerId:     gm.winner,
	}
}

func (gm *Manager) DrawWordTiles(n int, id PlayerId) ([]string, error) {
	gm.mux.Lock()

	if n <= 0 || n > len(gm.wordTiles) {
		return nil, fmt.Errorf("invalid number of wordTiles requested: %d", n)
	}

	// Collect keys that haven't been set to true
	availableWords := make([]string, 0)
	for word, playerId := range gm.wordTiles {
		if playerId == "" {
			availableWords = append(availableWords, word)
		}
	}

	// Check if there are enough wordTiles available
	if len(availableWords) < n {
		return nil, fmt.Errorf("not enough wordTiles available, only %d left", len(availableWords))
	}

	// Shuffle the available wordTiles
	rand.Shuffle(len(availableWords), func(i, j int) {
		availableWords[i], availableWords[j] = availableWords[j], availableWords[i]
	})

	// Select the first `n` wordTiles from the shuffled list
	selectedWords := availableWords[:n]

	// Mark the selected wordTiles as retrieved in gm.wordTiles
	for _, word := range selectedWords {
		gm.wordTiles[word] = id
	}

	gm.mux.Unlock()
	// return the whole list so that the client can remain stateless
	return gm.GetDrawnWordTiles(id)
}

// Submit - reads off the ransom note and puts the tiles back into the WordStore.
// A note is the player's ordered token list: tile keys ("42|banana") plus any
// BreakToken markers for line breaks between clusters. The whole list is stored
// (normalized) and handed to the host as-is, so all three sides speak the same
// tile language rather than a flattened string.
func (gm *Manager) Submit(note []string, id PlayerId) error {
	gm.mux.Lock()

	if err := gm.submitLocked(note, id); err != nil {
		gm.mux.Unlock()
		return err
	}

	judgingOpened := gm.maybeOpenJudgingLocked()
	state := gm.roundStateLocked()
	gm.mux.Unlock()

	// Push the live "answered" count, and — when this was the last eligible
	// player — tell everyone judging is open. Broadcast outside the lock so a
	// slow subscriber can't stall game state.
	gm.hub.broadcast(event{Type: "submission", Round: state.Round, Count: state.Count, Total: state.Total})
	if judgingOpened {
		gm.hub.broadcast(event{Type: "judging_ready", Round: state.Round})
	}
	return nil
}

// submitLocked validates and stores a note. The caller must hold gm.mux.
func (gm *Manager) submitLocked(note []string, id PlayerId) error {
	if !slices.Contains(gm.players, id) {
		log.Printf("Player %s not found in game", id)
		return fmt.Errorf("player %s not found", id)
	}

	// One note per round: a note only counts against an active round, and a
	// player may submit at most once until the host draws the next prompt.
	if gm.round == 0 {
		return ErrNoActiveRound
	}
	if id == gm.judge {
		return ErrJudgeCannotSubmit
	}
	if gm.judgingOpen {
		return ErrJudgingStarted
	}
	if gm.submittedThisRound[id] {
		return ErrAlreadySubmitted
	}

	// Verification loop (no mutations yet). Break tokens carry no tile, so they
	// skip the format/ownership checks; every real tile must belong to the
	// submitting player. We also count the tiles so an all-breaks/empty note is
	// rejected.
	tileCount := 0
	for _, token := range note {
		if token == BreakToken {
			continue
		}
		if len(strings.Split(token, "|")) < 2 {
			log.Println("Found word with wrong format")
			return fmt.Errorf("word %s not found in wrong format", token)
		}
		if gm.wordTiles[token] != id {
			return fmt.Errorf("word %s not part of your word pile", token)
		}
		tileCount++
	}
	if tileCount == 0 {
		return fmt.Errorf("no wordTiles found")
	}

	// Second pass, only now that the whole note has passed: release each tile
	// back to the pool. Break tokens release nothing.
	for _, token := range note {
		if token == BreakToken {
			continue
		}
		gm.wordTiles[token] = ""
	}

	// Store the normalized token list (edge/duplicate breaks collapsed) with
	// its author (for scoring) and a random sort key (so every screen shows the
	// board in the same shuffled order), and record that this player has
	// answered the round.
	gm.submittedNotes = append(gm.submittedNotes, submittedNote{
		tokens:  normalizeNote(note),
		author:  id,
		sortKey: rand.Float64(),
	})
	gm.submittedThisRound[id] = true

	return nil
}

// normalizeNote trims leading/trailing BreakToken markers and collapses any run
// of consecutive breaks to a single one, so the host never renders blank lines.
func normalizeNote(note []string) []string {
	out := make([]string, 0, len(note))
	for _, token := range note {
		if token == BreakToken {
			// Skip a leading break or one that follows another break.
			if len(out) == 0 || out[len(out)-1] == BreakToken {
				continue
			}
		}
		out = append(out, token)
	}
	// Drop a trailing break.
	if n := len(out); n > 0 && out[n-1] == BreakToken {
		out = out[:n-1]
	}
	return out
}

// StartRound draws the next prompt off this game's shuffled deck and begins a
// new round: it clears the previous round's submitted notes, resets the
// per-round submission and judging state, and selects the round's judge (see
// pickJudgeLocked). When the deck is exhausted it reshuffles and starts over,
// so a game never runs out of prompts. Returns the new round's state.
func (gm *Manager) StartRound() (RoundState, error) {
	gm.mux.Lock()

	if len(gm.promptDeck) == 0 {
		gm.mux.Unlock()
		return RoundState{}, errors.New("no prompts available")
	}
	if gm.promptCursor >= len(gm.promptDeck) {
		// Deck exhausted — reshuffle and start over.
		rand.Shuffle(len(gm.promptDeck), func(i, j int) {
			gm.promptDeck[i], gm.promptDeck[j] = gm.promptDeck[j], gm.promptDeck[i]
		})
		gm.promptCursor = 0
	}

	gm.currentPrompt = gm.promptDeck[gm.promptCursor]
	gm.promptCursor++
	gm.round++
	gm.submittedThisRound = make(map[PlayerId]bool)
	gm.submittedNotes = make([]submittedNote, 0)
	gm.judgingOpen = false
	gm.favoriteNote = 0
	gm.winner = ""
	gm.judge = gm.pickJudgeLocked()
	state := gm.roundStateLocked()

	gm.mux.Unlock()

	// Push the new round to every connected client. Broadcast outside the lock
	// so a slow subscriber can't stall game state.
	gm.hub.broadcast(event{Type: "round_started", Round: state.Round, Prompt: state.Prompt, JudgeId: state.JudgeId})
	log.Printf("Game %s: started round %d with prompt %q (judge %q)", gm.code, state.Round, state.Prompt, state.JudgeId)
	return state, nil
}

// CurrentRoundState returns the full active-round snapshot (round 0 with zero
// values before the first StartRound). Used by the GET /round endpoint and the
// reconnect snapshot.
func (gm *Manager) CurrentRoundState() RoundState {
	gm.mux.RLock()
	defer gm.mux.RUnlock()
	return gm.roundStateLocked()
}

// OpenJudging is the judge's override: it closes submissions and unlocks
// flipping/picking before every player has answered (the automatic path is
// maybeOpenJudgingLocked, triggered by the last eligible Submit). It needs an
// active round with a judge and at least one note to judge.
func (gm *Manager) OpenJudging() error {
	gm.mux.Lock()

	var err error
	switch {
	case gm.round == 0:
		err = ErrNoActiveRound
	case gm.judge == "":
		err = ErrNoJudge
	case gm.judgingOpen:
		err = ErrJudgingAlreadyOpen
	case len(gm.submittedNotes) == 0:
		err = ErrNoNotesYet
	}
	if err != nil {
		gm.mux.Unlock()
		return err
	}

	gm.judgingOpen = true
	round := gm.round
	gm.mux.Unlock()

	gm.hub.broadcast(event{Type: "judging_ready", Round: round})
	log.Printf("Game %s: judging opened for round %d", gm.code, round)
	return nil
}

// FlipNote turns a note face-up (by its 1-based id) and broadcasts the flip so
// the judge's phone and the host screen stay in sync. Flips are one-way and
// idempotent. They unlock when judging opens — except in judge-less rounds
// (<2 players), where the host may flip freely as before.
func (gm *Manager) FlipNote(noteId int) error {
	gm.mux.Lock()

	if gm.round == 0 {
		gm.mux.Unlock()
		return ErrNoActiveRound
	}
	if gm.judge != "" && !gm.judgingOpen {
		gm.mux.Unlock()
		return ErrJudgingNotOpen
	}
	if noteId < 1 || noteId > len(gm.submittedNotes) {
		gm.mux.Unlock()
		return ErrUnknownNote
	}
	note := &gm.submittedNotes[noteId-1]
	changed := !note.flipped
	note.flipped = true
	round := gm.round
	gm.mux.Unlock()

	if changed {
		gm.hub.broadcast(event{Type: "note_flipped", Round: round, NoteId: noteId})
	}
	return nil
}

// PickFavorite records the judge's favorite note: its author scores a point,
// and the winner is announced to every client (favorite_picked plus a players
// roster refresh with the new scores). One favorite per round, and the note
// must already be face-up.
func (gm *Manager) PickFavorite(noteId int) (PlayerId, error) {
	gm.mux.Lock()

	var err error
	switch {
	case gm.round == 0:
		err = ErrNoActiveRound
	case gm.judge == "":
		err = ErrNoJudge
	case !gm.judgingOpen:
		err = ErrJudgingNotOpen
	case gm.winner != "":
		err = ErrFavoritePicked
	case noteId < 1 || noteId > len(gm.submittedNotes):
		err = ErrUnknownNote
	case !gm.submittedNotes[noteId-1].flipped:
		err = ErrNoteNotFlipped
	}
	if err != nil {
		gm.mux.Unlock()
		return "", err
	}

	gm.favoriteNote = noteId
	gm.winner = gm.submittedNotes[noteId-1].author
	gm.scores[gm.winner]++
	winner, round := gm.winner, gm.round
	roster := gm.rosterLocked()
	gm.mux.Unlock()

	gm.hub.broadcast(event{Type: "favorite_picked", Round: round, NoteId: noteId, WinnerId: winner})
	gm.hub.broadcast(event{Type: "players", Players: roster})
	log.Printf("Game %s: round %d favorite is note %d by %q", gm.code, round, noteId, winner)
	return winner, nil
}

// Hub exposes this game's WebSocket subscriber hub (used by the events handler).
func (gm *Manager) Hub() *hub {
	return gm.hub
}

func (gm *Manager) GetDrawnWordTiles(id PlayerId) ([]string, error) {
	gm.mux.Lock()
	defer gm.mux.Unlock()

	if !slices.Contains(gm.players, id) {
		return nil, fmt.Errorf("player %s not found", id)
	}

	words := make([]string, 0)
	for word, playerId := range gm.wordTiles {
		if playerId == id {
			words = append(words, word)
		}
	}
	return words, nil
}

func (gm *Manager) GetPlayers() []PlayerId {
	gm.mux.Lock()
	defer gm.mux.Unlock()
	return gm.players
}

// Roster returns the current players as wire-shaped Player values (id +
// running score). Used by GET /players and the "players" event snapshot.
func (gm *Manager) Roster() []Player {
	gm.mux.RLock()
	defer gm.mux.RUnlock()
	return gm.rosterLocked()
}

// GetSubmittedNotes returns the current round's notes as wire views: a stable
// 1-based id, the ordered token list (tile keys plus BreakToken markers), and
// whether the note has been flipped face-up. Notes come back sorted by their
// random per-note sort key, so the judge's phone and the host screen show the
// board in the same shuffled order without revealing who answered first.
// Authors are withheld — the reveal happens via the favorite_picked event.
func (gm *Manager) GetSubmittedNotes() []NoteView {
	gm.mux.RLock()
	defer gm.mux.RUnlock()

	views := make([]NoteView, len(gm.submittedNotes))
	for i, note := range gm.submittedNotes {
		views[i] = NoteView{Id: i + 1, Tokens: note.tokens, Flipped: note.flipped}
	}
	slices.SortFunc(views, func(a, b NoteView) int {
		ka, kb := gm.submittedNotes[a.Id-1].sortKey, gm.submittedNotes[b.Id-1].sortKey
		switch {
		case ka < kb:
			return -1
		case ka > kb:
			return 1
		default:
			return 0
		}
	})
	return views
}
