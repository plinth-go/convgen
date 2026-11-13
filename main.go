package main

import (
	"fmt"
	"os"

	"github.com/plinth-go/convgen/internal/runner"
)

const usage = `convgen generates type-safe conversion functions from //convgen: comments.

Usage:
  convgen [./... | directory ...]

Examples:
  convgen ./...
  convgen ./internal/api
`

func main() {
	if len(os.Args) == 2 && (os.Args[1] == "-h" || os.Args[1] == "--help") {
		fmt.Print(usage)
		return
	}
	if err := runner.Run(os.Args[1:], os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
