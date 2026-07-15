# Quipnotes — game server

The multiplayer game server for **Quipnotes**, a party game where players arrange word
tiles into a "ransom note" answering a prompt, then a rotating judge picks a favorite.
This Go + Gin service is the **source of truth** for game state: it hosts many concurrent
games at once (Jackbox-style, each a 4-digit code) and pushes live updates to every screen
over WebSockets.

Quipnotes is a mash-up of two games we love — [Ransom Notes] by Very Special Games and
[Quiplash] by Jackbox Games — hence the name. The best in-person experience is still to buy
Ransom Notes and use its cards; this project makes it playable on phones, no table full of
tiny tiles required.

[Ransom Notes]: https://www.veryspecialgames.com/products/ransom-notes-the-ridiculous-word-magnet-game
[Quiplash]: https://www.jackboxgames.com/games/quiplash

## The system

Quipnotes is three independently-versioned projects (each its own git repo):

| Project | Role | Stack |
| --- | --- | --- |
| **`quipnotes`** (this repo) | Game server — owns all state, one HTTP/WS API per game code | Go + Gin |
| **`quipnotesclient`** | Player client — join with a name + code, draw tiles, submit notes, judge | Vue 3 + Vite |
| **`quipnotesmanager`** | Host/manager client — start a game, show the code, drive the board | Vue 3 + Vite |

Everything the server exposes is scoped to a 4-digit game code. State lives **in memory**
only — restarting the server wipes every game.

## Quick start

```bash
cp .env.example .env      # then edit — see Configuration below
go run .                  # serves on :8081 (override with PORT)
```

Requires **Go 1.21+**. The server **panics on startup** if `.env` is missing or
`WORDS_FILE_PATH` doesn't point to a readable words file. Once running, the interactive API docs
are at <http://localhost:8081/swagger/index.html>.

```bash
go build -o quipnotes     # build a binary
go test ./...             # run the test suite
swag init                 # regenerate Swagger docs in docs/ from handler annotations
```

## Configuration

Set via `.env` (see [`.env.example`](.env.example)):

| Variable | Required | Default | Purpose |
| --- | --- | --- | --- |
| `WORDS_FILE_PATH` | **yes** | — | Path to the word-tile file (`words.txt`). Missing/unreadable → panic on boot. |
| `PROMPTS_FILE_PATH` | no | built-in bank | Path to the prompts file. Unset/unreadable → log a warning and use a built-in family-friendly bank. |
| `PORT` | no | `8081` | Port to bind. |
| `GIN_MODE` | no | `debug` | `release` in production to quiet Gin. |

### `words.txt`

Ransom Notes' word tiles are proprietary, so no `words.txt` is committed. Buy a copy and
index the tiles (about 15 minutes), or bring **any** word list you like.

- One word per line, prefixed with one or more **part-of-speech markers** listing every
  part of speech the word can be, e.g. `[noun][verb] play`. Valid markers: `[noun]`
  `[verb]` `[adjective]` `[adverb]` `[pronoun]` `[preposition]` `[conjunction]`
  `[interjection]` `[other]` (punctuation, suffix tiles, articles).
- Blank lines are ignored; a line starting with `#` is a comment. The parser is
  forgiving: an unknown marker is dropped with a warning and an unmarked word defaults
  to `[other]`.
- Duplicate words are fine; each word line becomes a distinct tile keyed
  `"<index>|<word>"`. The tags are served to clients per tile key in the `pos` field of
  the draw/tiles responses, so players can browse their pile grouped by part of speech.

### `prompts.txt` (optional)

Prompts are the round cards a player writes a note about. Supply your own list, or skip it
entirely and the server falls back to a small built-in family-friendly bank so it still
boots with a playable game. See [`data/prompts.example.txt`](data/prompts.example.txt).

- One prompt per line — line-based (not CSV), so prompts may contain commas.
- Blank lines are ignored; a line starting with `#` is a comment.
- **Family-friendly mode:** prefix a line with the `[adult]` marker to rate it *adult*.
  Adult prompts are withheld from games a host starts in family-friendly mode (a lobby
  toggle), and the marker is stripped from the text. Every other line is family-friendly.

```text
# A comment. Everything below is a prompt, one per line.
The worst possible thing to say on a first date
A terrible name for a boat
[adult] Whisper something seductive to your date during a movie
```

## How the game plays

Play runs in **rounds** — one active prompt each:

1. The host starts a game (`POST /games` → a 4-digit code) and shares the code; players
   join from their phones.
2. A round begins when the next prompt is drawn (`POST /games/:code/rounds`). It's pushed
   live to every screen.
3. Each round rotates a **judge** (round-robin, so everyone judges once per cycle before
   repeating). The judge sits out; everyone else submits **one note**.
4. Judging opens automatically once every non-judge has submitted (or the judge forces it),
   then the judge flips notes face-up and picks a **favorite** — the author scores a point.
   A round with fewer than 2 players is judge-less and plays freely.
5. Anyone eligible draws the next prompt, which clears the board and starts the next round.

## Architecture

- **In-memory, multi-game.** A package-level `game.Registry` (`internal/game/game.go`) maps
  each 4-digit code to a `game.Manager` — one game with its own players, tile pool,
  submitted notes, prompt deck, scores, and judging state, each behind its own mutex. No
  database.
- **Tile wire format `"<id>|<word>"`.** The numeric id (the word's index in the words
  file) keeps duplicate words distinct across the whole protocol. Notes are stored as
  ordered token lists — tile keys plus a reserved `"\n"` break token for line breaks —
  never flattened to a string. Part-of-speech tags never ride in the token itself: the
  draw/tiles responses carry them in a separate `pos` map keyed by tile key.
- **WebSockets for push.** `GET /games/:code/events` streams round/judging/roster events and
  replays the current round's lifecycle on connect so refreshes and late joins recover
  mid-round. Player actions still go over the REST endpoints; the socket is push-only.

The routes, handlers (with Swagger annotations), and the full event catalog live in
`internal/game/router.go`; pure game logic in `game.go`; word/prompt loading in `words.go`
and `prompts.go`; the WebSocket hub in `hub.go`. See [`CLAUDE.md`](CLAUDE.md) for a
detailed map of the state model and every endpoint. Interactive API docs are served at
`/swagger/index.html`.

## Deployment

Production runs in Docker behind Caddy. See [`DEPLOY.md`](DEPLOY.md) for the full setup.
Note the known [port mismatch](CLAUDE.md#gotchas): `main.go` binds `:8081` while the base
`Dockerfile`/`docker-compose.yaml` reference 8080 — verify ports before relying on the
container.

## Development notes

- **CORS** allows all origins with credentials — a local-development posture only.
- Each sub-project is its own git repo. Branch off the latest `master` and open a PR rather
  than committing straight to `master`.

## License

The code in this repository is licensed under the [MIT License](LICENSE).

The Ransom Notes word tiles and prompt cards are **proprietary** to Very Special Games and
are **not** covered by this license — none are committed here. Supply your own `words.txt`
and (optionally) prompts file as described under [Configuration](#configuration).
