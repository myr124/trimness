package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"
	"trimness/internal/agent"
	"trimness/internal/llm"

	"github.com/joho/godotenv"
)

func main() {
	if len(os.Args) < 2 || len(os.Args) > 2 {
		fmt.Println("invalid arguments")
		return
	}

	err := godotenv.Load()
	if err != nil {
		log.Println("No .env file found, relying on system environment variables")
	}

	// 1. Create a base context with a timeout duration
	// Always call the cancel function (via defer) to avoid resource leaks
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	a := agent.Agent{Name: "Trimness", Client: llm.GeminiClient{API_key: os.Getenv("GEMINI_API_KEY"), Model: "gemini-3.5-flash"}}

	fmt.Println(a.Run(ctx, os.Args[1]))

}
