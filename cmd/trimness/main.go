package main

import (
	"context"
	"fmt"
	"log"
	"os"
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

	ctx := context.Background()

	a := agent.Agent{Name: "Trimness", Client: llm.GeminiClient{API_key: os.Getenv("GEMINI_API_KEY")}}

	fmt.Println(a.Run(ctx, os.Args[1]))

}
