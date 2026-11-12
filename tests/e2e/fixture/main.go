package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: fixture workspace|disable-alice|android DIRECTORY")
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var err error
	switch os.Args[1] {
	case "workspace":
		err = workspace(ctx, os.Args[2])
	case "ha-workspace":
		err = haWorkspace(ctx, os.Args[2])
	case "disable-alice":
		err = disableAlice(ctx, os.Args[2])
	case "android":
		err = androidFixture(ctx, os.Args[2])
	default:
		err = fmt.Errorf("unknown fixture")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
