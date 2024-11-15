package main

import (
	"fmt"
	"log"
	"os"

	"eric-sims/quipnotes/docs"
	"eric-sims/quipnotes/internal/game"

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
	game.Game = game.NewGameManager()

	err := godotenv.Load()
	if err != nil {
		panic(fmt.Sprintf("Error loading .env file: %s", err.Error()))
	}
	filePath := os.Getenv("WORDS_FILE_PATH")
	//htmlDir := os.Getenv("HTML_DIR_PATH")

	fmt.Println("filePath", filePath)
	if err := game.LoadWordsFromCSV(filePath); err != nil {
		panic(fmt.Sprintf("Failed to load words.csv: %s", err.Error()))
	}

	r := gin.Default()

	r.POST("/players", game.AddPlayer)
	r.DELETE("/players/:id", game.DeletePlayer)

	r.GET("/players/:id/tiles", game.GetTiles)

	r.POST("/game/draw", game.DrawTiles)
	r.POST("/game/submit", game.SubmitNote)

	docs.SwaggerInfo.BasePath = ""
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	log.Println("Starting server...")
	err = r.Run(":8081")
	if err != nil {
		panic(err)
	}
}
