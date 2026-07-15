package main

import (
	"fmt"
	"log"
	"os"

	"eric-sims/quipnotes/docs"
	"eric-sims/quipnotes/internal/game"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

// @title			quipNotes Server
// @version		1.0
// @description	This handles the game logic and communication
// @host			localhost:8081
func main() {
	// A .env file is convenient for local dev but optional in production, where
	// configuration is injected as real environment variables (e.g. by Docker
	// Compose on the VM). A missing file is tolerated; anything else is logged.
	if err := godotenv.Load(); err != nil {
		log.Printf("No .env file loaded (%s); using process environment", err.Error())
	}
	filePath := os.Getenv("WORDS_FILE_PATH")

	fmt.Println("filePath", filePath)
	tileKeys, tilePos, err := game.LoadWordsFromFile(filePath)
	if err != nil {
		panic(fmt.Sprintf("Failed to load words file: %s", err.Error()))
	}

	// Prompts are optional: LoadPromptsFromFile falls back to a built-in bank
	// (and logs a warning) if PROMPTS_FILE_PATH is unset or unreadable, so the
	// server still boots with a playable game.
	prompts := game.LoadPromptsFromFile(os.Getenv("PROMPTS_FILE_PATH"))

	game.Games = game.NewRegistry(tileKeys, tilePos, prompts)

	r := gin.Default()
	// CORS setup - allow only specific origins
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*"},                     // Your frontend's URL
		AllowMethods:     []string{"GET", "POST", "DELETE"}, // Allowed HTTP methods
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
	}))

	// Manager (host) endpoints: start / end a game.
	r.POST("/games", game.CreateGame)
	r.DELETE("/games/:code", game.CloseGame)

	// Game-scoped player endpoints.
	r.GET("/games/:code", game.GetGameInfo)
	r.GET("/games/:code/players", game.GetPlayers)
	r.POST("/games/:code/players", game.AddPlayer)
	r.DELETE("/games/:code/players/:id", game.DeletePlayer)
	r.GET("/games/:code/players/:id/tiles", game.GetTiles)
	r.POST("/games/:code/draw", game.DrawTiles)
	r.POST("/games/:code/submit", game.SubmitNote)

	// Rounds / prompts.
	r.POST("/games/:code/rounds", game.StartRound) // manager draws the next prompt
	r.GET("/games/:code/round", game.GetRound)     // current round (poll / reconnect)
	r.GET("/games/:code/events", game.ServeEvents) // WebSocket push channel

	// Judging: the judge opens judging early, flips notes face-up, and picks
	// the round's favorite (scoring its author a point).
	r.POST("/games/:code/judging", game.OpenJudging)
	r.POST("/games/:code/notes/:noteId/flip", game.FlipNote)
	r.POST("/games/:code/favorite", game.PickFavorite)

	// Manager (host) note board for a game.
	r.GET("/games/:code/submitted-notes", game.GetSubmittedNotes)

	docs.SwaggerInfo.BasePath = ""
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	// Bind to $PORT when provided (containers/PaaS inject it), else the local
	// default of 8081.
	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
	}
	log.Printf("Starting server on :%s ...", port)
	if err := r.Run(":" + port); err != nil {
		panic(err)
	}
}
