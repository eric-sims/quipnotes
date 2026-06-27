package game

import (
	"encoding/csv"
	"fmt"
	"log"
	"os"
	"strconv"
)

// LoadWordsFromCSV reads a single-column, header-less CSV and returns the base
// list of tile keys ("<rowIndex>|<word>"). The Registry holds these and copies
// them into each new game's pool, so every game starts with the full set.
func LoadWordsFromCSV(filepath string) ([]string, error) {
	// Open the CSV file
	file, err := os.Open(filepath)
	if err != nil {
		return nil, fmt.Errorf("could not open file: %v", err)
	}
	defer file.Close()

	// Create a new CSV reader
	reader := csv.NewReader(file)

	// Read all rows
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("could not read CSV: %v", err)
	}

	// Encode each row as a "<rowIndex>|<word>" tile key. The index prefix keeps
	// duplicate words distinct.
	tileKeys := make([]string, 0, len(records))
	for i, record := range records {
		tileKeys = append(tileKeys, fmt.Sprintf("%s|%s", strconv.Itoa(i), record[0]))
	}

	log.Printf("Loaded %d wordTiles.", len(tileKeys))
	return tileKeys, nil
}
