package game

import (
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
	Note     []string `json:"note"`
}

// SubmitNote godoc
//	@Summary		Turn in Note
//	@Description	Send a string array to turn in your wordTiles for the game.
//	@Router			/games/{code}/submit [post]
//	@Accept			json
//	@Param			code	path	string	true	"game code"
//	@Param			request	body		game.SubmitNoteRequest	true	"contains the note"
//	@Failure		400		{object}	ErrorResponse
//	@Failure		404		{object}	ErrorResponse
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
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

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
//	@Description	Returns a list of strings that are the submitted notes for the game
//	@Router			/games/{code}/submitted-notes [get]
//	@Produce		json
//	@Param			code	path		string	true	"game code"
//	@Failure		404		{object}	ErrorResponse
//	@Success		200		{object}	map[string][]string
func GetSubmittedNotes(c *gin.Context) {
	g, ok := resolveGame(c)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, gin.H{"notes": g.GetSubmittedNotes()})
}

// DeleteSubmittedNotes godoc
//	@Summary		Deletes the submitted notes
//	@Description	Clears the submitted notes for the game
//	@Router			/games/{code}/submitted-notes [delete]
//	@Param			code	path	string	true	"game code"
//	@Failure		404		{object}	ErrorResponse
//	@Success		200
func DeleteSubmittedNotes(c *gin.Context) {
	g, ok := resolveGame(c)
	if !ok {
		return
	}
	g.ClearSubmitted()
	c.Status(http.StatusOK)
}
