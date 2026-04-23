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

var clientManager = ws.NewClientManager()

func ConnectWebSocketClient(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Failed to upgrade connection: %v", err)
		return
	}

	client := ws.NewClient(clientManager, conn)
	clientManager.Add(client)

	go client.ReadPump()
	go client.WritePump()
}

func main() {
	go clientManager.Run()
	http.HandleFunc("/", ConnectWebSocketClient)
	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}
