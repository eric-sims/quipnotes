package internal

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/gorilla/websocket"
)

type Message struct {
	Command  string   `json:"command"`
	Words    []string `json:"words"`
	Count    *int     `json:"count,omitempty"`
	PlayerId string   `json:"playerId"`
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

	// Process incoming messages in a loop
	for {
		var message Message
		_, receivedBytes, err := conn.ReadMessage()
		if err != nil {
			log.Println("error, closing connection:", err)
			break
		}

		if err := json.Unmarshal(receivedBytes, &message); err != nil {
			log.Println("Could not unmarshal message")
			continue
		}

		playerID := message.PlayerId
		player, ok := Game.players[playerID]
		if !ok {
			player = &Player{
				ID: playerID,
			}
			Game.AddPlayer(player)
			defer Game.RemovePlayer(playerID)
		}

		// Check for "command" field to determine action type
		switch message.Command {
		case "draw_words":
			count := message.Count
			if count != nil {
				Game.DrawWordsFromList(*count, player)
				log.Printf("Player's new word list: %+v\n", player.wordsDrawn)
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

		err = conn.WriteMessage(websocket.TextMessage, wdMsg)
		if err != nil {
			log.Println(err)
		}
	}
}
