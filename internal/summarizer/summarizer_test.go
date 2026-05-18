package summarizer

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/nlink-jp/gem-summary/internal/vertexai"
	"github.com/nlink-jp/nlk/guard"
)

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

// TestSummarize_OverLimit pins the Phase-1 "reject when too
// big" contract that Phase 2 will replace with chunking.
func TestSummarize_OverLimit(t *testing.T) {
	fc := &fakeClient{}
	long := strings.Repeat("token ", 1000)
	_, err := Summarize(context.Background(), fc, long, Options{
		Style:          StyleMedium,
		MaxInputTokens: 100,
	})
	if err == nil {
		t.Fatal("expected over-limit error, got nil")
	}
	if !errors.Is(err, ErrInputTooLarge) {
		t.Errorf("expected ErrInputTooLarge, got: %v", err)
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
