package main

import (
	"log"
	"os"

	"github.com/takutakahashi/agentapi-proxy/internal/di"
)

func main() {
	container := di.NewContainer()
	server := container.GetHTTPServer()

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Starting server on port %s", port)
	if err := server.Start(":" + port); err != nil {
		log.Printf("Fatal error starting server: %v", err)
		os.Exit(1)
	}
}
