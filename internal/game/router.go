package game

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

type DrawTilesRequest struct {
	PlayerId PlayerId `json:"id"`
	Count    int      `json:"count"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

// WordsResponse is the player's whole pile. Pos maps each tile key to the
// word's part-of-speech tags (a word can have several); keys with no known
// tags are omitted and clients treat them as "other".
type WordsResponse struct {
	Words []string            `json:"words"`
	Pos   map[string][]string `json:"pos,omitempty"`
}

type CreateGameResponse struct {
	Code string `json:"code"`
}

// CreateGameRequest is the optional body of POST /games. familyFriendly limits
// the new game's prompt deck to family-friendly prompts (no explicit/suggestive
// content). The body may be omitted entirely, which defaults to false (all
// prompts) — so existing callers that post no body are unaffected.
type CreateGameRequest struct {
	FamilyFriendly bool `json:"familyFriendly"`
}

type GameInfoResponse struct {
	Code    string     `json:"code"`
	Players []PlayerId `json:"players"`
}

// PlayersResponse is the game roster. Each player is an object (not a bare id
// string) so a per-player score can be added later without a breaking change.
type PlayersResponse struct {
	Players []Player `json:"players"`
}

// NotesResponse is the note board: this round's notes in their shared shuffled
// display order, each with a stable 1-based id and its face-up state.
type NotesResponse struct {
	Notes []NoteView `json:"notes"`
}

// isConflict reports whether err is one of the friendly, retryable rule
// violations (round/submission/judging state) that map to 409 Conflict rather
// than a hard 500.
func isConflict(err error) bool {
	for _, conflict := range []error{
		ErrNoActiveRound, ErrAlreadySubmitted, ErrJudgeCannotSubmit,
		ErrJudgingStarted, ErrNoJudge, ErrJudgingAlreadyOpen,
		ErrJudgingNotOpen, ErrNoNotesYet, ErrFavoritePicked, ErrNoteNotFlipped,
		ErrNotTheJudge, ErrRoundAdvanced,
	} {
		if errors.Is(err, conflict) {
			return true
		}
	}
	return false
}

// resolveGame looks up the game named by the :code path param. On failure it
// writes a 404 and returns ok=false, so callers can simply `return`.
func resolveGame(c *gin.Context) (*Manager, bool) {
	code := c.Param("code")
	g, err := Games.GetGame(code)
	if err != nil {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: err.Error()})
		return nil, false
	}
	return g, true
}

// CreateGame godoc
//
//	@Summary		Starts a new game
//	@Description	Creates a new game and returns its unique 4-digit code. Driven by the manager (host). An optional body {familyFriendly:true} limits the game's prompt deck to family-friendly prompts; the body may be omitted (defaults to all prompts).
//	@Router			/games [post]
//	@Accept			json
//	@Produce		json
//	@Param			request	body		game.CreateGameRequest	false	"family-friendly mode (omit for all prompts)"
//	@Failure		400	{object}	ErrorResponse
//	@Failure		500	{object}	ErrorResponse
//	@Success		201	{object}	CreateGameResponse
func CreateGame(c *gin.Context) {
	// The body is optional: a host that just wants all prompts may post none.
	request := CreateGameRequest{}
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
			return
		}
	}

	g, err := Games.CreateGame(request.FamilyFriendly)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(http.StatusCreated, CreateGameResponse{Code: g.Code()})
}

// CloseGame godoc
//
//	@Summary		Ends a game
//	@Description	Removes a game from the server. Driven by the manager (host).
//	@Router			/games/{code} [delete]
//	@Param			code	path	string	true	"game code"
//	@Failure		404		{object}	ErrorResponse
//	@Success		200
func CloseGame(c *gin.Context) {
	code := c.Param("code")
	if err := Games.CloseGame(code); err != nil {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: err.Error()})
		return
	}
	c.Status(http.StatusOK)
}

// GetGameInfo godoc
//
//	@Summary		Game info
//	@Description	Returns a game's code and current players. Used to validate a join.
//	@Router			/games/{code} [get]
//	@Produce		json
//	@Param			code	path		string	true	"game code"
//	@Failure		404		{object}	ErrorResponse
//	@Success		200		{object}	GameInfoResponse
func GetGameInfo(c *gin.Context) {
	g, ok := resolveGame(c)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, GameInfoResponse{Code: g.Code(), Players: g.GetPlayers()})
}

// GetPlayers godoc
//
//	@Summary		Game roster
//	@Description	Returns a game's current players (roster) as objects. Used by the host to show who has joined; forward-compatible with per-player scoring.
//	@Router			/games/{code}/players [get]
//	@Produce		json
//	@Param			code	path		string	true	"game code"
//	@Failure		404		{object}	ErrorResponse
//	@Success		200		{object}	PlayersResponse
func GetPlayers(c *gin.Context) {
	g, ok := resolveGame(c)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, PlayersResponse{Players: g.Roster()})
}

// StartRoundRequest is the optional body of POST /rounds. The manager (host)
// sends no body and starts rounds unconditionally. A player sends their id
// plus the round they are advancing *from*: the server then requires them to
// be the current judge (when the round has one) and rejects a stale round so
// two taps can't skip a prompt.
type StartRoundRequest struct {
	PlayerId PlayerId `json:"id"`
	Round    int      `json:"round"`
}

// StartRound godoc
//
//	@Summary		Draws the next prompt (starts a round)
//	@Description	Pops the next prompt off the game's shuffled deck, begins a new round (selecting its judge), and clears the previous round's notes. With no body it is the manager's (host) unconditional draw. A player may advance instead by sending {id, round}: id must be the current judge (any joined player at round 0 / in a judge-less round) and round must be the current round number.
//	@Router			/games/{code}/rounds [post]
//	@Accept			json
//	@Produce		json
//	@Param			code	path		string					true	"game code"
//	@Param			request	body		game.StartRoundRequest	false	"player advancing the round (omit for the host)"
//	@Failure		400		{object}	ErrorResponse
//	@Failure		404		{object}	ErrorResponse
//	@Failure		409		{object}	ErrorResponse
//	@Failure		500		{object}	ErrorResponse
//	@Success		201		{object}	RoundState
func StartRound(c *gin.Context) {
	g, ok := resolveGame(c)
	if !ok {
		return
	}

	// The body is optional: the manager posts without one (the unconditional
	// host draw), so only bind when the request actually carries content.
	request := StartRoundRequest{}
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
			return
		}
	}

	var state RoundState
	var err error
	if request.PlayerId != "" {
		state, err = g.AdvanceRound(request.PlayerId, request.Round)
	} else {
		state, err = g.StartRound()
	}
	if err != nil {
		status := http.StatusInternalServerError
		if isConflict(err) {
			status = http.StatusConflict
		}
		c.JSON(status, ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(http.StatusCreated, state)
}

// GetRound godoc
//
//	@Summary		Current round
//	@Description	Returns the active round's full state: number (0 before the first prompt is drawn), prompt, judge, judging/submission progress, and the picked favorite. Used for polling and reconnect.
//	@Router			/games/{code}/round [get]
//	@Produce		json
//	@Param			code	path		string	true	"game code"
//	@Failure		404		{object}	ErrorResponse
//	@Success		200		{object}	RoundState
func GetRound(c *gin.Context) {
	g, ok := resolveGame(c)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, g.CurrentRoundState())
}

// OpenJudging godoc
//
//	@Summary		Opens judging early (judge's override)
//	@Description	Closes submissions and lets the judge start flipping notes before every player has answered (judging opens automatically when the last eligible player submits). Requires an active round with a judge and at least one note.
//	@Router			/games/{code}/judging [post]
//	@Param			code	path		string	true	"game code"
//	@Failure		404		{object}	ErrorResponse
//	@Failure		409		{object}	ErrorResponse
//	@Success		200
func OpenJudging(c *gin.Context) {
	g, ok := resolveGame(c)
	if !ok {
		return
	}
	if err := g.OpenJudging(); err != nil {
		status := http.StatusInternalServerError
		if isConflict(err) {
			status = http.StatusConflict
		}
		c.JSON(status, ErrorResponse{Error: err.Error()})
		return
	}
	c.Status(http.StatusOK)
}

// FlipNote godoc
//
//	@Summary		Flips a note face-up
//	@Description	Turns the note with the given 1-based id face-up and broadcasts note_flipped so every screen flips in sync. One-way and idempotent. Locked until judging opens, except in judge-less rounds.
//	@Router			/games/{code}/notes/{noteId}/flip [post]
//	@Param			code	path	string	true	"game code"
//	@Param			noteId	path	int		true	"1-based note id"
//	@Failure		400		{object}	ErrorResponse
//	@Failure		404		{object}	ErrorResponse
//	@Failure		409		{object}	ErrorResponse
//	@Success		200
func FlipNote(c *gin.Context) {
	g, ok := resolveGame(c)
	if !ok {
		return
	}
	noteId, err := strconv.Atoi(c.Param("noteId"))
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "noteId must be a number"})
		return
	}
	if err := g.FlipNote(noteId); err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, ErrUnknownNote):
			status = http.StatusBadRequest
		case isConflict(err):
			status = http.StatusConflict
		}
		c.JSON(status, ErrorResponse{Error: err.Error()})
		return
	}
	c.Status(http.StatusOK)
}

type PickFavoriteRequest struct {
	NoteId int `json:"noteId"`
}

type PickFavoriteResponse struct {
	WinnerId PlayerId `json:"winnerId"`
}

// PickFavorite godoc
//
//	@Summary		Picks the judge's favorite note
//	@Description	Records the round's winning note (by 1-based id): its author scores a point and favorite_picked is broadcast. One favorite per round; the note must be face-up.
//	@Router			/games/{code}/favorite [post]
//	@Accept			json
//	@Produce		json
//	@Param			code	path		string						true	"game code"
//	@Param			request	body		game.PickFavoriteRequest	true	"the winning note id"
//	@Failure		400		{object}	ErrorResponse
//	@Failure		404		{object}	ErrorResponse
//	@Failure		409		{object}	ErrorResponse
//	@Success		200		{object}	PickFavoriteResponse
func PickFavorite(c *gin.Context) {
	g, ok := resolveGame(c)
	if !ok {
		return
	}
	request := PickFavoriteRequest{}
	if err := c.Bind(&request); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	winner, err := g.PickFavorite(request.NoteId)
	if err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, ErrUnknownNote):
			status = http.StatusBadRequest
		case isConflict(err):
			status = http.StatusConflict
		}
		c.JSON(status, ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, PickFavoriteResponse{WinnerId: winner})
}

// ServeEvents godoc
//
//	@Summary		Game event stream (WebSocket)
//	@Description	Upgrades to a WebSocket that pushes round_started / submission / game_ended events for the game. Both players and the host subscribe.
//	@Router			/games/{code}/events [get]
//	@Param			code	path	string	true	"game code"
//	@Failure		404		{object}	ErrorResponse
func ServeEvents(c *gin.Context) {
	g, ok := resolveGame(c)
	if !ok {
		return
	}
	serveEvents(g, c.Writer, c.Request)
}

// DrawTiles godoc
//
//	@Summary		Draws Tiles
//	@Description	Draws Tiles (wordTiles) for a given player and a given count. Returns the player's entire pile plus a pos map of part-of-speech tags per tile key.
//	@Router			/games/{code}/draw [post]
//	@Accept			json
//	@Produce		json
//	@Param			code	path		string					true	"game code"
//	@Param			request	body		game.DrawTilesRequest	true	"tells how many tiles to draw"
//	@Failure		400		{object}	ErrorResponse
//	@Failure		404		{object}	ErrorResponse
//	@Failure		500		{object}	ErrorResponse
//	@Success		200		{object}	WordsResponse
func DrawTiles(c *gin.Context) {
	g, ok := resolveGame(c)
	if !ok {
		return
	}

	request := DrawTilesRequest{}
	if err := c.Bind(&request); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	words, err := g.DrawWordTiles(request.Count, request.PlayerId)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	c.JSON(http.StatusOK, WordsResponse{Words: words, Pos: Games.TilePos(words)})
}

// GetTiles godoc
//
//	@Summary		Gets Drawn Tiles
//	@Description	Gets all the tiles that are drawn by the player, plus a pos map of part-of-speech tags per tile key.
//	@Router			/games/{code}/players/{id}/tiles [get]
//	@Produce		json
//	@Param			code	path		string	true	"game code"
//	@Param			id		path		string	true	"player id"
//	@Failure		400		{object}	ErrorResponse
//	@Failure		404		{object}	ErrorResponse
//	@Failure		500		{object}	ErrorResponse
//	@Success		200		{object}	WordsResponse
func GetTiles(c *gin.Context) {
	g, ok := resolveGame(c)
	if !ok {
		return
	}

	id := c.Param("id")
	if len(id) == 0 || strings.TrimSpace(id) == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "id required"})
		return
	}

	words, err := g.GetDrawnWordTiles(PlayerId(id))
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	c.JSON(http.StatusOK, WordsResponse{Words: words, Pos: Games.TilePos(words)})
}

type SubmitNoteRequest struct {
	PlayerId PlayerId `json:"id"`
	// Note is the ordered token list: tile keys ("42|banana") plus optional
	// "\n" (BreakToken) markers for line breaks between clusters.
	Note []string `json:"note"`
}

// SubmitNote godoc
//
//	@Summary		Turn in Note
//	@Description	Send the ordered note tokens (tile keys "42|banana" plus optional "\n" line breaks) to turn in your wordTiles for the game.
//	@Router			/games/{code}/submit [post]
//	@Accept			json
//	@Param			code	path	string	true	"game code"
//	@Param			request	body		game.SubmitNoteRequest	true	"contains the note"
//	@Failure		400		{object}	ErrorResponse
//	@Failure		404		{object}	ErrorResponse
//	@Failure		409		{object}	ErrorResponse
//	@Failure		500		{object}	ErrorResponse
//	@Success		200
func SubmitNote(c *gin.Context) {
	g, ok := resolveGame(c)
	if !ok {
		return
	}

	request := SubmitNoteRequest{}
	if err := c.Bind(&request); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	// Submit broadcasts the live submission count itself (and judging_ready
	// when the last eligible player answers).
	if err := g.Submit(request.Note, request.PlayerId); err != nil {
		// Round/judging rule violations are friendly, retryable conditions —
		// 409 so the client can gate the button instead of treating it as a
		// hard failure.
		if isConflict(err) {
			c.JSON(http.StatusConflict, ErrorResponse{Error: err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	c.Status(http.StatusOK)
}

type AddPlayerRequest struct {
	PlayerId string `json:"id"`
}

// AddPlayer godoc
//
//	@Summary		Joins a game
//	@Description	Adds a player to a game. The playerId must be unique within the game.
//	@Router			/games/{code}/players [post]
//	@Accept			json
//	@Param			code	path	string	true	"game code"
//	@Param			request	body		game.AddPlayerRequest	true	"contains the player id"
//	@Failure		400		{object}	ErrorResponse
//	@Failure		404		{object}	ErrorResponse
//	@Failure		500		{object}	ErrorResponse
//	@Success		200
func AddPlayer(c *gin.Context) {
	g, ok := resolveGame(c)
	if !ok {
		return
	}

	var p AddPlayerRequest
	if c.Bind(&p) == nil {
		if p.PlayerId == "" {
			c.JSON(http.StatusBadRequest, ErrorResponse{Error: "playerId required"})
			return
		}

		if err := g.AddPlayer(PlayerId(p.PlayerId)); err != nil {
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
			return
		}
	}

	c.Status(http.StatusOK)
}

// DeletePlayer godoc
//
//	@Summary		Leaves a game
//	@Description	Removes a player from a game. The playerId must exist.
//	@Router			/games/{code}/players/{id} [delete]
//	@Param			code	path	string	true	"game code"
//	@Param			id		path		string	true	"player id"
//	@Failure		400		{object}	ErrorResponse
//	@Failure		404		{object}	ErrorResponse
//	@Failure		500		{object}	ErrorResponse
//	@Success		200
func DeletePlayer(c *gin.Context) {
	g, ok := resolveGame(c)
	if !ok {
		return
	}

	id := c.Param("id")
	if len(id) == 0 || strings.TrimSpace(id) == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "id required"})
		return
	}
	if err := g.RemovePlayer(PlayerId(id)); err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	c.Status(http.StatusOK)
}

// GetSubmittedNotes godoc
//
//	@Summary		Returns the submitted notes
//	@Description	Returns this round's note board in its shared shuffled display order. Each note carries a stable 1-based id, its ordered token list (tile keys "42|banana" plus "\n" line breaks), and whether it is face-up. Authors are withheld until the favorite is picked.
//	@Router			/games/{code}/submitted-notes [get]
//	@Produce		json
//	@Param			code	path		string	true	"game code"
//	@Failure		404		{object}	ErrorResponse
//	@Success		200		{object}	NotesResponse
func GetSubmittedNotes(c *gin.Context) {
	g, ok := resolveGame(c)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, NotesResponse{Notes: g.GetSubmittedNotes()})
}
