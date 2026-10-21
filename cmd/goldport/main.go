package main

import (
	"os"

	"github.com/dvjn/goldport/internal/app"
	"github.com/dvjn/goldport/internal/cli"
)

func main() {
	if len(os.Args) > 1 {
		os.Exit(cli.Run(os.Args[1:], cli.IO{In: os.Stdin, Out: os.Stdout, Err: os.Stderr, Getenv: os.Getenv}))
	}

	app.New().Run()
}
