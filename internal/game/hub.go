package game

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// event is a server->client message pushed over the WebSocket. Fields are
// omitempty so each event type only carries what it needs:
//   - round_started: {round, prompt}
//   - submission:    {round, count, total}
//   - players:       {players: [{id}]}  (roster; also a snapshot on connect)
//   - game_ended:    {}
type event struct {
	Type    string   `json:"type"`
	Round   int      `json:"round,omitempty"`
	Prompt  string   `json:"prompt,omitempty"`
	Count   int      `json:"count,omitempty"`
	Total   int      `json:"total,omitempty"`
	Players []Player `json:"players,omitempty"`
}

// wsClient is one connected subscriber. The hub writes serialized events to
// send; the client's write pump drains it to the socket.
type wsClient struct {
	send chan []byte
}

// hub is a per-game set of WebSocket subscribers with a simple broadcast. It is
// guarded by a mutex rather than a run-loop goroutine: traffic is low (a few
// events per round) and games come and go, so keeping no background goroutine
// per game is simpler and leak-free.
type hub struct {
	mu      sync.Mutex
	clients map[*wsClient]struct{}
	closed  bool
}

func newHub() *hub {
	return &hub{clients: make(map[*wsClient]struct{})}
}

// register adds a client. It returns false if the hub has already been closed
// (the game ended), so the caller can drop the connection.
func (h *hub) register(c *wsClient) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return false
	}
	h.clients[c] = struct{}{}
	return true
}

// unregister removes a client and closes its send channel (signaling its write
// pump to exit). Safe to call multiple times: it is a no-op if the client was
// already removed (e.g. dropped by broadcast or closeAll).
func (h *hub) unregister(c *wsClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.clients[c]; ok {
		delete(h.clients, c)
		close(c.send)
	}
}

// broadcast serializes e and delivers it to every subscriber. A client whose
// buffer is full is dropped rather than allowed to stall the broadcast.
func (h *hub) broadcast(e event) {
	payload, err := json.Marshal(e)
	if err != nil {
		log.Printf("hub: could not marshal event: %v", err)
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		select {
		case c.send <- payload:
		default:
			delete(h.clients, c)
			close(c.send)
		}
	}
}

// closeAll drops every subscriber and marks the hub closed so no new client can
// register. Called when the game ends.
func (h *hub) closeAll() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	for c := range h.clients {
		delete(h.clients, c)
		close(c.send)
	}
}

// clientCount is the number of live subscribers (used by tests).
func (h *hub) clientCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}

// --- WebSocket connection handling ---

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	sendBufferSize = 16
)

var upgrader = websocket.Upgrader{
	// Dev posture: allow any origin, matching the server's CORS "*".
	CheckOrigin: func(r *http.Request) bool { return true },
}

// serveEvents upgrades the request to a WebSocket, registers it with the game's
// hub, sends the current-round snapshot, and runs the read/write pumps until the
// socket closes.
func serveEvents(gm *Manager, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws upgrade failed: %v", err)
		return
	}

	client := &wsClient{send: make(chan []byte, sendBufferSize)}
	if !gm.hub.register(client) {
		conn.Close()
		return
	}

	// Immediately send the current round so a late joiner / phone refresh gets
	// the active prompt without waiting for the next broadcast.
	if round, prompt := gm.CurrentRound(); round > 0 {
		if payload, err := json.Marshal(event{Type: "round_started", Round: round, Prompt: prompt}); err == nil {
			select {
			case client.send <- payload:
			default:
			}
		}
	}

	// Also snapshot the current roster so a connecting/reconnecting host sees who
	// has already joined without waiting for the next join/leave broadcast.
	if roster := gm.Roster(); len(roster) > 0 {
		if payload, err := json.Marshal(event{Type: "players", Players: roster}); err == nil {
			select {
			case client.send <- payload:
			default:
			}
		}
	}

	go writePump(conn, client)
	readPump(conn, gm.hub, client)
}

// readPump drains inbound frames (clients don't send meaningful messages; this
// exists to process control frames / pongs and detect disconnect). On any error
// it unregisters the client and closes the socket.
func readPump(conn *websocket.Conn, h *hub, c *wsClient) {
	defer func() {
		h.unregister(c)
		conn.Close()
	}()
	conn.SetReadLimit(512)
	_ = conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(pongWait))
	})
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
	}
}

// writePump delivers queued events and sends periodic pings to keep the
// connection alive. It exits when the hub closes the send channel or on any
// write error, closing the socket so the read pump unblocks promptly.
func writePump(conn *websocket.Conn, c *wsClient) {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		conn.Close()
	}()
	for {
		select {
		case msg, ok := <-c.send:
			_ = conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// The hub closed the channel: tell the client and stop.
				_ = conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
				return
			}
			if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			_ = conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
