package internal

import (
	"fmt"
	"log"
	"sync"

	"github.com/gorilla/websocket"
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
	fmt.Println("Added player: %+V", player)
}

func (gm *GameManager) RemovePlayer(id string) {
	gm.mux.Lock()
	defer gm.mux.Unlock()
	//delete(gm.players, id)
	// What if a web socket connection randomly disconnects!
}

func (gm *GameManager) Broadcast(message []byte) {
	gm.mux.RLock()
	defer gm.mux.RUnlock()
	for _, player := range gm.players {
		err := player.Conn.WriteMessage(websocket.TextMessage, message)
		if err != nil {
			log.Println(err)
			return
		}
	}
}
