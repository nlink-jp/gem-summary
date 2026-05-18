// Package cmd defines the gem-summary CLI surface via cobra.
//
// gem-summary is a single-purpose CLI that summarises a text
// document via Vertex AI Gemini. Default path is one LLM call;
// inputs that exceed the model's effective context window fall
// back to chunked + parallel + merge summarisation (Phase 2;
// not yet implemented — over-limit inputs currently error).
// See the RFP under docs/ja/gem-summary-rfp.ja.md for the full
// design rationale.
package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/nlink-jp/gem-summary/internal/config"
	"github.com/nlink-jp/gem-summary/internal/summarizer"
	"github.com/nlink-jp/gem-summary/internal/vertexai"
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
AI Gemini LLM call. Inputs that exceed the configured token
threshold will, in v0.2.0+, fall back to chunked + parallel +
merge summarisation; in v0.1.x they surface as an error.

For full design rationale see docs/ja/gem-summary-rfp.ja.md (the
project RFP). For everyday configuration see ` + "`config.example.toml`" + `
and the README.`,
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

// run is the gem-summary entry point. End-to-end flow:
//   1. Read input from FILE (or stdin if FILE is "-" / omitted).
//   2. Load config from TOML + env + flags.
//   3. Build a Vertex AI client.
//   4. Hand off to internal/summarizer.
//   5. Emit summary to stdout (or JSON if --json), progress to
//      stderr (unless --quiet).
//
// Returning errors from RunE makes cobra emit them on stderr
// and exit non-zero; the CLI wrapper in main.go converts that
// into a clean os.Exit(1).
func run(cmd *cobra.Command, args []string) error {
	doc, err := readInput(args)
	if err != nil {
		return err
	}

	cfg, err := config.Load(flagConfig)
	if err != nil {
		return err
	}
	// Flag-level model override sits ABOVE the env-var override
	// applied in config.Load. The precedence chain is therefore
	//   flag > env > config-file > built-in default.
	if flagModel != "" {
		cfg.Model.Name = flagModel
	}

	style, err := summarizer.ParseStyle(orFlag(flagStyle, cfg.Summary.DefaultStyle))
	if err != nil {
		return err
	}

	progress := newProgressFunc(flagQuiet)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client, err := vertexai.NewClient(ctx, cfg.GCP.Project, cfg.GCP.Location, cfg.Model.Name, cfg.Summary.RequestTimeout)
	if err != nil {
		return err
	}

	progress(fmt.Sprintf("model: %s @ %s/%s", client.Model(), cfg.GCP.Project, cfg.GCP.Location))

	maxIn := cfg.Summary.ChunkThreshold
	if flagMaxInputTokens > 0 {
		maxIn = flagMaxInputTokens
	}

	t0 := time.Now()
	res, err := summarizer.Summarize(ctx, client, doc, summarizer.Options{
		Style:          style,
		Lang:           flagLang,
		MaxInputTokens: maxIn,
		Progress:       progress,
	})
	if err != nil {
		return err
	}
	elapsed := time.Since(t0)

	progress(fmt.Sprintf("done: %d chunks, %d→%d tokens, %s",
		res.Chunks, res.InputTokens, res.OutputTokens, elapsed.Round(time.Millisecond)))

	return emitResult(cmd.OutOrStdout(), res, elapsed)
}

// readInput returns the document body for the single positional
// argument, treating "-" or no-arg as stdin. The CLI accepts
// only one input source — the chunked path lands within a
// single document, not by concatenating multiple files.
func readInput(args []string) (string, error) {
	src := "-"
	if len(args) == 1 {
		src = args[0]
	}
	if src == "-" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("read stdin: %w", err)
		}
		return string(data), nil
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", src, err)
	}
	return string(data), nil
}

// orFlag returns the flag value if set, else the fallback.
// Trim avoids treating a single space as "set".
func orFlag(flag, fallback string) string {
	if flag == "" {
		return fallback
	}
	return flag
}

// newProgressFunc returns the progress callback Summarize uses.
// When --quiet is on, the function is a no-op so the inner
// loops can call it unconditionally.
func newProgressFunc(quiet bool) func(string) {
	if quiet {
		return func(string) {}
	}
	return func(s string) {
		fmt.Fprintln(os.Stderr, "gem-summary: "+s)
	}
}

// emitResult writes the summary in the user-selected format.
// Default is plain text on stdout (one trailing newline so the
// output composes with `wc -l`); --json writes a structured
// payload that the shell-agent-v2 summary shell-tool consumes
// to surface token usage upstream.
func emitResult(out io.Writer, res *summarizer.Result, elapsed time.Duration) error {
	if flagJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]any{
			"summary":          res.Summary,
			"chunks":           res.Chunks,
			"tokens_in":        res.InputTokens,
			"tokens_out":       res.OutputTokens,
			"duration_seconds": elapsed.Seconds(),
		})
	}
	_, err := fmt.Fprintln(out, res.Summary)
	return err
}
