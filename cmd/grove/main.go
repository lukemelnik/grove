// Package main is the entry point for the grove CLI.
package main

import (
	"fmt"
	"os"

	"github.com/lukemelnik/grove/internal/cmd"
)

func main() {
	rootCmd := cmd.NewRootCmd()
	if err := rootCmd.Execute(); err != nil {
		if !cmd.ErrorAlreadyReported(err) {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(1)
	}
}
