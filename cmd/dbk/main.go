package main

import (
	"fmt"
	"os"

	"github.com/tanzid64/tui-db-manager/internal/cli"
)

func main() {
	if err := cli.NewRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "dbk:", err)
		os.Exit(1)
	}
}
