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
- `submittedNotes []submittedNote` — the current round's ransom notes (cleared each round). Each holds its ordered token list (tile keys plus `"\n"` `BreakToken` line breaks), its **author** (server-side only, for scoring), a **flipped** flag, and a random **sortKey** so every screen shows the board in the same shuffled order without leaking submission order
- `promptDeck []string` + `promptCursor int` — this game's shuffled prompt deck and next index
- `round int` + `currentPrompt string` — active round (0 = none) and its prompt
- `submittedThisRound map[PlayerId]bool` — who has answered this round (reset each round)
- `scores map[PlayerId]int` + `hasJudged map[PlayerId]bool` — running scores (survive rounds and rejoins) and the judge-rotation cycle
- `judge PlayerId`, `judgingOpen bool`, `favoriteNote int`, `winner PlayerId` — the round's judging state (favoriteNote is a 1-based note id, 0 = none)
- `hub *hub` — this game's WebSocket subscribers (see `hub.go`)
- `mux sync.RWMutex` — guards this game's fields

**Rounds:** `StartRound()` pops the next prompt (reshuffling when the deck is exhausted so a
game never runs out), increments `round`, clears `submittedThisRound`, `submittedNotes` and
the judging state, **selects the round's judge**, and broadcasts `round_started`. `Submit`
returns `ErrNoActiveRound` / `ErrAlreadySubmitted` / `ErrJudgeCannotSubmit` /
`ErrJudgingStarted` (all mapped to **409** via `isConflict` in `router.go`) to enforce one
note per round, that the judge doesn't answer, and that submissions close once judging
opens. `words.go` loads words, `prompts.go` loads prompts, `hub.go` holds the WebSocket
hub + connection pumps; judging tests live in `judging_test.go`.

**Judging & scoring:** each round one player is the **judge** — `pickJudgeLocked` takes the
first player (in join order) who hasn't judged this rotation cycle, resetting the cycle once
everyone has, so every player judges once before anyone judges again. A round that starts
with **fewer than 2 players gets no judge** (`judge == ""`) and plays like the pre-judging
game (everyone may submit; the host can flip notes freely). Judging **opens automatically**
when every non-judge player has submitted (`maybeOpenJudgingLocked`, also re-checked when a
straggler leaves), or the judge **forces it** via `OpenJudging` (needs ≥1 note); open judging
closes submissions. The judge (or host) then **flips notes face-up** (`FlipNote` — one-way,
idempotent, broadcasts `note_flipped`) and **picks a favorite** (`PickFavorite` — the note
must be face-up, one pick per round; the author scores a point and `favorite_picked` plus a
refreshed scored `players` roster are broadcast). If the judge leaves mid-round the next
player in rotation takes over and `round_started` is re-broadcast with the new `judgeId`.

Player IDs only need to be unique **within a game**.

**Tile key format:** `"<csvRowIndex>|<word>"` — the index prefix makes duplicate words distinct. Every tile drawn, held, and submitted uses this format throughout the wire protocol. A submitted note may also contain the reserved **`BreakToken` (`"\n"`)** between clusters to mark a line break; it has no `|`, so it never collides with a tile. Notes are stored and returned as their **token list**, not flattened to a string — the host parses each token and renders each cluster on its own line.

**Request/response flow** (every route is game-scoped):
- `POST /games` — manager starts a game; returns `{code}`
- `DELETE /games/:code` — manager ends a game (open, no auth)
- `GET /games/:code` — returns `{code, players}` (players as bare id strings); used to validate a join
- `GET /games/:code/players` — returns the roster `{players: [{id, score}]}`; used by the host to show who has joined and the live scoreboard
- `POST /games/:code/players` → `{id}` — player joins (id unique within the game); broadcasts the updated `players` roster
- `DELETE /games/:code/players/:id` — player leaves; broadcasts the updated `players` roster (and may reassign the judge / open judging, see above)
- `POST /games/:code/draw` → `{id, count}` — draws `count` available tiles, returns the player's **entire current pile** (not just new tiles)
- `GET /games/:code/players/:id/tiles` — the player's current pile (used after submit to refresh)
- `POST /games/:code/submit` → `{id, note: ["42|banana", "\n", ...]}` — the ordered token list (tiles plus optional `"\n"` line breaks); validates all tiles belong to the player and the round rules, then atomically releases them and appends the normalized token list to `submittedNotes`; **409** if no active round, already answered, submitter is the judge, or judging has opened
- `POST /games/:code/rounds` — draws the next prompt; returns the full `RoundState` (201). With **no body** it is the manager's unconditional host draw. A **player** may advance instead with `{id, round}` (`AdvanceRound`): `id` must be a joined player and — while the round has a judge — the current judge (round 0 and judge-less rounds accept any joined player, so a game can run without the host after creation); `round` is the round being advanced *from* — a mismatch is **409** `ErrRoundAdvanced` so racing taps can't skip a prompt (**409** `ErrNotTheJudge` for a non-judge)
- `GET /games/:code/round` — current `RoundState`: `{round, prompt, judgeId, judgingOpen, count, total, favoriteNoteId, winnerId}` (round 0 / zero values before any draw; `total` excludes the judge)
- `POST /games/:code/judging` — judge force-opens judging early (auto-opens when the last eligible player submits); **409** if no round/judge/notes or already open
- `POST /games/:code/notes/:noteId/flip` — flips a note face-up by its 1-based id (one-way, idempotent); **409** until judging opens (except judge-less rounds), **400** unknown id
- `POST /games/:code/favorite` → `{noteId}` — judge picks the winning note; returns `{winnerId}` and scores the author a point; **409** if judging not open, already picked, or the note is face-down
- `GET /games/:code/events` — **WebSocket** upgrade; pushes `round_started {round,prompt,judgeId}`, `submission {round,count,total}`, `judging_ready {round}`, `note_flipped {round,noteId}`, `favorite_picked {round,noteId,winnerId}`, `players {players:[{id,score}]}` (roster on join/leave and after a pick), and `game_ended {}`. On connect the round's lifecycle is **replayed as a snapshot** (`round_started`, then `judging_ready` / `favorite_picked` if the round got that far, plus the roster) — flip state is not replayed; clients re-fetch the note board when judging is open
- `GET /games/:code/submitted-notes` → `{notes: [{id, tokens, flipped}, ...]}` — the note board in its shared shuffled display order (per-round random sort keys; note `id`s are 1-based and stable; authors withheld until the reveal; notes are cleared server-side by `StartRound`, so there is no manual clear route)

A handler that can't find `:code` returns **404** (`resolveGame` in `router.go`). `router.go`
holds all Gin handlers (with Swagger annotations) and the request/response structs. Pure
game logic is in `game.go`. `words.go` handles CSV loading only.

## Gotchas

- **Port mismatch in Docker:** `main.go` binds `:8081`, but `Dockerfile` exposes 8080 and `docker-compose.yaml` maps `8080:8080`. Verify before using the container.
- CORS allows all origins (`*`) with credentials — intended only for local development.
- The `DrawWordTiles` lock is released manually (not deferred) before calling `GetDrawnWordTiles`, which acquires its own lock. This is intentional to avoid a deadlock since both methods lock `mux`.
