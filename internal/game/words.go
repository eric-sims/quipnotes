package game

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
)

// validPosTags is the closed set of part-of-speech markers a word line may
// carry. "other" holds anything that isn't a real part of speech (punctuation
// tiles, suffix fragments like "s"/"ing", articles).
var validPosTags = map[string]bool{
	"noun": true, "verb": true, "adjective": true, "adverb": true,
	"pronoun": true, "preposition": true, "conjunction": true,
	"interjection": true, "other": true,
}

// posFallback tags a word whose line carried no usable markers.
var posFallback = []string{"other"}

// LoadWordsFromFile reads the word bank — one word per line, prefixed by one or
// more part-of-speech markers, e.g. "[noun][verb] play". Blank lines and "#"
// comment lines are skipped (same conventions as the prompts file). It returns
// the base tile keys ("<index>|<word>", index = 0-based ordinal of accepted
// word lines) plus a tile-key → POS-tags map. The Registry holds both and
// copies the keys into each new game's pool, so every game starts with the
// full set.
//
// The parser is forgiving like the prompts loader: an unknown marker is
// dropped with a warning, a line with no valid markers defaults to ["other"],
// and a line that is only markers (no word) is skipped. Unlike prompts, words
// are required — an unreadable or empty file is an error (the caller panics).
func LoadWordsFromFile(filepath string) ([]string, map[string][]string, error) {
	file, err := os.Open(filepath)
	if err != nil {
		return nil, nil, fmt.Errorf("could not open words file: %v", err)
	}
	defer file.Close()

	tileKeys := make([]string, 0)
	tilePos := make(map[string][]string)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, commentPrefix) {
			continue
		}
		word, pos, ok := parseWordLine(line)
		if !ok {
			continue
		}
		key := fmt.Sprintf("%s|%s", strconv.Itoa(len(tileKeys)), word)
		tileKeys = append(tileKeys, key)
		tilePos[key] = pos
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, fmt.Errorf("could not read words file: %v", err)
	}
	if len(tileKeys) == 0 {
		return nil, nil, fmt.Errorf("words file %q had no usable lines", filepath)
	}

	log.Printf("Loaded %d wordTiles.", len(tileKeys))
	return tileKeys, tilePos, nil
}

// parseWordLine splits one trimmed, non-comment line into its word text and
// POS tags. Markers are the leading "[tag]" runs; the word is everything after
// them, trimmed. ok is false only when there is no word text to keep.
func parseWordLine(line string) (word string, pos []string, ok bool) {
	rest := line
	for strings.HasPrefix(rest, "[") {
		end := strings.Index(rest, "]")
		if end < 0 {
			break // an unclosed "[" belongs to the word itself (e.g. "[sic")
		}
		tag := strings.ToLower(strings.TrimSpace(rest[1:end]))
		if validPosTags[tag] {
			pos = append(pos, tag)
		} else {
			log.Printf("words file: dropping unknown marker %q in line %q", tag, line)
		}
		rest = strings.TrimLeft(rest[end+1:], " \t")
	}
	word = strings.TrimSpace(rest)
	if word == "" {
		log.Printf("words file: skipping marker-only line %q", line)
		return "", nil, false
	}
	if len(pos) == 0 {
		log.Printf("words file: line %q has no part-of-speech markers; tagging as %q", line, posFallback[0])
		pos = posFallback
	}
	return word, pos, true
}
