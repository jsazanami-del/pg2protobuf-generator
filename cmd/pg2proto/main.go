package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/jsazanami-del/pg2protobuf-generator/internal/cli"
)

func main() {
	os.Exit(run())
}

func run() int {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := cli.Execute(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		var ee *cli.ExitError
		if errors.As(err, &ee) {
			return ee.Code
		}
		return cli.ExitErrorCode
	}
	return cli.ExitOK
}
