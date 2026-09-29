package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/c0dn/ocws/internal/cli"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if err := cli.New(version).Execute(); err != nil {
		var exit cli.ExitError
		if errors.As(err, &exit) {
			os.Exit(exit.Code)
		}
		fmt.Fprintln(os.Stderr, "ocws:", err)
		os.Exit(1)
	}
}
