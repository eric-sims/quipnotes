package internal

import (
	"fmt"
	"log"
	"strings"
	"sync"
)

var Game *GameManager

type GameManager struct {
	players map[string]*Player
	mux     sync.RWMutex
	// add channels for message routing, events, etc.
}

// NewGameManager creates a new game manager instance
func NewGameManager() *GameManager {
	return &GameManager{
		players: make(map[string]*Player),
	}
}

func (gm *GameManager) AddPlayer(player *Player) {
	gm.mux.Lock()
	defer gm.mux.Unlock()
	gm.players[player.ID] = player
	fmt.Printf("Added player: %+v\n", player)
}

func (gm *GameManager) RemovePlayer(id string) {
	gm.mux.Lock()
	defer gm.mux.Unlock()
	for _, s := range gm.players[id].wordsDrawn {
		gm.players[id].RemoveWord(s)
	}

	// What if a web socket connection randomly disconnects!
	//delete(gm.players, id)
}

func (gm *GameManager) TradeWords(words []any, player *Player) {
	gm.mux.Lock()
	defer gm.mux.Unlock()

	data := convertWords(words)

	for _, word := range data {
		player.RemoveWord(word)
	}

	newWords, err := RetrieveNWords(len(words))
	if err != nil {
		log.Println(err)
	}
	player.AddWords(newWords)
}

func (gm *GameManager) DrawWordsFromList(n int, player *Player) {
	words, err := RetrieveNWords(n)
	if err != nil {
		log.Println(err)
	}
	player.AddWords(words)
}

// TurnInRansomNote - reads off the ransom note and puts the tiles back into the WordStore
func (gm *GameManager) TurnInRansomNote(note []any, player *Player) {
	data := convertWords(note)
	fmt.Println("RECEIVED NOTE:")
	// TODO: Handle Ransom Note logic
	for _, word := range data {
		if len(strings.Split(word, "|")) < 2 {
			log.Println("Found word with wrong format")
			continue
		}

		fmt.Printf(strings.Split(word, "|")[1] + " ")

		player.RemoveWord(word) // TODO: Possible bug. Players can draw words that have been turned in.
		WordStore[word] = false
	}
	fmt.Println()
}

func convertWords(words []any) []string {
	var data []string
	for _, w := range words {
		data = append(data, fmt.Sprintf("%v", w))
	}

	return data
}
