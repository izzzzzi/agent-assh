package main

import (
	"os"

	"github.com/agent-ssh/assh/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
