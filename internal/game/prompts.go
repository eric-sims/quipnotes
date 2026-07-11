package game

import (
	"bufio"
	"log"
	"os"
	"strings"
)

// Prompt is one round prompt plus its content rating. FamilyFriendly prompts are
// safe to serve when a game is created in "family friendly" mode; adult prompts
// (overly explicit, suggestive, or sexual) are FamilyFriendly=false and appear
// only in games that did not request family-friendly mode.
type Prompt struct {
	Text           string
	FamilyFriendly bool
}

// adultMarker flags a prompt line as adult (not family-friendly). It sits at the
// very start of the line; the loader strips it (and any following whitespace) to
// recover the prompt text. Every other non-comment line is family-friendly, so
// annotating the file only means prefixing the handful of adult prompts.
const adultMarker = "[adult]"

// commentPrefix starts a comment line in the prompt file (skipped by the loader),
// so the file can carry a header explaining the [adult] convention.
const commentPrefix = "#"

// defaultPromptTexts is a built-in fallback prompt bank. Unlike the word list
// (required; panics if missing), prompts fall back to these so the server always
// boots with a playable game even without PROMPTS_FILE_PATH. They are all tame,
// so defaultPrompts() rates every one family-friendly.
var defaultPromptTexts = []string{
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

// defaultPrompts wraps the built-in bank as Prompts. Every default is
// family-friendly, so a family-friendly game can always fall back to these.
func defaultPrompts() []Prompt {
	prompts := make([]Prompt, len(defaultPromptTexts))
	for i, text := range defaultPromptTexts {
		prompts[i] = Prompt{Text: text, FamilyFriendly: true}
	}
	return prompts
}

// LoadPromptsFromFile reads a prompt file — one prompt per line, blank lines and
// surrounding whitespace ignored (line-based rather than CSV so prompts may
// freely contain commas). A line starting with "#" is a comment (skipped). A
// line starting with the "[adult]" marker is rated adult (not family-friendly);
// the marker is stripped to recover the prompt text. Every other line is
// family-friendly. On any failure, or an empty/unset path, it logs a warning and
// returns the built-in defaults so the server never fails to start over prompts.
func LoadPromptsFromFile(filepath string) []Prompt {
	if strings.TrimSpace(filepath) == "" {
		log.Println("PROMPTS_FILE_PATH not set; using built-in default prompts")
		return defaultPrompts()
	}

	file, err := os.Open(filepath)
	if err != nil {
		log.Printf("could not open prompts file %q: %v; using built-in defaults", filepath, err)
		return defaultPrompts()
	}
	defer file.Close()

	prompts := make([]Prompt, 0)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, commentPrefix) {
			continue
		}
		prompts = append(prompts, parsePromptLine(line))
	}
	if err := scanner.Err(); err != nil {
		log.Printf("error reading prompts file %q: %v; using built-in defaults", filepath, err)
		return defaultPrompts()
	}

	// Drop lines that were only a marker with no prompt text after it.
	prompts = slicesFilter(prompts, func(p Prompt) bool { return p.Text != "" })
	if len(prompts) == 0 {
		log.Printf("prompts file %q had no usable lines; using built-in defaults", filepath)
		return defaultPrompts()
	}

	familyFriendly := 0
	for _, p := range prompts {
		if p.FamilyFriendly {
			familyFriendly++
		}
	}
	log.Printf("Loaded %d prompts (%d family-friendly, %d adult).", len(prompts), familyFriendly, len(prompts)-familyFriendly)
	return prompts
}

// parsePromptLine turns a single trimmed, non-comment line into a Prompt: an
// "[adult]"-prefixed line becomes an adult prompt (marker stripped), anything
// else a family-friendly one.
func parsePromptLine(line string) Prompt {
	if len(line) >= len(adultMarker) && strings.EqualFold(line[:len(adultMarker)], adultMarker) {
		return Prompt{Text: strings.TrimSpace(line[len(adultMarker):]), FamilyFriendly: false}
	}
	return Prompt{Text: line, FamilyFriendly: true}
}

// slicesFilter returns the elements of in for which keep is true, preserving
// order. A tiny local helper so the loader reads cleanly.
func slicesFilter(in []Prompt, keep func(Prompt) bool) []Prompt {
	out := in[:0]
	for _, p := range in {
		if keep(p) {
			out = append(out, p)
		}
	}
	return out
}
