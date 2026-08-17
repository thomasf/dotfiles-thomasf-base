package main

import (
	"fmt"
	"os"
)

func printUsage() {
	fmt.Fprintf(os.Stderr, "Usage: go-mod-rename <command> [options]\n\n")
	fmt.Fprintf(os.Stderr, "  rename  Rename this module\n")
	fmt.Fprintf(os.Stderr, "  switch  Switch a dependency to its new module path\n\n")
	fmt.Fprintf(os.Stderr, "See \"go-mod-rename <command> -h\" for options.\n")
}

func main() {
	args := os.Args[1:]
	if len(args) < 1 {
		printUsage()
		os.Exit(1)
	}

	switch args[0] {
	case "rename":
		runRename(args[1:])

	case "switch":
		runSwitch(args[1:])

	case "help", "-h", "--help":
		printUsage()

	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", args[0])
		printUsage()
		os.Exit(1)
	}
}
