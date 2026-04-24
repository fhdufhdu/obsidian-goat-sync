package ws

import (
	"testing"
)

func TestHubRegisterUnregister(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	client := &Client{
		hub:  hub,
		send: make(chan []byte, 256),
	}

	hub.Register <- client

	hub.mu.RLock()
	if len(hub.clients) != 1 {
		t.Errorf("expected 1 client, got %d", len(hub.clients))
	}
	hub.mu.RUnlock()

	hub.Unregister <- client

	client2 := &Client{
		hub:  hub,
		send: make(chan []byte, 256),
	}
	hub.Register <- client2

	hub.mu.RLock()
	if len(hub.clients) != 1 {
		t.Errorf("expected 1 client after unregister+register, got %d", len(hub.clients))
	}
	hub.mu.RUnlock()
}

func TestHubBroadcastToVault(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	c1 := &Client{hub: hub, send: make(chan []byte, 256), vault: "personal"}
	c2 := &Client{hub: hub, send: make(chan []byte, 256), vault: "personal"}
	c3 := &Client{hub: hub, send: make(chan []byte, 256), vault: "work"}

	hub.Register <- c1
	hub.Register <- c2
	hub.Register <- c3

	hub.BroadcastToVault("personal", []byte("hello"), c1)

	msg := <-c2.send
	if string(msg) != "hello" {
		t.Errorf("expected 'hello', got '%s'", string(msg))
	}

	select {
	case m := <-c3.send:
		t.Errorf("c3 should not receive, got '%s'", string(m))
	default:
	}
}
