package main

import (
	"fmt"
	"os"

	"github.com/ametel01/agentreceipt/cmd"
	"github.com/ametel01/agentreceipt/internal/buildinfo"
)

func main() {
	if err := cmd.Execute(buildinfo.Version()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
