package main

import (
	"fmt"
	"io"
	"os"

	"stagehand/internal/version"
)

func main() {
	if err := run(os.Stdout); err != nil {
		os.Exit(1)
	}
}

func run(w io.Writer) error {
	_, err := fmt.Fprintln(w, version.CLIMessage("stagehandd scaffold", "implementation deferred until runtime stories"))
	return err
}
