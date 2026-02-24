package main

import (
	"os"

	"github.com/scottcrooks/mono/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args))
}
