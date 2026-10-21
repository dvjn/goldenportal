package main

import (
	"context"
	"log"

	"github.com/dvjn/goldport/internal/app"
)

func main() {
	app, err := app.New(context.Background())
	if err != nil {
		log.Fatalf("create goldport app: %v", err)
	}

	if err := app.Run(); err != nil {
		log.Fatalf("run goldport app: %v", err)
	}
}
