package main

import (
	"os"

	"tokenhub/backend/internal/migration/cli"

	// Register all source adapters.
	_ "tokenhub/backend/internal/migration/source/litellm"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
