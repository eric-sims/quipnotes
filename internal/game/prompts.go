package game

import (
	"bufio"
	"log"
	"os"
	"slices"
	"strings"
)

// defaultPrompts is a built-in fallback prompt bank. Unlike the word list
// (which is required and panics if missing), prompts fall back to these so the
// server always boots with a playable game even without PROMPTS_FILE_PATH.
var defaultPrompts = []string{
	"The worst possible thing to say on a first date",
	"A rejected slogan for an energy drink",
	"What the villain monologues about before losing",
	"A note left on the fridge by a very passive-aggressive roommate",
	"The real reason the wifi is down",
	"A terrible name for a boat",
	"What your pet is really thinking about you",
	"An honest warning label for adulthood",
	"The last text message before the world ended",
	"A motivational poster nobody asked for",
	"What the fortune cookie should have said",
	"A confession whispered to a houseplant",
	"The worst superpower to have at a wedding",
	"A newspaper headline from the year 3000",
	"What the GPS says when it gives up on you",
	"A ransom note for something completely worthless",
	"The title of your unauthorized autobiography",
	"An excuse for being late that no one believes",
	"What the toaster would say if it could talk",
	"A bad piece of advice disguised as wisdom",
}

// LoadPromptsFromFile reads a prompt file — one prompt per line, with blank
// lines and surrounding whitespace ignored (line-based rather than CSV so
// prompts may freely contain commas). On any failure, or an empty/unset path,
// it logs a warning and returns the built-in defaultPrompts so the server never
// fails to start over prompts.
func LoadPromptsFromFile(filepath string) []string {
	if strings.TrimSpace(filepath) == "" {
		log.Println("PROMPTS_FILE_PATH not set; using built-in default prompts")
		return slices.Clone(defaultPrompts)
	}

	file, err := os.Open(filepath)
	if err != nil {
		log.Printf("could not open prompts file %q: %v; using built-in defaults", filepath, err)
		return slices.Clone(defaultPrompts)
	}
	defer file.Close()

	prompts := make([]string, 0)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			prompts = append(prompts, line)
		}
	}
	if err := scanner.Err(); err != nil {
		log.Printf("error reading prompts file %q: %v; using built-in defaults", filepath, err)
		return slices.Clone(defaultPrompts)
	}
	if len(prompts) == 0 {
		log.Printf("prompts file %q had no usable lines; using built-in defaults", filepath)
		return slices.Clone(defaultPrompts)
	}

	log.Printf("Loaded %d prompts.", len(prompts))
	return prompts
}
