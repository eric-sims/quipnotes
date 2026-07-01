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
	submittedNotes []string

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
	defer gm.mux.Unlock()

	if strings.TrimSpace(string(id)) == "" {
		return errors.New("invalid player id")
	}

	if slices.Contains(gm.players, id) {
		return errors.New("cannot add player. id already exists")
	}
	gm.players = append(gm.players, id)

	log.Printf("Added player: %+v\n", id)
	return nil
}

func (gm *Manager) RemovePlayer(id PlayerId) error {
	gm.mux.Lock()
	defer gm.mux.Unlock()

	for word, playerId := range gm.wordTiles {
		if playerId == id {
			gm.wordTiles[word] = ""
		}
	}

	index := slices.Index(gm.players, id)
	if index == -1 {
		return errors.New("cannot remove player. id does not exist")
	}
	gm.players = slices.Delete(gm.players, index, index+1)
	delete(gm.submittedThisRound, id)

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

// Submit - reads off the ransom note and puts the tiles back into the WordStore
func (gm *Manager) Submit(note []string, id PlayerId) error {
	gm.mux.Lock()
	defer gm.mux.Unlock()

	sb := strings.Builder{}
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

	fmt.Println("RECEIVED NOTE:")
	// Verification Loop
	for _, word := range note {
		if len(strings.Split(word, "|")) < 2 {
			log.Println("Found word with wrong format")
			return fmt.Errorf("word %s not found in wrong format", word)
		}

		if gm.wordTiles[word] != id {
			return fmt.Errorf("word %s not part of your word pile", word)
		}

		legibleWord := strings.Split(word, "|")[1] + " "
		fmt.Printf(legibleWord)
		sb.WriteString(legibleWord)
	}
	fmt.Println()
	sb.WriteString("\n")

	if strings.TrimSpace(sb.String()) == "" {
		return fmt.Errorf("no wordTiles found")
	}

	// Need to loop through a second time because if it errors out in the first loop, we want the
	// player to keep his/her wordTiles
	for _, word := range note {
		gm.wordTiles[word] = ""
	}

	// Add to submittedNotes and record that this player has answered the round.
	gm.submittedNotes = append(gm.submittedNotes, sb.String())
	gm.submittedThisRound[id] = true

	return nil
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
	gm.submittedNotes = make([]string, 0)
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

func (gm *Manager) GetSubmittedNotes() []string {
	gm.mux.Lock()
	defer gm.mux.Unlock()

	return gm.submittedNotes
}

