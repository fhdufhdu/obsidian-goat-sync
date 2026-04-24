package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/gorilla/websocket"

	"obsidian-sync/internal/config"
	"obsidian-sync/internal/dashboard"
	"obsidian-sync/internal/db"
	"obsidian-sync/internal/github"
	"obsidian-sync/internal/storage"
	"obsidian-sync/internal/ws"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func main() {
	cfg := config.Load()

	database, err := db.Open(cfg.DataDir + "/sync.db")
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}
	defer database.Close()

	queries := db.NewQueries(database)
	store := storage.New(cfg.DataDir)
	hub := ws.NewHub()
	go hub.Run()

	handler := ws.NewHandler(queries, store, hub)

	backup := github.NewBackupService(queries, store)
	go backup.Start()

	mux := http.NewServeMux()

	dash := dashboard.New(cfg, queries, store)
	dash.RegisterRoutes(mux)

	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("token")
		valid, err := queries.ValidateToken(token)
		if err != nil || !valid {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("upgrade error: %v", err)
			return
		}

		client := ws.NewClient(hub, conn, handler)
		hub.Register <- client
		go client.WritePump()
		go client.ReadPump()
	})

	addr := fmt.Sprintf(":%s", cfg.Port)
	log.Printf("Obsidian Sync running on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
