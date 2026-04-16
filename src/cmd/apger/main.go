package main

import (
	"fmt"
	"os"

	"github.com/NurOS-Linux/apger/src/core"
)

func main() {
	if err := core.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "apger: %v\n", err)
		os.Exit(1)
	}
}
