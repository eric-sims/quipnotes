package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"eric-sims/quipnotes/internal"

	"github.com/joho/godotenv"
)

var (
	filePath = os.Getenv("WORDS_FILE_PATH")
	htmlDir  = os.Getenv("HTML_DIR_PATH")
)

func main() {
	// Load environment variables from .env file
	err := godotenv.Load()
	if err != nil {
		panic(fmt.Sprintf("Error loading .env file: %s", err.Error()))
	}

	// Load words from the data file

	fmt.Println("filePath", filePath)
	if err := internal.LoadWordsFromCSV(filePath); err != nil {
		panic(fmt.Sprintf("Failed to load words.csv: %s", err.Error()))
	}
	log.Printf("Loaded %d words.", len(internal.WordStore))

	// initialize the Game
	internal.Game = internal.NewGameManager()

	// Setup HTTP endpoints
	http.Handle("/", http.FileServer(http.Dir(htmlDir)))
	http.HandleFunc("/ws", internal.HandleConnections)

	// Start server
	log.Println("Starting server on :8080")
	err = http.ListenAndServe(":8080", nil)
	if err != nil {
		panic(fmt.Sprintf("ListenAndServe: %s", err.Error()))
	}
}
