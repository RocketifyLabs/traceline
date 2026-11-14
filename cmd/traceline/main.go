// Command traceline is the Traceline.
//
// It is deliberately thin: all behaviour lives in internal/cli so it can be
// tested without spawning a process.
package main

import (
	"context"
	"os"

	"github.com/RocketifyLabs/traceline/internal/cli"
)

func main() {
	os.Exit(cli.Main(context.Background(), os.Args[1:]))
}
