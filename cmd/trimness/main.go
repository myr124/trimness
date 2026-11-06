package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 || len(os.Args) > 2 {
		fmt.Println("invalid arguments")
		return
	}

	// add llm client and call to gemini api

	fmt.Println(os.Args[1])
}
