package game

import (
	"encoding/csv"
	"fmt"
	"log"
	"os"
	"strconv"
)

// LoadWordsFromCSV - Function to load words from a CSV file into the global Words structure
func LoadWordsFromCSV(filepath string) error {
	// Open the CSV file
	file, err := os.Open(filepath)
	if err != nil {
		return fmt.Errorf("could not open file: %v", err)
	}
	defer file.Close()

	// Create a new CSV reader
	reader := csv.NewReader(file)

	// Read all rows
	records, err := reader.ReadAll()
	if err != nil {
		return fmt.Errorf("could not read CSV: %v", err)
	}

	// Iterate through each record, encoding "word|type" and storing in the map
	for i, record := range records {
		Game.words[fmt.Sprintf("%s|%s", strconv.Itoa(i), record[0])] = ""
	}

	log.Printf("Loaded %d words.", len(Game.words))
	return nil
}
