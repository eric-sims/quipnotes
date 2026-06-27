# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
go run .                 # run the server (listens on :8081)
go build -o quipnotes    # build binary
go test ./...            # run all tests
swag init                # regenerate Swagger docs in docs/ from handler annotations
```

Requires a `.env` file (copy `.env.example`). The server **panics on startup** if `.env` is missing or `WORDS_FILE_PATH` does not point to a readable CSV. Swagger UI is served at `/swagger/index.html`.

The `words.csv` format is a single-column, header-less CSV. Each row becomes a tile keyed as `"<rowIndex>|<word>"`.

## Architecture

The server hosts **many concurrent games**. The package-level `game.Games *Registry`
(`internal/game/game.go`), initialized in `main.go`, holds:
- `games map[string]*Manager` — live games keyed by 4-digit code
- `tileKeys []string` — the base tile list, loaded once from CSV
- `mux sync.RWMutex` — guards the games map

`LoadWordsFromCSV` returns the `tileKeys`; `NewRegistry(tileKeys)` stores them. Registry
methods: `CreateGame()` (allocates a unique 4-digit code, retrying on collision, and copies
`tileKeys` into a fresh per-game pool — **no players are added at creation**),
`GetGame(code)`, and `CloseGame(code)` (no auth).

Each `game.Manager` is **one game** and holds:
- `code string` — its 4-digit code
- `players []PlayerId` — players who joined this game
- `wordTiles map[string]PlayerId` — tile key (`"42|banana"`) → owning player ID, or `""` if available
- `submittedNotes []string` — accumulated ransom notes
- `mux sync.RWMutex` — guards this game's fields

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
- `POST /games/:code/submit` → `{id, note: ["42|banana", ...]}` — validates all tiles belong to the player, then atomically releases them and appends the human-readable note to `submittedNotes`
- `GET /games/:code/submitted-notes` / `DELETE /games/:code/submitted-notes` — manager read/clear for the note board

A handler that can't find `:code` returns **404** (`resolveGame` in `router.go`). `router.go`
holds all Gin handlers (with Swagger annotations) and the request/response structs. Pure
game logic is in `game.go`. `words.go` handles CSV loading only.

## Gotchas

- **Port mismatch in Docker:** `main.go` binds `:8081`, but `Dockerfile` exposes 8080 and `docker-compose.yaml` maps `8080:8080`. Verify before using the container.
- CORS allows all origins (`*`) with credentials — intended only for local development.
- The `DrawWordTiles` lock is released manually (not deferred) before calling `GetDrawnWordTiles`, which acquires its own lock. This is intentional to avoid a deadlock since both methods lock `mux`.
