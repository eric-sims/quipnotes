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
// "players" event). Today it carries only the id; a Score field will be added
// when scoring lands, which is an additive, non-breaking change to the shape.
type Player struct {
	Id PlayerId `json:"id"`
}

// rosterLocked maps the internal players slice to the wire roster. The caller
// must hold gm.mux.
func rosterLocked(players []PlayerId) []Player {
	roster := make([]Player, len(players))
	for i, id := range players {
		roster[i] = Player{Id: id}
	}
	return roster
}

// Round/submit errors the router maps to 409 Conflict (a friendly, retryable
// condition) rather than a hard 500.
var (
	ErrNoActiveRound    = errors.New("no active round yet; wait for the host to draw a prompt")
	ErrAlreadySubmitted = errors.New("you already submitted a note this round")
)

// Manager holds the state for a single game. Many games run concurrently, each
// identified by its code and held in the Registry.
type Manager struct {
	code           string
	players        []PlayerId
	wordTiles      map[string]PlayerId
	submittedNotes [][]string // each note is its ordered token list (tiles + BreakToken)

	// Round / prompt state. A "round" is an active prompt: the host draws the
	// next prompt off promptDeck to start one. round is 0 until the first draw.
	promptDeck         []string          // shuffled copy of the registry prompts
	promptCursor       int               // index of the next prompt to draw
	round              int               // 0 = no round started; ++ each draw
	currentPrompt      string            // the active round's prompt ("" if none)
	submittedThisRound map[PlayerId]bool // players who submitted this round

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
	roster := rosterLocked(gm.players)

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
	roster := rosterLocked(gm.players)

	gm.mux.Unlock()

	// Broadcast the updated roster outside the lock (mirrors StartRound / AddPlayer).
	gm.hub.broadcast(event{Type: "players", Players: roster})
	log.Printf("Removed player: %+v\n", id)
	return nil
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
	defer gm.mux.Unlock()

	if !slices.Contains(gm.players, id) {
		log.Printf("Player %s not found in game", id)
		return fmt.Errorf("player %s not found", id)
	}

	// One note per round: a note only counts against an active round, and a
	// player may submit at most once until the host draws the next prompt.
	if gm.round == 0 {
		return ErrNoActiveRound
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

	// Store the normalized token list (edge/duplicate breaks collapsed) and
	// record that this player has answered the round.
	gm.submittedNotes = append(gm.submittedNotes, normalizeNote(note))
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
// new round: it clears the previous round's submitted notes and resets the
// per-round submission set. When the deck is exhausted it reshuffles and starts
// over, so a game never runs out of prompts. Returns the new round number and
// its prompt.
func (gm *Manager) StartRound() (int, string, error) {
	gm.mux.Lock()

	if len(gm.promptDeck) == 0 {
		gm.mux.Unlock()
		return 0, "", errors.New("no prompts available")
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
	gm.submittedNotes = make([][]string, 0)
	round, prompt := gm.round, gm.currentPrompt

	gm.mux.Unlock()

	// Push the new round to every connected client. Broadcast outside the lock
	// so a slow subscriber can't stall game state.
	gm.hub.broadcast(event{Type: "round_started", Round: round, Prompt: prompt})
	log.Printf("Game %s: started round %d with prompt %q", gm.code, round, prompt)
	return round, prompt, nil
}

// CurrentRound returns the active round number and prompt (0 / "" before the
// first StartRound). Used by the GET /round endpoint and the reconnect snapshot.
func (gm *Manager) CurrentRound() (int, string) {
	gm.mux.RLock()
	defer gm.mux.RUnlock()
	return gm.round, gm.currentPrompt
}

// RoundSubmissionStatus reports the current round plus how many of the game's
// players have submitted a note this round, for the live host indicator.
func (gm *Manager) RoundSubmissionStatus() (round, count, total int) {
	gm.mux.RLock()
	defer gm.mux.RUnlock()
	return gm.round, len(gm.submittedThisRound), len(gm.players)
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

// Roster returns the current players as wire-shaped Player values (id today,
// score later). Used by GET /players and the "players" event snapshot.
func (gm *Manager) Roster() []Player {
	gm.mux.RLock()
	defer gm.mux.RUnlock()
	return rosterLocked(gm.players)
}

// GetSubmittedNotes returns the current round's notes, each as its ordered
// token list (tile keys plus BreakToken markers). The host parses each token at
// its boundary; see quipnotesmanager/src/components/NoteSlate.vue.
func (gm *Manager) GetSubmittedNotes() [][]string {
	gm.mux.Lock()
	defer gm.mux.Unlock()

	return gm.submittedNotes
}

