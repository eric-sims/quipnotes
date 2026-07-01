package game

import (
	"encoding/json"
	"testing"
)

// newTestClient registers a fresh subscriber on the hub and returns it.
func newTestClient(h *hub) *wsClient {
	c := &wsClient{send: make(chan []byte, sendBufferSize)}
	h.register(c)
	return c
}

func TestHubBroadcastReachesAllClients(t *testing.T) {
	h := newHub()
	a := newTestClient(h)
	b := newTestClient(h)

	h.broadcast(event{Type: "round_started", Round: 1, Prompt: "hello"})

	for name, c := range map[string]*wsClient{"a": a, "b": b} {
		select {
		case payload := <-c.send:
			var e event
			if err := json.Unmarshal(payload, &e); err != nil {
				t.Fatalf("client %s: bad payload: %v", name, err)
			}
			if e.Type != "round_started" || e.Round != 1 || e.Prompt != "hello" {
				t.Fatalf("client %s: unexpected event %+v", name, e)
			}
		default:
			t.Fatalf("client %s did not receive the broadcast", name)
		}
	}
}

func TestHubUnregisterStopsDelivery(t *testing.T) {
	h := newHub()
	c := newTestClient(h)

	h.unregister(c)
	if h.clientCount() != 0 {
		t.Fatalf("expected 0 clients after unregister, got %d", h.clientCount())
	}

	// Broadcasting must not panic (would happen if it wrote to the closed
	// channel) and the client receives nothing.
	h.broadcast(event{Type: "round_started", Round: 2})
	if _, ok := <-c.send; ok {
		t.Fatal("expected the unregistered client's channel to be closed and empty")
	}
}

func TestHubCloseAllRejectsNewClients(t *testing.T) {
	h := newHub()
	c := newTestClient(h)

	h.closeAll()
	if h.clientCount() != 0 {
		t.Fatalf("expected 0 clients after closeAll, got %d", h.clientCount())
	}
	// The existing client's channel is closed.
	if _, ok := <-c.send; ok {
		t.Fatal("expected closeAll to close the client channel")
	}
	// A new registration is refused once the hub is closed.
	if h.register(&wsClient{send: make(chan []byte, 1)}) {
		t.Fatal("expected register to fail after closeAll")
	}
}
