package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"eric-sims/quipnotes/internal"

	"github.com/joho/godotenv"
)

func main() {
	err := godotenv.Load()
	if err != nil {
		panic(fmt.Sprintf("Error loading .env file: %s", err.Error()))
	}
	filePath := os.Getenv("WORDS_FILE_PATH")
	htmlDir := os.Getenv("HTML_DIR_PATH")

	fmt.Println("filePath", filePath)
	if err := internal.LoadWordsFromCSV(filePath); err != nil {
		panic(fmt.Sprintf("Failed to load words.csv: %s", err.Error()))
	}
	log.Printf("Loaded %d words.", len(internal.WordStore))

	internal.Game = internal.NewGameManager()

	http.Handle("/", http.FileServer(http.Dir(htmlDir)))
	http.HandleFunc("/ws", internal.HandleConnections)

	log.Println("Starting server on :8080")
	err = http.ListenAndServe(":8080", nil)
	if err != nil {
		panic(fmt.Sprintf("ListenAndServe: %s", err.Error()))
	}
}
