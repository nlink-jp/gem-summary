package summarizer

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/nlink-jp/gem-summary/internal/vertexai"
	"github.com/nlink-jp/nlk/guard"
)

// fakeClientFunc lets tests script the per-call response so a
// single test can assert chunk-vs-merge dispatch by inspecting
// the system prompt.
type fakeClientFunc struct {
	fn func(systemPrompt, userPrompt string) (*vertexai.Response, error)
}

func (c *fakeClientFunc) Generate(_ context.Context, systemPrompt, userPrompt string) (*vertexai.Response, error) {
	return c.fn(systemPrompt, userPrompt)
}

// fakeClient is a minimal stub that records the last call and
// returns a canned response. Used in place of *vertexai.Client
// for unit tests that should not touch the real LLM.
type fakeClient struct {
	gotSystem string
	gotUser   string
	resp      *vertexai.Response
	err       error
}

func (c *fakeClient) Generate(_ context.Context, systemPrompt, userPrompt string) (*vertexai.Response, error) {
	c.gotSystem = systemPrompt
	c.gotUser = userPrompt
	if c.err != nil {
		return nil, c.err
	}
	if c.resp != nil {
		return c.resp, nil
	}
	return &vertexai.Response{Text: "OK", InputTokens: 10, OutputTokens: 5}, nil
}

func TestParseStyle(t *testing.T) {
	cases := []struct {
		in      string
		want    Style
		wantErr bool
	}{
		{"short", StyleShort, false},
		{"SHORT", StyleShort, false}, // lowercase normalisation
		{"  medium ", StyleMedium, false},
		{"", StyleMedium, false}, // empty maps to medium
		{"long", StyleLong, false},
		{"mediium", "", true}, // typo rejected
		{"verbose", "", true}, // out-of-set rejected
	}
	for _, c := range cases {
		got, err := ParseStyle(c.in)
		if (err != nil) != c.wantErr {
			t.Errorf("ParseStyle(%q) err = %v, wantErr=%v", c.in, err, c.wantErr)
			continue
		}
		if !c.wantErr && got != c.want {
			t.Errorf("ParseStyle(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestBuildSystemPrompt_StyleGuidance pins that each style
// produces a recognisably different system prompt. Specific
// phrasing isn't asserted (it can evolve), but the per-style
// instruction MUST land in the output.
func TestBuildSystemPrompt_StyleGuidance(t *testing.T) {
	tag := guard.NewTagWithName("user_data_test")

	short := BuildSystemPrompt(StyleShort, "", tag)
	medium := BuildSystemPrompt(StyleMedium, "", tag)
	long := BuildSystemPrompt(StyleLong, "", tag)

	if short == medium || medium == long || short == long {
		t.Error("system prompt should differ across styles")
	}
	if !strings.Contains(short, "2-4 sentences") {
		t.Errorf("short style should mention 2-4 sentences, got: %s", short)
	}
	if !strings.Contains(long, "multi-paragraph") {
		t.Errorf("long style should mention multi-paragraph, got: %s", long)
	}

	// Every style must reference the tag name so the model
	// knows the wrap boundary.
	for _, s := range []string{short, medium, long} {
		if !strings.Contains(s, tag.Name()) {
			t.Errorf("system prompt should reference tag name %q", tag.Name())
		}
	}
}

// TestBuildSystemPrompt_LangGuidance pins the documented
// behaviour: empty lang → "same language as document"; an
// explicit lang → an instruction to use it.
func TestBuildSystemPrompt_LangGuidance(t *testing.T) {
	tag := guard.NewTagWithName("user_data_test")

	auto := BuildSystemPrompt(StyleMedium, "", tag)
	if !strings.Contains(auto, "same language") {
		t.Errorf("auto-detect should mention 'same language', got: %s", auto)
	}

	ja := BuildSystemPrompt(StyleMedium, "Japanese", tag)
	if !strings.Contains(ja, "Japanese") {
		t.Errorf("explicit lang should appear in prompt, got: %s", ja)
	}
}

// TestBuildUserPrompt_GuardWraps ensures the document body
// lands inside the guard tag, not outside it. This is the
// load-bearing prompt-injection defence: if BuildUserPrompt
// regressed and emitted the doc text outside the tag, an
// attacker-controlled document could pose as instructions.
func TestBuildUserPrompt_GuardWraps(t *testing.T) {
	tag := guard.NewTagWithName("user_data_test")

	out, err := BuildUserPrompt("the doc body", tag)
	if err != nil {
		t.Fatalf("BuildUserPrompt: %v", err)
	}
	want := "<" + tag.Name() + ">the doc body</" + tag.Name() + ">"
	if !strings.Contains(out, want) {
		t.Errorf("user prompt should contain %q\ngot: %s", want, out)
	}
}

// TestBuildUserPrompt_RejectsCollision pins the fail-closed
// behaviour: a document that already contains the tag name
// MUST surface an error so we don't inadvertently summarise
// what looks like a forged closing tag.
func TestBuildUserPrompt_RejectsCollision(t *testing.T) {
	tag := guard.NewTagWithName("user_data_test")
	// Embed the tag name in the document body to simulate the
	// pathological collision case.
	doc := "before " + tag.Name() + " after"

	if _, err := BuildUserPrompt(doc, tag); err == nil {
		t.Fatal("expected collision error, got nil")
	}
}

// TestEstimateTokens validates that the heuristic produces
// non-trivial counts and the documented "max + CJK" combination.
// We don't pin exact numbers — the estimator is a heuristic —
// only the ordering / boundary properties.
func TestEstimateTokens(t *testing.T) {
	if got := EstimateTokens(""); got != 0 {
		t.Errorf("empty string: got %d, want 0", got)
	}

	short := EstimateTokens("Hello world. This is a short English sentence.")
	long := EstimateTokens(strings.Repeat("Hello world. ", 100))
	if !(long > short) {
		t.Errorf("long should estimate higher than short: short=%d long=%d", short, long)
	}

	// CJK gets a per-rune bump on top of the char/word
	// baseline; a 50-char Japanese line should estimate
	// notably higher than 50 ASCII characters.
	en := EstimateTokens(strings.Repeat("a", 50))
	ja := EstimateTokens(strings.Repeat("あ", 50))
	if !(ja > en) {
		t.Errorf("CJK should estimate higher than equivalent-byte-count ASCII: en=%d ja=%d", en, ja)
	}
}

// TestSummarize_HappyPath verifies the end-to-end single-call
// pipeline against fakeClient: prompts get built, the response
// flows back, and the Result reflects vertexai's token counts.
func TestSummarize_HappyPath(t *testing.T) {
	fc := &fakeClient{
		resp: &vertexai.Response{Text: "the summary", InputTokens: 42, OutputTokens: 7},
	}
	res, err := Summarize(context.Background(), fc, "this is a short test document",
		Options{Style: StyleMedium})
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if res.Summary != "the summary" {
		t.Errorf("Summary = %q, want %q", res.Summary, "the summary")
	}
	if res.Chunks != 1 {
		t.Errorf("Chunks = %d, want 1", res.Chunks)
	}
	if res.InputTokens != 42 || res.OutputTokens != 7 {
		t.Errorf("token counts not propagated: %+v", res)
	}
	// System prompt should reach the client.
	if !strings.Contains(fc.gotSystem, "summariser") {
		t.Errorf("system prompt missing role: %s", fc.gotSystem)
	}
	// User prompt should wrap the document.
	if !strings.Contains(fc.gotUser, "this is a short test document") {
		t.Errorf("user prompt missing doc body: %s", fc.gotUser)
	}
}

// TestSummarize_ChunkedPath verifies that an over-threshold
// input flows through the chunked path: multiple chunk
// summaries get produced (via the fake client) and a merge
// call wraps them up. The fake client returns "CHUNK n" /
// "MERGED" responses based on a counter so the assertions can
// see the chain of calls.
func TestSummarize_ChunkedPath(t *testing.T) {
	// ~6000-char input → ~1500 estimated tokens, over the
	// 1000-token MaxInputTokens cap so the chunked path
	// activates. ChunkSize=200 gives ~10 chunks; the merge
	// prompt with 10 "CHUNK" stubs lands well under the cap,
	// so exactly one merge call.
	long := strings.Repeat("paragraph one. ", 400)

	var mu sync.Mutex
	var calls []string
	fc := &fakeClientFunc{
		fn: func(systemPrompt, userPrompt string) (*vertexai.Response, error) {
			mu.Lock()
			defer mu.Unlock()
			isMerge := strings.Contains(systemPrompt, "merging multiple")
			if isMerge {
				calls = append(calls, "merge")
				return &vertexai.Response{Text: "MERGED", InputTokens: 200, OutputTokens: 50}, nil
			}
			calls = append(calls, "chunk")
			return &vertexai.Response{Text: "CHUNK", InputTokens: 100, OutputTokens: 20}, nil
		},
	}
	res, err := Summarize(context.Background(), fc, long, Options{
		Style:            StyleMedium,
		MaxInputTokens:   1000, // doc ~1300 tokens → chunked path
		ChunkSize:        200,
		ChunkOverlap:     10,
		ChunkParallelism: 3,
	})
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if res.Summary != "MERGED" {
		t.Errorf("Summary = %q, want MERGED", res.Summary)
	}

	mu.Lock()
	defer mu.Unlock()
	chunkCount := 0
	mergeCount := 0
	for _, c := range calls {
		switch c {
		case "chunk":
			chunkCount++
		case "merge":
			mergeCount++
		}
	}
	if chunkCount < 2 {
		t.Errorf("expected ≥2 chunk calls, got %d", chunkCount)
	}
	if mergeCount != 1 {
		t.Errorf("expected exactly 1 merge call, got %d", mergeCount)
	}
	// Result.Chunks should include the merge call (chunks + merge).
	if res.Chunks != chunkCount+mergeCount {
		t.Errorf("Result.Chunks = %d, want %d (%d chunks + %d merge)",
			res.Chunks, chunkCount+mergeCount, chunkCount, mergeCount)
	}
	// Tokens should be aggregated.
	wantIn := chunkCount*100 + mergeCount*200
	wantOut := chunkCount*20 + mergeCount*50
	if res.InputTokens != wantIn || res.OutputTokens != wantOut {
		t.Errorf("aggregated tokens: in=%d out=%d, want in=%d out=%d",
			res.InputTokens, res.OutputTokens, wantIn, wantOut)
	}
}

// TestSummarize_ChunkedPath_PropagatesError pins the error
// path: if a per-chunk generate fails, Summarize returns
// (nil, err) and does NOT swallow partial results.
func TestSummarize_ChunkedPath_PropagatesError(t *testing.T) {
	long := strings.Repeat("paragraph one. ", 1000)
	chunkErr := errors.New("simulated 429")
	var mu sync.Mutex
	calls := 0
	fc := &fakeClientFunc{
		fn: func(systemPrompt, userPrompt string) (*vertexai.Response, error) {
			mu.Lock()
			defer mu.Unlock()
			calls++
			// Fail on the 2nd chunk call to exercise the
			// cancellation path.
			if calls == 2 {
				return nil, chunkErr
			}
			return &vertexai.Response{Text: "ok"}, nil
		},
	}
	_, err := Summarize(context.Background(), fc, long, Options{
		Style:            StyleMedium,
		MaxInputTokens:   500,
		ChunkSize:        100,
		ChunkParallelism: 1, // serial so the 2nd call is deterministically 2nd
	})
	if err == nil {
		t.Fatal("expected chunked error, got nil")
	}
	if !strings.Contains(err.Error(), "simulated 429") {
		t.Errorf("error didn't propagate chunk error string: %v", err)
	}
}

// TestSummarize_SingleChunkFallsThroughToSingleCall pins the
// optimisation: when the chunker produces only 1 chunk
// (because the doc fits in one window) the merge call is
// skipped — no point paying for a merge LLM round on a
// single-chunk doc.
func TestSummarize_SingleChunkFallsThroughToSingleCall(t *testing.T) {
	doc := "this is a moderately sized input"
	fc := &fakeClientFunc{
		fn: func(systemPrompt, _ string) (*vertexai.Response, error) {
			if strings.Contains(systemPrompt, "merging multiple") {
				t.Error("merge call should not happen for single-chunk fallback")
			}
			return &vertexai.Response{Text: "single", InputTokens: 5, OutputTokens: 2}, nil
		},
	}
	res, err := Summarize(context.Background(), fc, doc, Options{
		Style:          StyleMedium,
		MaxInputTokens: 3,           // tiny cap forces chunked entry
		ChunkSize:      10_000_000,  // but chunks are huge → 1 chunk
	})
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if res.Chunks != 1 {
		t.Errorf("Chunks = %d, want 1 (single-chunk fallback)", res.Chunks)
	}
}

// TestSummarize_ProgressCallback ensures the Options.Progress
// hook fires at least once so the CLI's --quiet vs default
// modes have something to gate on.
func TestSummarize_ProgressCallback(t *testing.T) {
	fc := &fakeClient{resp: &vertexai.Response{Text: "ok"}}
	var lines []string
	_, err := Summarize(context.Background(), fc, "doc", Options{
		Style:    StyleMedium,
		Progress: func(s string) { lines = append(lines, s) },
	})
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if len(lines) == 0 {
		t.Error("expected at least one progress callback")
	}
}
