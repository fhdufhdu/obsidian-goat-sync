package main

import (
	"log"
	"net/http"

	"obsidian-goat-sync/internal/sqlite"
	"obsidian-goat-sync/internal/ws"

	"github.com/gorilla/websocket"
)

var (
	upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin:     func(r *http.Request) bool { return true },
	}

	clientManager = ws.NewClientManager()
)

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
	conn, connErr := sqlite.Open("./sync.db")
	if connErr != nil {
		log.Fatalf("Server failed to start: %v", connErr)
		return
	}
	defer conn.Close()

	go clientManager.Run()
	http.HandleFunc("/", ConnectWebSocketClient)
	httpErr := http.ListenAndServe(":8080", nil)
	if httpErr != nil {
		log.Fatalf("Server failed to start: %v", httpErr)
	}
}
