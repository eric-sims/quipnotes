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

// DrawTiles godoc
//	@Summary		Draws Tiles
//	@Description	Draws Tiles (wordTiles) for a given player and a given count
//	@Router			/game/draw [post]
//	@Accept			json
//	@Produce		json
//	@Param			request	body		game.DrawTilesRequest	true	"tells how many tiles to draw"
//	@Failure		400		{object}	ErrorResponse
//	@Failure		500		{object}	ErrorResponse
//	@Success		200		{object}	WordsResponse
func DrawTiles(c *gin.Context) {
	request := DrawTilesRequest{}
	if err := c.Bind(&request); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	words, err := Game.DrawWordTiles(request.Count, request.PlayerId)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	c.JSON(http.StatusOK, WordsResponse{words})
}

// GetTiles godoc
//	@Summary		Gets Drawn Tiles
//	@Description	Gets all the tiles that are drawn by the player.
//	@Router			/players/:id/tiles [get]
//	@Produce		json
//	@Param			id	path		string	true	"player id"
//	@Failure		400	{object}	ErrorResponse
//	@Failure		500	{object}	ErrorResponse
//	@Success		200	{object}	WordsResponse
func GetTiles(c *gin.Context) {
	id := c.Param("id")
	if len(id) == 0 || strings.TrimSpace(id) == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "id required"})
		return
	}

	words, err := Game.GetDrawnWordTiles(PlayerId(id))
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
//	@Router			/game/submit [post]
//	@Accept			json
//	@Param			request	body		game.SubmitNoteRequest	true	"contains the note"
//	@Failure		400		{object}	ErrorResponse
//	@Failure		500		{object}	ErrorResponse
//	@Success		200
func SubmitNote(c *gin.Context) {
	request := SubmitNoteRequest{}
	if err := c.Bind(&request); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	if err := Game.Submit(request.Note, request.PlayerId); err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	c.Status(http.StatusOK)
}

type AddPlayerRequest struct {
	PlayerId string `json:"id"`
}

// AddPlayer godoc
//	@Summary		Adds a player to the game
//	@Description	Adds a player to the game. The playerId must be unique.
//	@Router			/players [post]
//	@Accept			json
//	@Param			request	body		game.AddPlayerRequest	true	"contains the player id"
//	@Failure		400		{object}	ErrorResponse
//	@Failure		500		{object}	ErrorResponse
//	@Success		200
func AddPlayer(c *gin.Context) {
	var p AddPlayerRequest
	if c.Bind(&p) == nil {
		if p.PlayerId == "" {
			c.JSON(http.StatusBadRequest, ErrorResponse{Error: "playerId required"})
			return
		}

		if err := Game.AddPlayer(PlayerId(p.PlayerId)); err != nil {
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
			return
		}
	}

	c.Status(http.StatusOK)
}

// DeletePlayer godoc
//	@Summary		Deletes a player
//	@Description	Deletes a player from the game. The playerId must exist.
//	@Router			/players/:id [delete]
//	@Param			id	path		string	true	"player id"
//	@Failure		400	{object}	ErrorResponse
//	@Failure		500	{object}	ErrorResponse
//	@Success		200
func DeletePlayer(c *gin.Context) {
	id := c.Param("id")
	if len(id) == 0 || strings.TrimSpace(id) == "" || strings.TrimSpace(id) == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "id required"})
		return
	}
	if err := Game.RemovePlayer(PlayerId(id)); err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	c.Status(http.StatusOK)
}

// GetSubmittedNotes godoc
//	@Summary		Returns the submitted notes
//	@Description	Returns a list of strings that are the submitted notes
//	@Router			/game/submitted-notes [get]
//	@Success		200	{object}	[]string
func GetSubmittedNotes(c *gin.Context) {
	c.JSON(200, gin.H{"notes": Game.GetSubmittedNotes()})
}

// DeleteSubmittedNotes godoc
//	@Summary		Deletes the submitted notes
//	@Description	Deletes the submitted notes
//	@Router			/game/submitted-notes [delete]
//	@Success		200
func DeleteSubmittedNotes(c *gin.Context) {
	c.JSON(200, gin.H{"notes": Game.GetSubmittedNotes()})
}
