package main

import (
	"fmt"
	"os"
	"trimness/internal/agent"
)

func main() {
	if len(os.Args) < 2 || len(os.Args) > 2 {
		fmt.Println("invalid arguments")
		return
	}

	a := agent.Agent{Name: "Trimness"}

	a.Run(os.Args[1])

	// add llm client and call to gemini api

	fmt.Println(a.History)
}
