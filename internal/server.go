package internal

import (
	"log"
	"net/http"

	"github.com/gorilla/websocket"
)

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
		var msg map[string]interface{}
		err := conn.ReadJSON(&msg)
		if err != nil {
			log.Println("Error reading JSON:", err)
			break
		}

		// Check for "command" field to determine action type
		if cmd, ok := msg["command"].(string); ok {
			switch cmd {
			case "draw_words":
				Game.DrawWordsFromList(5, player)
			case "turn_in_ransom_note":
				// list of words
				note := msg["note"].([]interface{})
				Game.TurnInRansomNote(note, player)
			case "trade_words":
				// given a set of words, n number of words from the pile can be exchanged
				words := msg["words"].([]interface{})
				Game.TradeWords(words, player)
			default:
				err := conn.WriteMessage(websocket.TextMessage, []byte("could not find message"))
				if err != nil {
					log.Println("Error writing message:", err)
				}
				continue
			}
		}

		wdMsg, err := JsonEncode(player.wordsDrawn)
		if err != nil {
			log.Println(err)
		}
		err = player.Conn.WriteMessage(websocket.TextMessage, wdMsg)
		if err != nil {
			log.Println(err)
		}
	}
}
