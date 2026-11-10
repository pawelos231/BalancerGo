package main

import (
	"log"
	"os"

	"load-balancer/internal/app"
)

func main() {
	if err := app.Run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}
