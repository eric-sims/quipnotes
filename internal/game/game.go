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
	players []PlayerId
	words   map[string]PlayerId
	mux     sync.RWMutex
}

// NewGameManager creates a new game manager instance
func NewGameManager() *Manager {
	return &Manager{
		players: make([]PlayerId, 0),
		words:   make(map[string]PlayerId),
		mux:     sync.RWMutex{},
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

	for word, playerId := range gm.words {
		if playerId == id {
			gm.words[word] = ""
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

func (gm *Manager) DrawWordsFromList(n int, id PlayerId) ([]string, error) {
	gm.mux.Lock()

	if n <= 0 || n > len(Game.words) {
		return nil, fmt.Errorf("invalid number of words requested: %d", n)
	}

	// Collect keys that haven't been set to true
	availableWords := make([]string, 0)
	for word, playerId := range Game.words {
		if playerId == "" {
			availableWords = append(availableWords, word)
		}
	}

	// Check if there are enough words available
	if len(availableWords) < n {
		return nil, fmt.Errorf("not enough words available, only %d left", len(availableWords))
	}

	// Shuffle the available words
	rand.Shuffle(len(availableWords), func(i, j int) {
		availableWords[i], availableWords[j] = availableWords[j], availableWords[i]
	})

	// Select the first `n` words from the shuffled list
	selectedWords := availableWords[:n]

	// Mark the selected words as retrieved in Game.words
	for _, word := range selectedWords {
		Game.words[word] = id
	}

	gm.mux.Unlock()
	// return the whole list so that the client can remain stateless
	return gm.GetWords(id)
}

// TurnInRansomNote - reads off the ransom note and puts the tiles back into the WordStore
func (gm *Manager) TurnInRansomNote(note []string, id PlayerId) error {
	gm.mux.Lock()
	defer gm.mux.Unlock()

	if !slices.Contains(gm.players, id) {
		log.Printf("Player %s not found in game", id)
		return fmt.Errorf("player %s not found", id)
	}

	log.Println("RECEIVED NOTE:")
	// TODO: Handle Ransom Note logic
	for _, word := range note {
		if len(strings.Split(word, "|")) < 2 {
			log.Println("Found word with wrong format")
			return fmt.Errorf("word %s not found in wrong format", word)
		}

		if gm.words[word] != id {
			return fmt.Errorf("word %s not part of your word pile", word)
		}

		log.Printf(strings.Split(word, "|")[1] + " ")

		gm.words[word] = ""
	}
	log.Println()
	return nil
}

func (gm *Manager) GetWords(id PlayerId) ([]string, error) {
	gm.mux.Lock()
	defer gm.mux.Unlock()

	if !slices.Contains(gm.players, id) {
		return nil, fmt.Errorf("player %s not found", id)
	}

	words := make([]string, 0)
	for word, playerId := range gm.words {
		if playerId == id {
			words = append(words, word)
		}
	}
	return words, nil
}
