package main

import (
	"log"
	"net/http"

	"obsidian-goat-sync/internal/ws"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

var hub = ws.NewHub()

func ConnectWebSocketClient(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Failed to upgrade connection: %v", err)
		return
	}

	client := ws.NewClient(hub, conn)
	hub.AddClinet(client)

	// Allow collection of old messages and prevent too many messages
	// from filling the Websocket send buffer.
	go client.ReadPump()
	go client.WritePump()
}

func main() {
	go hub.ManageClient()
	http.HandleFunc("/", ConnectWebSocketClient)
	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}
