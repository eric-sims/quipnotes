package game

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// writeTempPrompts writes lines to a temp prompt file and returns its path.
func writeTempPrompts(t *testing.T, lines string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "prompts.txt")
	if err := os.WriteFile(path, []byte(lines), 0o600); err != nil {
		t.Fatalf("write temp prompts: %v", err)
	}
	return path
}

func TestLoadPromptsParsesRatingsCommentsAndBlanks(t *testing.T) {
	path := writeTempPrompts(t, `# header comment explaining the format
Describe the solar system

  Explain how vaccines work
[adult] Explain the female orgasm
[ADULT]   Whisper something seductive to your date
# trailing comment
[adult]
`)

	prompts := LoadPromptsFromFile(path)

	want := []Prompt{
		{Text: "Describe the solar system", FamilyFriendly: true},
		{Text: "Explain how vaccines work", FamilyFriendly: true},
		{Text: "Explain the female orgasm", FamilyFriendly: false},
		{Text: "Whisper something seductive to your date", FamilyFriendly: false},
	}
	if !slices.Equal(prompts, want) {
		t.Fatalf("parsed prompts mismatch:\n got %+v\nwant %+v", prompts, want)
	}
}

func TestLoadPromptsFallsBackToFamilyFriendlyDefaults(t *testing.T) {
	for _, tc := range []struct {
		name string
		path string
	}{
		{"unset path", ""},
		{"missing file", filepath.Join(t.TempDir(), "does-not-exist.txt")},
		{"empty file", writeTempPrompts(t, "\n#only a comment\n   \n")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prompts := LoadPromptsFromFile(tc.path)
			if len(prompts) == 0 {
				t.Fatal("expected fallback default prompts, got none")
			}
			for _, p := range prompts {
				if !p.FamilyFriendly {
					t.Fatalf("default prompt %q is not family-friendly", p.Text)
				}
			}
		})
	}
}

// ratedRegistry builds a registry with a known mix of family-friendly and adult
// prompts for the filtering tests.
func ratedRegistry() (*Registry, []string, []string) {
	familyFriendly := []string{"solar system", "how vaccines work", "a terrible boat name"}
	adult := []string{"the female orgasm", "a threesome request"}

	prompts := make([]Prompt, 0, len(familyFriendly)+len(adult))
	for _, t := range familyFriendly {
		prompts = append(prompts, Prompt{Text: t, FamilyFriendly: true})
	}
	for _, t := range adult {
		prompts = append(prompts, Prompt{Text: t, FamilyFriendly: false})
	}
	return NewRegistry(sampleTileKeys(), prompts), familyFriendly, adult
}

// drawnPrompts starts one round per deck entry and returns the distinct prompts
// the game drew (a full deck yields every prompt exactly once before it wraps).
func drawnPrompts(t *testing.T, g *Manager, deckSize int) []string {
	t.Helper()
	seen := make([]string, 0, deckSize)
	for i := 0; i < deckSize; i++ {
		state, err := g.StartRound()
		if err != nil {
			t.Fatalf("StartRound %d: %v", i, err)
		}
		if !slices.Contains(seen, state.Prompt) {
			seen = append(seen, state.Prompt)
		}
	}
	return seen
}

func TestCreateGameFamilyFriendlyExcludesAdultPrompts(t *testing.T) {
	r, familyFriendly, adult := ratedRegistry()

	g, err := r.CreateGame(true)
	if err != nil {
		t.Fatalf("CreateGame(true): %v", err)
	}

	seen := drawnPrompts(t, g, len(familyFriendly))
	slices.Sort(seen)
	wantSeen := slices.Clone(familyFriendly)
	slices.Sort(wantSeen)
	if !slices.Equal(seen, wantSeen) {
		t.Fatalf("family-friendly game drew %v; want exactly %v", seen, wantSeen)
	}
	for _, bad := range adult {
		if slices.Contains(seen, bad) {
			t.Fatalf("family-friendly game drew adult prompt %q", bad)
		}
	}
}

func TestCreateGameDefaultIncludesAdultPrompts(t *testing.T) {
	r, familyFriendly, adult := ratedRegistry()

	g, err := r.CreateGame(false)
	if err != nil {
		t.Fatalf("CreateGame(false): %v", err)
	}

	seen := drawnPrompts(t, g, len(familyFriendly)+len(adult))
	for _, want := range append(slices.Clone(familyFriendly), adult...) {
		if !slices.Contains(seen, want) {
			t.Fatalf("default game never drew %q; deck should include every prompt", want)
		}
	}
}

func TestCreateGameFamilyFriendlyFallsBackWhenNoneRated(t *testing.T) {
	// A misconfigured bank with only adult prompts: a family-friendly game must
	// still be playable and must never draw one of the adult prompts.
	adult := []string{"the female orgasm", "a threesome request"}
	prompts := make([]Prompt, len(adult))
	for i, text := range adult {
		prompts[i] = Prompt{Text: text, FamilyFriendly: false}
	}
	r := NewRegistry(sampleTileKeys(), prompts)

	g, err := r.CreateGame(true)
	if err != nil {
		t.Fatalf("CreateGame(true): %v", err)
	}

	state, err := g.StartRound()
	if err != nil {
		t.Fatalf("StartRound on fallback deck: %v", err)
	}
	if state.Prompt == "" {
		t.Fatal("family-friendly fallback drew an empty prompt")
	}
	if slices.Contains(adult, state.Prompt) {
		t.Fatalf("family-friendly fallback drew adult prompt %q", state.Prompt)
	}
}
