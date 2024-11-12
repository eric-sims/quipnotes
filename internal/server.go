package internal

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/gorilla/websocket"
)

type Message struct {
	Command string   `json:"command"`
	Words   []string `json:"words"`
	Count   *int     `json:"count,omitempty"`
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// HandleConnections processes incoming WebSocket connections and messages
func HandleConnections(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Failed to upgrade to WebSocket:", err)
		return
	}

	var player *Player
	playerID := r.URL.Query().Get("playerID")
	player, ok := Game.players[playerID]
	if !ok {
		player = &Player{
			ID:   playerID,
			Conn: conn,
		}
	} else {
		player.Conn = conn
	}

	Game.AddPlayer(player)
	defer Game.RemovePlayer(playerID)

	// Process incoming messages in a loop
	for {
		var message Message
		_, receivedBytes, err := conn.ReadMessage()
		if err != nil {
			log.Println("Could not read message")
			continue
		}

		if err := json.Unmarshal(receivedBytes, &message); err != nil {
			log.Println("Could not unmarshal message")
			continue
		}

		// Check for "command" field to determine action type
		switch message.Command {
		case "draw_words":
			count := message.Count
			if count != nil {
				Game.DrawWordsFromList(*count, player)
			} else {
				log.Println("Could not draw words!")
			}
		case "turn_in_ransom_note":
			// list of words
			Game.TurnInRansomNote(message.Words, player)
		case "trade_words":
			// given a set of words, n number of words from the pile can be exchanged
			Game.TradeWords(message.Words, player)
		default:
			err := conn.WriteMessage(websocket.TextMessage, []byte("could not find message"))
			if err != nil {
				log.Println("Error writing message:", err)
			}
			continue
		}

		wdMsg, err := JsonEncode(player.wordsDrawn)
		if err != nil {
			log.Println(err)
		}
		err = player.Conn.WriteMessage(websocket.TextMessage, wdMsg)
		if err != nil {
			log.Println(err)
			log.Fatal()
		}
	}
}
