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

//	@title			quipNotes Server
//	@version		1.0
//	@description	This handles the game logic and communication
//	@host			localhost:8081
func main() {
	err := godotenv.Load()
	if err != nil {
		panic(fmt.Sprintf("Error loading .env file: %s", err.Error()))
	}
	filePath := os.Getenv("WORDS_FILE_PATH")

	fmt.Println("filePath", filePath)
	tileKeys, err := game.LoadWordsFromCSV(filePath)
	if err != nil {
		panic(fmt.Sprintf("Failed to load words.csv: %s", err.Error()))
	}
	game.Games = game.NewRegistry(tileKeys)

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
	r.POST("/games/:code/players", game.AddPlayer)
	r.DELETE("/games/:code/players/:id", game.DeletePlayer)
	r.GET("/games/:code/players/:id/tiles", game.GetTiles)
	r.POST("/games/:code/draw", game.DrawTiles)
	r.POST("/games/:code/submit", game.SubmitNote)

	// Manager (host) note board for a game.
	r.GET("/games/:code/submitted-notes", game.GetSubmittedNotes)
	r.DELETE("/games/:code/submitted-notes", game.DeleteSubmittedNotes)

	docs.SwaggerInfo.BasePath = ""
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	log.Println("Starting server...")
	err = r.Run(":8081")
	if err != nil {
		panic(err)
	}
}
