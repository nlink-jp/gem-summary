package main

import "github.com/nlink-jp/gem-summary/cmd"

// version is set at build time via -ldflags "-X main.version=...".
// Cobra reads it via cmd.Execute and surfaces it through
// `gem-summary --version`.
var version = "dev"

func main() {
	cmd.Execute(version)
}
