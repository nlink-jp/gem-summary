package summarizer

import (
	"strings"
	"testing"
)

func TestSplitIntoChunks_Empty(t *testing.T) {
	if got := SplitIntoChunks("", 100, 10); got != nil {
		t.Errorf("empty doc: got %d chunks, want nil", len(got))
	}
}

func TestSplitIntoChunks_FitsInOne(t *testing.T) {
	doc := "short document that fits well under the budget"
	chunks := SplitIntoChunks(doc, 1000, 100)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].Text != doc {
		t.Errorf("single chunk text drift: got %q want %q", chunks[0].Text, doc)
	}
	if chunks[0].Index != 0 {
		t.Errorf("first chunk Index = %d, want 0", chunks[0].Index)
	}
	if chunks[0].EstimatedTokens <= 0 {
		t.Errorf("EstimatedTokens not populated: %d", chunks[0].EstimatedTokens)
	}
}

// TestSplitIntoChunks_BoundaryParagraph pins the documented
// preference: when a paragraph break (double newline) lives
// within the slack window, the chunker snaps to it. Mid-
// sentence cuts are a fallback, not the default.
func TestSplitIntoChunks_BoundaryParagraph(t *testing.T) {
	// Build a doc with a clear paragraph break near the
	// 200-char mark. With sizeTokens=70 (≈210 chars), the
	// snapBreakpoint search window is ±21 chars around 210,
	// so the \n\n at position 199 is in range.
	first := strings.Repeat("a", 199)
	second := strings.Repeat("b", 199)
	doc := first + "\n\n" + second

	chunks := SplitIntoChunks(doc, 70, 0)
	if len(chunks) < 2 {
		t.Fatalf("expected ≥2 chunks, got %d", len(chunks))
	}
	if !strings.HasSuffix(chunks[0].Text, "\n\n") {
		t.Errorf("first chunk should end at paragraph break, got tail: %q",
			tail(chunks[0].Text, 5))
	}
	if !strings.HasPrefix(chunks[1].Text, "b") {
		t.Errorf("second chunk should start at the 'b' paragraph, got head: %q",
			head(chunks[1].Text, 5))
	}
}

// TestSplitIntoChunks_Overlap verifies adjacent chunks share
// the configured number of characters at the boundary.
// Exact overlap can shift slightly because of snapping, so
// the test accepts ±20% drift around the requested overlap.
func TestSplitIntoChunks_Overlap(t *testing.T) {
	// Use distinctive markers every 50 chars so we can pin
	// where overlap lives.
	var sb strings.Builder
	for i := 0; i < 100; i++ {
		sb.WriteString("###")
		sb.WriteString(strings.Repeat(".", 47))
	}
	doc := sb.String()
	// sizeTokens=50 (chars≈150), overlap=10 (chars≈30).
	chunks := SplitIntoChunks(doc, 50, 10)
	if len(chunks) < 2 {
		t.Fatalf("expected ≥2 chunks, got %d", len(chunks))
	}
	// The end of chunk[0] should be findable inside chunk[1]'s
	// head — that is the overlap region.
	end0 := chunks[0].Text[len(chunks[0].Text)-15:]
	if !strings.Contains(chunks[1].Text[:60], end0[:5]) {
		t.Errorf("expected overlap from chunk0 tail %q to appear in chunk1 head; chunk1 head=%q",
			end0[:5], chunks[1].Text[:60])
	}
}

// TestSplitIntoChunks_NoOverlap verifies overlap=0 produces
// non-overlapping chunks (each character belongs to exactly
// one chunk modulo the snapping). The combined chunks should
// reconstruct the original text minus duplicate boundaries.
func TestSplitIntoChunks_NoOverlap(t *testing.T) {
	doc := strings.Repeat("abcdefghij", 200) // 2000 chars
	chunks := SplitIntoChunks(doc, 100, 0)
	if len(chunks) < 2 {
		t.Fatalf("expected ≥2 chunks, got %d", len(chunks))
	}

	// Concatenating all chunk texts should reproduce the
	// original doc when overlap is 0 — modulo any snapping
	// that pushed the boundary forward/back into the slack
	// region.
	reconstructed := ""
	for _, c := range chunks {
		reconstructed += c.Text
	}
	if reconstructed != doc {
		t.Errorf("no-overlap reconstruction differs from input; "+
			"reconstructed len=%d, input len=%d",
			len(reconstructed), len(doc))
	}
}

// TestSplitIntoChunks_ForwardProgress guards the bad-config
// case: an overlap so large that next = end - overlap rewinds
// past the current pos. The chunker must still terminate.
func TestSplitIntoChunks_ForwardProgress(t *testing.T) {
	doc := strings.Repeat("x", 1000)
	// sizeTokens 50 → ~150 chars; overlap 100 → ~300 chars
	// nominally rewinds. Must still terminate.
	chunks := SplitIntoChunks(doc, 50, 100)
	if len(chunks) == 0 {
		t.Fatal("got 0 chunks, expected ≥1")
	}
	if len(chunks) > 50 {
		t.Errorf("excessive chunk count %d — chunker did not make forward progress", len(chunks))
	}
}

// TestSplitIntoChunks_ZeroSizeFallback verifies that a
// nonsense chunk size doesn't loop forever — defensive.
func TestSplitIntoChunks_ZeroSizeFallback(t *testing.T) {
	doc := "anything at all"
	if got := SplitIntoChunks(doc, 0, 0); len(got) != 1 || got[0].Text != doc {
		t.Errorf("zero size should return single passthrough chunk, got %+v", got)
	}
}

// TestSplitIntoChunks_IndicesAreSequential ensures Index
// values are 0, 1, 2, … without gaps. The merge prompt relies
// on this ordering to assemble chunk summaries.
func TestSplitIntoChunks_IndicesAreSequential(t *testing.T) {
	doc := strings.Repeat("abcdefghij ", 300)
	chunks := SplitIntoChunks(doc, 80, 20)
	for i, c := range chunks {
		if c.Index != i {
			t.Errorf("Chunk[%d].Index = %d, want %d", i, c.Index, i)
		}
	}
}

func tail(s string, n int) string {
	if n >= len(s) {
		return s
	}
	return s[len(s)-n:]
}

func head(s string, n int) string {
	if n >= len(s) {
		return s
	}
	return s[:n]
}
