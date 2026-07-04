package main

import (
	"fmt"
	"os"

	"github.com/carapace-sh/carapace-spec-argparse/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
