package main

import (
	"fmt"
	"os"

	"github.com/jharvey10/test-repo/internal/runner"
)

func main() {
	fmt.Println("Starting Alloy wow \\{^_^}/")

	r := runner.New()
	if err := r.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
