// Command itm is the Incident Time Machine.
//
// It is deliberately thin: all behaviour lives in internal/cli so it can be
// tested without spawning a process.
package main

import (
	"context"
	"os"

	"github.com/andrelbuzzo/itm/internal/cli"
)

func main() {
	os.Exit(cli.Main(context.Background(), os.Args[1:]))
}
