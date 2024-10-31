package internal

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"strconv"
)

type Words map[string]bool

var WordStore Words

// LoadWordsFromCSV - Function to load words from a CSV file into the global Words structure
func LoadWordsFromCSV(filepath string) error {
	// Initialize the global Words structure
	WordStore = make(map[string]bool)

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
		WordStore[fmt.Sprintf("%s|%s", strconv.Itoa(i), record[0])] = false
	}

	return nil
}

// RetrieveNWords - returns a random subset of WordStore, and sets those map values to true
func RetrieveNWords(n int) ([]string, error) {
	//TODO: add mutex locks and unlocks
	if n <= 0 || n > len(WordStore) {
		return nil, fmt.Errorf("invalid number of words requested: %d", n)
	}

	// Collect keys that haven't been set to true
	availableWords := make([]string, 0)
	for word, retrieved := range WordStore {
		if !retrieved {
			availableWords = append(availableWords, word)
		}
	}

	// Check if there are enough words available
	if len(availableWords) < n {
		return nil, fmt.Errorf("not enough words available, only %d left", len(availableWords))
	}

	// Shuffle the available words
	rand.Shuffle(len(availableWords), func(i, j int) {
		availableWords[i], availableWords[j] = availableWords[j], availableWords[i]
	})

	// Select the first `n` words from the shuffled list
	selectedWords := availableWords[:n]

	// Mark the selected words as retrieved in WordStore
	for _, word := range selectedWords {
		WordStore[word] = true
	}

	return selectedWords, nil
}

// JsonEncode - encodes a []string to JSON []byte
func JsonEncode(data []string) ([]byte, error) {
	encodedData, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("error encoding JSON: %v", err)
	}
	return encodedData, nil
}
