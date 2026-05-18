// Package cmd defines the gem-summary CLI surface via cobra.
//
// gem-summary is a single-purpose CLI that summarises a text
// document via Vertex AI Gemini. Default path is one LLM call;
// inputs that exceed the model's effective context window fall
// back to chunked + parallel + merge summarisation. See the RFP
// under docs/ja/gem-summary-rfp.ja.md for the design rationale.
//
// This file holds the cobra root command and flag parsing. The
// heavy lifting (config load, Vertex AI client, summarisation
// pipeline, progress output) lives in internal/.
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Flag values bound by cobra. Kept package-scoped so RunE can
// reach them without threading them through every helper.
var (
	flagConfig         string
	flagStyle          string
	flagLang           string
	flagModel          string
	flagMaxInputTokens int
	flagChunkSize      int
	flagJSON           bool
	flagQuiet          bool
)

var rootCmd = &cobra.Command{
	Use:   "gem-summary [FILE]",
	Short: "Single-call text summariser via Vertex AI Gemini",
	Long: `gem-summary reads a text document from FILE (or stdin if FILE is
"-" or omitted) and writes a summary to stdout via a single Vertex
AI Gemini LLM call. Inputs that exceed the model's effective
context window automatically fall back to chunked + parallel +
merge summarisation.

For full design rationale and the chunking algorithm, see
docs/ja/gem-summary-rfp.ja.md (the project RFP). For everyday
configuration see ` + "`config.example.toml`" + ` and the README.`,
	Args: cobra.MaximumNArgs(1),
	RunE: run,
}

// Execute runs the root command. Called from main.go with the
// build-time version string injected via -ldflags.
func Execute(version string) {
	rootCmd.Version = version

	rootCmd.Flags().StringVarP(&flagConfig, "config", "c", "",
		"Config file path (default: ~/.config/gem-summary/config.toml)")
	rootCmd.Flags().StringVar(&flagStyle, "style", "",
		"Output length preset: short, medium, long (default: from config)")
	rootCmd.Flags().StringVar(&flagLang, "lang", "",
		"Output language (e.g. ja, en). Default: auto-detect from input.")
	rootCmd.Flags().StringVar(&flagModel, "model", "",
		"Override the Gemini model name from config.")
	rootCmd.Flags().IntVar(&flagMaxInputTokens, "max-input-tokens", 0,
		"Hard cap on input token count (default: from config).")
	rootCmd.Flags().IntVar(&flagChunkSize, "chunk-size", 0,
		"Window size in tokens when chunking is triggered (default: from config).")
	rootCmd.Flags().BoolVar(&flagJSON, "json", false,
		"Emit structured JSON instead of plain-text summary.")
	rootCmd.Flags().BoolVarP(&flagQuiet, "quiet", "q", false,
		"Suppress stderr progress lines.")

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// run is the scaffolding stub. Phase-1 commits in the
// implementation series wire in config / Vertex AI client /
// summariser; right now it just confirms the binary builds and
// flags parse.
func run(cmd *cobra.Command, args []string) error {
	fmt.Fprintln(os.Stderr, "gem-summary: scaffold build — summarisation not yet implemented")
	return nil
}
