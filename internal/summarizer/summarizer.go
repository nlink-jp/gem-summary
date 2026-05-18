// Package summarizer orchestrates the summarisation pipeline.
//
// Phase 1 (this file): single-call path only. Inputs whose
// estimated token count exceeds the configured threshold are
// rejected with ErrInputTooLarge; the chunked + parallel +
// merge fallback path lands in Phase 2.
//
// Inputs are always wrapped in a nlk/guard nonce tag before
// reaching the LLM, so prompt-injection payloads inside the
// source document cannot escape into the system-prompt
// instruction band.
package summarizer

import (
	"context"
	"errors"
	"fmt"

	"github.com/nlink-jp/gem-summary/internal/vertexai"
	"github.com/nlink-jp/nlk/guard"
)

// ErrInputTooLarge is returned by Summarize when the input's
// estimated token count exceeds Options.MaxInputTokens. Phase 2
// will replace this rejection with the chunked path; the
// sentinel exists now so callers (the CLI) can render a
// user-friendly message rather than a raw error string.
var ErrInputTooLarge = errors.New("input exceeds the configured token threshold")

// Result is what Summarize hands back to the CLI. Token counts
// come from Vertex AI's UsageMetadata when available; Chunks is
// 1 in Phase 1 (single-call path) and >1 once chunking lands.
type Result struct {
	Summary      string
	Chunks       int
	InputTokens  int
	OutputTokens int
}

// Options controls a single Summarize call. The CLI populates
// these from the merged config / flag values and passes them in
// so the summariser stays decoupled from cobra / config types.
type Options struct {
	Style           Style
	Lang            string // empty = auto-detect
	MaxInputTokens  int    // 0 means "no limit" — used when chunking lands in Phase 2
	Progress        func(string)
}

// Client is the LLM dependency the summariser needs. The full
// *vertexai.Client satisfies this; tests can pass a stub.
type Client interface {
	Generate(ctx context.Context, systemPrompt, userPrompt string) (*vertexai.Response, error)
}

// Summarize runs the single-call pipeline:
//   1. Estimate input token count.
//   2. If it exceeds opts.MaxInputTokens (and the cap is > 0),
//      return ErrInputTooLarge for the CLI to surface.
//   3. Build a fresh guard tag, wrap the document, build the
//      system + user prompts.
//   4. Call client.Generate.
//   5. Return the cleaned summary text alongside chunk/token
//      stats for the --json output path.
//
// The function does NOT enforce a minimum-output sanity check
// (e.g. "summary must be non-empty") — that responsibility
// belongs to the CLI layer so it can produce a clear
// human-readable exit status when the model returns nothing.
func Summarize(ctx context.Context, client Client, doc string, opts Options) (*Result, error) {
	inTok := EstimateTokens(doc)
	if opts.MaxInputTokens > 0 && inTok > opts.MaxInputTokens {
		return nil, fmt.Errorf("%w: estimated %d tokens > limit %d (chunking lands in Phase 2)",
			ErrInputTooLarge, inTok, opts.MaxInputTokens)
	}

	tag := guard.NewTag()
	sys := BuildSystemPrompt(opts.Style, opts.Lang, tag)
	user, err := BuildUserPrompt(doc, tag)
	if err != nil {
		return nil, err
	}

	if opts.Progress != nil {
		opts.Progress(fmt.Sprintf("input: ~%d tokens (single-call path)", inTok))
	}

	resp, err := client.Generate(ctx, sys, user)
	if err != nil {
		return nil, fmt.Errorf("summarise: %w", err)
	}

	return &Result{
		Summary:      resp.Text,
		Chunks:       1,
		InputTokens:  resp.InputTokens,
		OutputTokens: resp.OutputTokens,
	}, nil
}
