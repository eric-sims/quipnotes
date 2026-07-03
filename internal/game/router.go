package game

import (
	"errors"
	"net/http"
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

type WordsResponse struct {
	Words []string `json:"words"`
}

type CreateGameResponse struct {
	Code string `json:"code"`
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

// RoundResponse describes the active round: its number (0 before the first
// prompt is drawn) and prompt text.
type RoundResponse struct {
	Round  int    `json:"round"`
	Prompt string `json:"prompt"`
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
//	@Summary		Starts a new game
//	@Description	Creates a new game and returns its unique 4-digit code. Driven by the manager (host).
//	@Router			/games [post]
//	@Produce		json
//	@Failure		500	{object}	ErrorResponse
//	@Success		201	{object}	CreateGameResponse
func CreateGame(c *gin.Context) {
	g, err := Games.CreateGame()
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(http.StatusCreated, CreateGameResponse{Code: g.Code()})
}

// CloseGame godoc
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

// StartRound godoc
//	@Summary		Draws the next prompt (starts a round)
//	@Description	Pops the next prompt off the game's shuffled deck, begins a new round, and clears the previous round's notes. Driven by the manager (host).
//	@Router			/games/{code}/rounds [post]
//	@Produce		json
//	@Param			code	path		string	true	"game code"
//	@Failure		404		{object}	ErrorResponse
//	@Failure		500		{object}	ErrorResponse
//	@Success		201		{object}	RoundResponse
func StartRound(c *gin.Context) {
	g, ok := resolveGame(c)
	if !ok {
		return
	}
	round, prompt, err := g.StartRound()
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(http.StatusCreated, RoundResponse{Round: round, Prompt: prompt})
}

// GetRound godoc
//	@Summary		Current round
//	@Description	Returns the active round number and prompt (round 0 before the first prompt is drawn). Used for polling and reconnect.
//	@Router			/games/{code}/round [get]
//	@Produce		json
//	@Param			code	path		string	true	"game code"
//	@Failure		404		{object}	ErrorResponse
//	@Success		200		{object}	RoundResponse
func GetRound(c *gin.Context) {
	g, ok := resolveGame(c)
	if !ok {
		return
	}
	round, prompt := g.CurrentRound()
	c.JSON(http.StatusOK, RoundResponse{Round: round, Prompt: prompt})
}

// ServeEvents godoc
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
//	@Summary		Draws Tiles
//	@Description	Draws Tiles (wordTiles) for a given player and a given count
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

	c.JSON(http.StatusOK, WordsResponse{words})
}

// GetTiles godoc
//	@Summary		Gets Drawn Tiles
//	@Description	Gets all the tiles that are drawn by the player.
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

	c.JSON(http.StatusOK, WordsResponse{words})
}

type SubmitNoteRequest struct {
	PlayerId PlayerId `json:"id"`
	// Note is the ordered token list: tile keys ("42|banana") plus optional
	// "\n" (BreakToken) markers for line breaks between clusters.
	Note []string `json:"note"`
}

// SubmitNote godoc
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

	if err := g.Submit(request.Note, request.PlayerId); err != nil {
		// "No active round" / "already submitted this round" are friendly,
		// retryable conditions — 409 so the client can gate the button instead
		// of treating it as a hard failure.
		if errors.Is(err, ErrNoActiveRound) || errors.Is(err, ErrAlreadySubmitted) {
			c.JSON(http.StatusConflict, ErrorResponse{Error: err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	// Push a live submission count to any connected host screen.
	round, count, total := g.RoundSubmissionStatus()
	g.Hub().broadcast(event{Type: "submission", Round: round, Count: count, Total: total})

	c.Status(http.StatusOK)
}

type AddPlayerRequest struct {
	PlayerId string `json:"id"`
}

// AddPlayer godoc
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
//	@Summary		Returns the submitted notes
//	@Description	Returns the submitted notes for the game. Each note is an ordered token list: tile keys ("42|banana") plus "\n" (BreakToken) markers for line breaks.
//	@Router			/games/{code}/submitted-notes [get]
//	@Produce		json
//	@Param			code	path		string	true	"game code"
//	@Failure		404		{object}	ErrorResponse
//	@Success		200		{object}	map[string][][]string
func GetSubmittedNotes(c *gin.Context) {
	g, ok := resolveGame(c)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, gin.H{"notes": g.GetSubmittedNotes()})
}
