package main

import (
	"os"

	"github.com/denisakp/sentinel/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
