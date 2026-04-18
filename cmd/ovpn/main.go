package main

import (
	"fmt"
	"os"

	"ovpn/internal/cli"
)

// main wires dependencies, starts workers, and blocks until shutdown.
func main() {
	root := cli.NewRootCmd()
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
