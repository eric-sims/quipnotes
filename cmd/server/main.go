package main

import (
	"fmt"
	"log"
	"net/http"

	"eric-sims/quipnotes/internal"
)

func main() {
	// Load words from the data file
	if err := internal.LoadWordsFromCSV("./data/words.csv"); err != nil {
		panic(fmt.Sprintf("Failed to load words.txt: %s", err.Error()))
	}
	log.Printf("Loaded %d words.", len(internal.WordStore))

	// initialize the Game
	internal.Game = internal.NewGameManager()

	// Setup HTTP endpoints
	http.Handle("/", http.FileServer(http.Dir("./static/word-tile-game/build")))
	http.HandleFunc("/ws", internal.HandleConnections)

	// Start server
	log.Println("Starting server on :8080")
	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		panic(fmt.Sprintf("ListenAndServe: %s", err.Error()))
	}
}
