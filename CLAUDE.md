# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
go run .                 # run the server (listens on :8081)
go build -o quipnotes    # build binary
go test ./...            # run all tests
swag init                # regenerate Swagger docs in docs/ from handler annotations
```

Requires a `.env` file (copy `.env.example`). The server **panics on startup** if `.env` is missing or `WORDS_FILE_PATH` does not point to a readable CSV. `PROMPTS_FILE_PATH` is **optional** (one prompt per line; see `data/prompts.example.txt`) — if unset/unreadable the server logs a warning and falls back to a built-in prompt bank (`defaultPrompts` in `internal/game/prompts.go`) rather than panicking. Swagger UI is served at `/swagger/index.html`.

The `words.csv` format is a single-column, header-less CSV. Each row becomes a tile keyed as `"<rowIndex>|<word>"`. Prompts are loaded line-based (not CSV) so they may contain commas.

## Architecture

The server hosts **many concurrent games**. The package-level `game.Games *Registry`
(`internal/game/game.go`), initialized in `main.go`, holds:
- `games map[string]*Manager` — live games keyed by 4-digit code
- `tileKeys []string` — the base tile list, loaded once from CSV
- `mux sync.RWMutex` — guards the games map

`LoadWordsFromCSV` returns the `tileKeys` and `LoadPromptsFromFile` the `prompts`;
`NewRegistry(tileKeys, prompts)` stores both. Registry methods: `CreateGame()` (allocates a
unique 4-digit code, retrying on collision, copies `tileKeys` into a fresh per-game pool and
a **shuffled copy of `prompts` into the game's deck** — **no players are added at creation**),
`GetGame(code)`, and `CloseGame(code)` (no auth; broadcasts `game_ended` and closes the
game's sockets).

Each `game.Manager` is **one game** and holds:
- `code string` — its 4-digit code
- `players []PlayerId` — players who joined this game
- `wordTiles map[string]PlayerId` — tile key (`"42|banana"`) → owning player ID, or `""` if available
- `submittedNotes []string` — the current round's ransom notes (cleared each round)
- `promptDeck []string` + `promptCursor int` — this game's shuffled prompt deck and next index
- `round int` + `currentPrompt string` — active round (0 = none) and its prompt
- `submittedThisRound map[PlayerId]bool` — who has answered this round (reset each round)
- `hub *hub` — this game's WebSocket subscribers (see `hub.go`)
- `mux sync.RWMutex` — guards this game's fields

**Rounds:** `StartRound()` pops the next prompt (reshuffling when the deck is exhausted so a
game never runs out), increments `round`, clears `submittedThisRound` and `submittedNotes`,
and broadcasts `round_started`. `Submit` returns `ErrNoActiveRound` / `ErrAlreadySubmitted`
(mapped to **409** in `router.go`) to enforce one note per round. `words.go` loads words,
`prompts.go` loads prompts, `hub.go` holds the WebSocket hub + connection pumps.

Player IDs only need to be unique **within a game**.

**Tile key format:** `"<csvRowIndex>|<word>"` — the index prefix makes duplicate words distinct. Every tile drawn, held, and submitted uses this format throughout the wire protocol.

**Request/response flow** (every route is game-scoped):
- `POST /games` — manager starts a game; returns `{code}`
- `DELETE /games/:code` — manager ends a game (open, no auth)
- `GET /games/:code` — returns `{code, players}`; used to validate a join
- `POST /games/:code/players` → `{id}` — player joins (id unique within the game)
- `DELETE /games/:code/players/:id` — player leaves
- `POST /games/:code/draw` → `{id, count}` — draws `count` available tiles, returns the player's **entire current pile** (not just new tiles)
- `GET /games/:code/players/:id/tiles` — the player's current pile (used after submit to refresh)
- `POST /games/:code/submit` → `{id, note: ["42|banana", ...]}` — validates all tiles belong to the player and the round rules, then atomically releases them and appends the human-readable note to `submittedNotes`; **409** if no active round or already answered this round
- `POST /games/:code/rounds` — manager draws the next prompt; returns `{round, prompt}` (201)
- `GET /games/:code/round` — current `{round, prompt}` (round 0 before any draw)
- `GET /games/:code/events` — **WebSocket** upgrade; pushes `round_started {round,prompt}` (also a snapshot on connect), `submission {round,count,total}`, and `game_ended {}`
- `GET /games/:code/submitted-notes` — manager reads the note board (notes are cleared server-side by `StartRound`, so there is no manual clear route)

A handler that can't find `:code` returns **404** (`resolveGame` in `router.go`). `router.go`
holds all Gin handlers (with Swagger annotations) and the request/response structs. Pure
game logic is in `game.go`. `words.go` handles CSV loading only.

## Gotchas

- **Port mismatch in Docker:** `main.go` binds `:8081`, but `Dockerfile` exposes 8080 and `docker-compose.yaml` maps `8080:8080`. Verify before using the container.
- CORS allows all origins (`*`) with credentials — intended only for local development.
- The `DrawWordTiles` lock is released manually (not deferred) before calling `GetDrawnWordTiles`, which acquires its own lock. This is intentional to avoid a deadlock since both methods lock `mux`.
