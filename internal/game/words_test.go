package game

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// writeWordsFile drops content into a temp file and returns its path.
func writeWordsFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "words.txt")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing temp words file: %v", err)
	}
	return path
}

func TestLoadWordsFromFileParsesMarkers(t *testing.T) {
	path := writeWordsFile(t, `# header comment

[noun] banana
[noun][verb] play
[other] !
`)
	keys, pos, err := LoadWordsFromFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantKeys := []string{"0|banana", "1|play", "2|!"}
	if !reflect.DeepEqual(keys, wantKeys) {
		t.Fatalf("keys = %v, want %v", keys, wantKeys)
	}
	wantPos := map[string][]string{
		"0|banana": {"noun"},
		"1|play":   {"noun", "verb"},
		"2|!":      {"other"},
	}
	if !reflect.DeepEqual(pos, wantPos) {
		t.Fatalf("pos = %v, want %v", pos, wantPos)
	}
}

func TestLoadWordsFromFileForgivingParsing(t *testing.T) {
	path := writeWordsFile(t, `[nuon][verb] mistagged
untagged
[noun]
[NOUN] shouty
[noun] [verb] spaced
`)
	keys, pos, err := LoadWordsFromFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The marker-only "[noun]" line is skipped, so indices stay contiguous.
	wantKeys := []string{"0|mistagged", "1|untagged", "2|shouty", "3|spaced"}
	if !reflect.DeepEqual(keys, wantKeys) {
		t.Fatalf("keys = %v, want %v", keys, wantKeys)
	}
	wantPos := map[string][]string{
		"0|mistagged": {"verb"},         // unknown marker dropped
		"1|untagged":  {"other"},        // no markers -> other
		"2|shouty":    {"noun"},         // markers are case-insensitive
		"3|spaced":    {"noun", "verb"}, // whitespace between markers is fine
	}
	if !reflect.DeepEqual(pos, wantPos) {
		t.Fatalf("pos = %v, want %v", pos, wantPos)
	}
}

func TestLoadWordsFromFileDuplicateWordsGetDistinctKeys(t *testing.T) {
	path := writeWordsFile(t, "[noun] banana\n[noun] banana\n")
	keys, _, err := LoadWordsFromFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"0|banana", "1|banana"}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("keys = %v, want %v", keys, want)
	}
}

func TestLoadWordsFromFileErrors(t *testing.T) {
	if _, _, err := LoadWordsFromFile(filepath.Join(t.TempDir(), "missing.txt")); err == nil {
		t.Fatal("expected an error for a missing file")
	}
	empty := writeWordsFile(t, "# only a comment\n\n")
	if _, _, err := LoadWordsFromFile(empty); err == nil {
		t.Fatal("expected an error for a file with no usable lines")
	}
}

func TestRegistryTilePos(t *testing.T) {
	r := NewRegistry(
		[]string{"0|alpha", "1|beta"},
		map[string][]string{"0|alpha": {"noun"}, "1|beta": {"verb", "noun"}},
		samplePrompts(),
	)
	got := r.TilePos([]string{"1|beta", "9|unknown"})
	want := map[string][]string{"1|beta": {"verb", "noun"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("TilePos = %v, want %v", got, want)
	}
}

// TestDrawnTilesCarryPos covers the join the draw/tiles handlers perform:
// every key in a player's drawn pile resolves through Registry.TilePos.
func TestDrawnTilesCarryPos(t *testing.T) {
	keys := sampleTileKeys()
	tilePos := make(map[string][]string, len(keys))
	for _, key := range keys {
		tilePos[key] = []string{"noun"}
	}
	r := NewRegistry(keys, tilePos, samplePrompts())
	g, err := r.CreateGame(false)
	if err != nil {
		t.Fatalf("CreateGame: %v", err)
	}
	if err := g.AddPlayer("p1"); err != nil {
		t.Fatalf("AddPlayer: %v", err)
	}
	drawn, err := g.DrawWordTiles(3, "p1")
	if err != nil {
		t.Fatalf("DrawWordTiles: %v", err)
	}
	pos := r.TilePos(drawn)
	if len(pos) != len(drawn) {
		t.Fatalf("TilePos covered %d of %d drawn keys: %v", len(pos), len(drawn), pos)
	}
	for _, key := range drawn {
		if !reflect.DeepEqual(pos[key], []string{"noun"}) {
			t.Fatalf("pos[%q] = %v, want [noun]", key, pos[key])
		}
	}
}

func TestRegistryTilePosNilMap(t *testing.T) {
	r := NewRegistry([]string{"0|alpha"}, nil, samplePrompts())
	if got := r.TilePos([]string{"0|alpha"}); got != nil {
		t.Fatalf("TilePos with nil map = %v, want nil", got)
	}
}
