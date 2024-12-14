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

var Game *Manager

type PlayerId string
type Manager struct {
	players        []PlayerId
	wordTiles      map[string]PlayerId
	submittedNotes []string
	mux            sync.RWMutex
}

// NewGameManager creates a new game manager instance
func NewGameManager() *Manager {
	return &Manager{
		players:   make([]PlayerId, 0),
		wordTiles: make(map[string]PlayerId),
		mux:       sync.RWMutex{},
	}
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
	slices.Delete(gm.players, index, index+1)

	log.Printf("Removed player: %+v\n", id)
	return nil
}

func (gm *Manager) DrawWordTiles(n int, id PlayerId) ([]string, error) {
	gm.mux.Lock()

	if n <= 0 || n > len(Game.wordTiles) {
		return nil, fmt.Errorf("invalid number of wordTiles requested: %d", n)
	}

	// Collect keys that haven't been set to true
	availableWords := make([]string, 0)
	for word, playerId := range Game.wordTiles {
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

	// Mark the selected wordTiles as retrieved in Game.wordTiles
	for _, word := range selectedWords {
		Game.wordTiles[word] = id
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

	// Add to submittedNotes
	gm.submittedNotes = append(gm.submittedNotes, sb.String())

	return nil
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

func (gm *Manager) GetSubmitted() []string {
	gm.mux.Lock()
	defer gm.mux.Unlock()

	return gm.submittedNotes
}

func (gm *Manager) DeleteSubmitted() {
	gm.mux.Lock()
	defer gm.mux.Unlock()

	gm.submittedNotes = make([]string, 0)
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

func (gm *Manager) ClearSubmitted() {
	gm.mux.Lock()
	defer gm.mux.Unlock()

	gm.submittedNotes = make([]string, 0)
}
