package summarizer

import (
	"strings"
)

// Chunk represents one window of the input document. The
// chunked path produces one *vertexai.Generate call per
// Chunk, so size and ordering matter: the index is preserved
// through parallel summarisation so the merge prompt can
// present the chunk summaries in source order.
type Chunk struct {
	Index           int
	Text            string
	EstimatedTokens int
}

// SplitIntoChunks slices doc into Chunks of approximately
// sizeTokens tokens with overlapTokens of overlap between
// adjacent chunks. The token argument is converted to a
// character budget via the same heuristic as EstimateTokens
// (over-estimation-biased) so the resulting chunks are
// reliably ≤ sizeTokens, never larger.
//
// The split point is snapped to the nearest natural
// breakpoint within ±10% of the target — paragraph break
// (double newline) is preferred, then single newline, then
// sentence terminator (. ! ? 。 ! ?). Mid-word splits happen
// only as a last resort when none of those land in the
// search window.
//
// overlap is overlay of the PREVIOUS chunk's tail at the
// start of the next chunk so the summariser sees enough
// surrounding context to summarise the chunk meaningfully.
// The overlap region is included verbatim — no
// deduplication on the summariser side because each chunk
// summary is treated as an independent slice for the merge
// prompt.
//
// Returns nil for empty input. A doc that fits in a single
// chunk returns a single-element slice; callers should treat
// that case identically to "no chunking needed".
func SplitIntoChunks(doc string, sizeTokens, overlapTokens int) []Chunk {
	if doc == "" {
		return nil
	}
	if sizeTokens <= 0 {
		// Defensive: a zero / negative chunk size would loop
		// forever. Treat as "no chunking" — caller can decide
		// whether to error out higher up.
		return []Chunk{{Index: 0, Text: doc, EstimatedTokens: EstimateTokens(doc)}}
	}

	// Convert the token target into a char budget. The token
	// estimator's char-based path uses chars/4 as the lower
	// bound, but real inputs tend to land closer to chars/3
	// once CJK / word-based contributions are accounted for.
	// Target chars/3 here to bias chunks slightly smaller
	// than sizeTokens — under-shooting is cheap (one extra
	// chunk), overshooting can blow the model's hard limit.
	charBudget := sizeTokens * 3
	overlapChars := overlapTokens * 3
	// Defensive normalisation: an overlap >= half the chunk
	// size would make next = end - overlap rewind so far that
	// the loop progresses by only the fallback +1 byte per
	// iteration, producing O(N) chunks. Cap at 50%. The user-
	// facing config has bounded defaults (200000 / 2000) so
	// this only fires when someone hand-edits config to
	// pathological values.
	if overlapChars > charBudget/2 {
		overlapChars = charBudget / 2
	}

	// Single-chunk fast path: doc fits.
	if len(doc) <= charBudget {
		return []Chunk{{Index: 0, Text: doc, EstimatedTokens: EstimateTokens(doc)}}
	}

	var chunks []Chunk
	pos := 0
	for pos < len(doc) {
		end := pos + charBudget
		if end >= len(doc) {
			end = len(doc)
		} else {
			end = snapBreakpoint(doc, pos, end, charBudget)
		}
		text := doc[pos:end]
		chunks = append(chunks, Chunk{
			Index:           len(chunks),
			Text:            text,
			EstimatedTokens: EstimateTokens(text),
		})
		if end == len(doc) {
			break
		}
		// Advance with overlap. Make sure we still make
		// forward progress even when overlap is large enough
		// to nominally rewind past pos.
		next := end - overlapChars
		if next <= pos {
			next = pos + 1
		}
		pos = next
	}
	return chunks
}

// snapBreakpoint returns a position close to `target` where a
// natural break (paragraph, line, sentence) lives. Looks both
// behind and ahead within ±10% of charBudget; preferred order
// is paragraph break (\n\n) > newline > sentence end. If none
// is found, returns `target` itself for a mid-word cut.
func snapBreakpoint(doc string, start, target, charBudget int) int {
	// Search window: ±10% of the budget, clamped.
	slack := charBudget / 10
	if slack < 64 {
		slack = 64
	}
	low := target - slack
	if low <= start {
		low = start + 1
	}
	high := target + slack
	if high > len(doc) {
		high = len(doc)
	}

	if p := findLastInRange(doc, "\n\n", low, high); p > 0 {
		return p + 2
	}
	if p := findLastInRange(doc, "\n", low, high); p > 0 {
		return p + 1
	}
	// Sentence terminators — check each candidate string.
	for _, term := range []string{". ", "! ", "? ", "。", "！", "？"} {
		if p := findLastInRange(doc, term, low, high); p > 0 {
			return p + len(term)
		}
	}
	return target
}

// findLastInRange returns the highest index in [low, high)
// where substr appears, or -1 if not found.
func findLastInRange(s, substr string, low, high int) int {
	if low >= high || low >= len(s) {
		return -1
	}
	if high > len(s) {
		high = len(s)
	}
	region := s[low:high]
	idx := strings.LastIndex(region, substr)
	if idx < 0 {
		return -1
	}
	return low + idx
}
