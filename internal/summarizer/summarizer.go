// Package summarizer orchestrates the summarisation pipeline.
//
// Single-call path: inputs whose estimated token count fits
// within MaxInputTokens are summarised in one LLM call.
//
// Chunked path: inputs that exceed MaxInputTokens are split by
// SplitIntoChunks, each chunk is summarised in parallel
// (Options.ChunkParallelism workers; defaults to 1 if the
// caller leaves it zero), then the per-chunk summaries are
// merged via a final LLM call. If the merged summary itself
// over-runs the cap, the merge is run again at a smaller
// effective batch — bounded recursion guards against runaway
// fan-out on pathological inputs.
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
	"sync"

	"github.com/nlink-jp/gem-summary/internal/vertexai"
	"github.com/nlink-jp/nlk/guard"
)

// ErrInputTooLarge is returned when the input cannot be
// summarised even after the chunked path has fanned out and
// merged — i.e. the merged-summary itself still exceeds
// MaxInputTokens after the recursion limit. Sentinel exposed
// so callers (the CLI) can errors.Is-check it for a clean
// user-facing message.
var ErrInputTooLarge = errors.New("input exceeds the configured token threshold even after chunked merge")

// Result is what Summarize hands back to the CLI. Token
// counts and Chunks reflect the actual pipeline that ran:
// Chunks == 1 for the single-call path; Chunks == N for the
// chunked path, where N includes the merge call.
type Result struct {
	Summary      string
	Chunks       int
	InputTokens  int
	OutputTokens int
}

// Options controls a single Summarize call. The CLI populates
// these from the merged config / flag values and passes them
// in so the summariser stays decoupled from cobra / config
// types.
//
// MaxInputTokens doubles as the chunk-threshold: inputs above
// this go through the chunked path. Zero means "no limit" —
// the single-call path is always used, which only makes sense
// for callers that know their inputs are small.
//
// ChunkSize / ChunkOverlap / ChunkParallelism are honoured
// only when the chunked path activates. ChunkSize must be
// strictly smaller than MaxInputTokens (otherwise a single
// chunk would still over-run the threshold and we'd recurse
// indefinitely); a zero or invalid ChunkSize falls back to
// half MaxInputTokens.
type Options struct {
	Style            Style
	Lang             string // empty = auto-detect
	MaxInputTokens   int    // 0 means "no limit"
	ChunkSize        int
	ChunkOverlap     int
	ChunkParallelism int
	Progress         func(string)
}

// Client is the LLM dependency the summariser needs. The full
// *vertexai.Client satisfies this; tests use a fake.
type Client interface {
	Generate(ctx context.Context, systemPrompt, userPrompt string) (*vertexai.Response, error)
}

// maxMergeRecursion caps the number of merge-of-merges
// passes. Most inputs collapse in one merge; pathological
// inputs (a 50MB log whose per-chunk summaries still sum to
// 1M tokens) might need a second pass. Beyond two we surface
// ErrInputTooLarge.
const maxMergeRecursion = 2

// Summarize routes between the single-call and chunked paths
// and returns a unified Result.
func Summarize(ctx context.Context, client Client, doc string, opts Options) (*Result, error) {
	inTok := EstimateTokens(doc)
	if opts.MaxInputTokens > 0 && inTok > opts.MaxInputTokens {
		return summarizeChunked(ctx, client, doc, opts, inTok)
	}
	return summarizeSingleCall(ctx, client, doc, opts, inTok)
}

// summarizeSingleCall is the original Phase-1 path. Kept as a
// dedicated helper so Phase-2's chunked path can call it for
// individual chunks without going through the threshold check.
func summarizeSingleCall(ctx context.Context, client Client, doc string, opts Options, inTok int) (*Result, error) {
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

// summarizeChunked runs the chunked + parallel + merge path.
// Chunk LLM calls fan out to opts.ChunkParallelism workers
// (defaults to 1 if not set — but the CLI always sets the
// configured value of 3). Errors from any chunk abort the
// whole call; partial results are not exposed.
func summarizeChunked(ctx context.Context, client Client, doc string, opts Options, inTok int) (*Result, error) {
	chunkSize := opts.ChunkSize
	if chunkSize <= 0 {
		// Defensive: a non-positive chunk size would either loop
		// (handled inside SplitIntoChunks) or trivially flatten
		// to single-chunk. Fall back to half MaxInputTokens —
		// arbitrary but safe.
		chunkSize = opts.MaxInputTokens / 2
		if chunkSize <= 0 {
			chunkSize = 1
		}
	}
	// Note: we do NOT defend against ChunkSize >= MaxInputTokens.
	// If the chunker reports a single chunk after the split, the
	// single-chunk fallback below skips the merge step entirely;
	// if it reports multiple chunks but each fits within
	// MaxInputTokens (i.e. the user set ChunkSize well), the
	// chunked path proceeds normally. The pathological "single
	// chunk still over MaxInputTokens" case is handled by the
	// merge-recursion limit in mergeWithRecursion.
	parallelism := opts.ChunkParallelism
	if parallelism <= 0 {
		parallelism = 1
	}

	chunks := SplitIntoChunks(doc, chunkSize, opts.ChunkOverlap)
	if len(chunks) == 0 {
		return nil, fmt.Errorf("chunker produced zero chunks (doc length %d)", len(doc))
	}
	if len(chunks) == 1 {
		// Doc fit in one chunk after all — fall through to
		// the single-call path so we don't pay for a merge
		// LLM round on a single-chunk doc.
		return summarizeSingleCall(ctx, client, doc, opts, inTok)
	}

	if opts.Progress != nil {
		opts.Progress(fmt.Sprintf("input: ~%d tokens → %d chunks @ %d-token windows, parallelism %d",
			inTok, len(chunks), chunkSize, parallelism))
	}

	chunkSummaries, chunkIn, chunkOut, err := runChunksParallel(ctx, client, chunks, opts, parallelism)
	if err != nil {
		return nil, err
	}

	if opts.Progress != nil {
		opts.Progress(fmt.Sprintf("chunks summarised (%d→%d tokens); merging", chunkIn, chunkOut))
	}

	mergedSummary, mergeIn, mergeOut, mergeCalls, err := mergeWithRecursion(ctx, client, chunkSummaries, opts, 0)
	if err != nil {
		return nil, err
	}

	return &Result{
		Summary:      mergedSummary,
		Chunks:       len(chunks) + mergeCalls,
		InputTokens:  chunkIn + mergeIn,
		OutputTokens: chunkOut + mergeOut,
	}, nil
}

// runChunksParallel fans out per-chunk Generate calls to
// parallelism workers. Returns the chunk summaries in source
// order (chunkSummaries[i] is the summary of chunks[i]) along
// with aggregated token counts. Cancels remaining workers as
// soon as one fails; the first error wins.
func runChunksParallel(ctx context.Context, client Client, chunks []Chunk, opts Options, parallelism int) ([]string, int, int, error) {
	type chunkResult struct {
		index   int
		summary string
		inTok   int
		outTok  int
	}

	results := make([]chunkResult, len(chunks))
	errCh := make(chan error, len(chunks))
	sem := make(chan struct{}, parallelism)
	wg := sync.WaitGroup{}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	for i, ch := range chunks {
		wg.Add(1)
		go func(idx int, chunk Chunk) {
			defer wg.Done()

			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}

			tag := guard.NewTag()
			sys := BuildSystemPrompt(opts.Style, opts.Lang, tag)
			user, err := BuildUserPrompt(chunk.Text, tag)
			if err != nil {
				errCh <- fmt.Errorf("chunk %d build prompt: %w", idx, err)
				cancel()
				return
			}

			resp, err := client.Generate(ctx, sys, user)
			if err != nil {
				errCh <- fmt.Errorf("chunk %d generate: %w", idx, err)
				cancel()
				return
			}
			results[idx] = chunkResult{
				index:   idx,
				summary: resp.Text,
				inTok:   resp.InputTokens,
				outTok:  resp.OutputTokens,
			}
			if opts.Progress != nil {
				opts.Progress(fmt.Sprintf("chunk %d/%d done (%d→%d tokens)",
					idx+1, len(chunks), resp.InputTokens, resp.OutputTokens))
			}
		}(i, ch)
	}

	wg.Wait()
	close(errCh)
	if err, ok := <-errCh; ok {
		return nil, 0, 0, err
	}

	summaries := make([]string, len(chunks))
	totalIn := 0
	totalOut := 0
	for _, r := range results {
		summaries[r.index] = r.summary
		totalIn += r.inTok
		totalOut += r.outTok
	}
	return summaries, totalIn, totalOut, nil
}

// mergeWithRecursion calls the merge LLM round. If the merged
// summary itself exceeds MaxInputTokens (rare; would require
// summing ~MaxInputTokens of chunk summaries), the function
// chunks the chunk-summary list and recurses up to
// maxMergeRecursion levels. Returns the final summary along
// with aggregated token / call counts.
func mergeWithRecursion(ctx context.Context, client Client, chunkSummaries []string, opts Options, depth int) (string, int, int, int, error) {
	tag := guard.NewTag()
	sys := BuildMergeSystemPrompt(opts.Style, opts.Lang, tag)
	user, err := BuildMergeUserPrompt(chunkSummaries, tag)
	if err != nil {
		return "", 0, 0, 0, fmt.Errorf("merge prompt: %w", err)
	}
	mergedInTok := EstimateTokens(user)
	if opts.MaxInputTokens > 0 && mergedInTok > opts.MaxInputTokens {
		if depth >= maxMergeRecursion {
			return "", 0, 0, 0, fmt.Errorf("%w: merge input still %d tokens after %d recursions",
				ErrInputTooLarge, mergedInTok, depth)
		}
		// Recursively merge in batches. Reduce the batch size
		// roughly halving the number of summaries per merge
		// call to make forward progress.
		batchSize := (len(chunkSummaries) + 1) / 2
		if batchSize < 2 {
			return "", 0, 0, 0, fmt.Errorf("%w: cannot reduce merge batch below 2 (got %d summaries)",
				ErrInputTooLarge, len(chunkSummaries))
		}
		var nextRound []string
		totalIn := 0
		totalOut := 0
		calls := 0
		for i := 0; i < len(chunkSummaries); i += batchSize {
			end := i + batchSize
			if end > len(chunkSummaries) {
				end = len(chunkSummaries)
			}
			s, in, out, c, err := mergeWithRecursion(ctx, client, chunkSummaries[i:end], opts, depth+1)
			if err != nil {
				return "", 0, 0, 0, err
			}
			nextRound = append(nextRound, s)
			totalIn += in
			totalOut += out
			calls += c
		}
		// One more merge across the per-batch summaries.
		final, in, out, c, err := mergeWithRecursion(ctx, client, nextRound, opts, depth+1)
		return final, totalIn + in, totalOut + out, calls + c, err
	}

	resp, err := client.Generate(ctx, sys, user)
	if err != nil {
		return "", 0, 0, 0, fmt.Errorf("merge generate: %w", err)
	}
	return resp.Text, resp.InputTokens, resp.OutputTokens, 1, nil
}
