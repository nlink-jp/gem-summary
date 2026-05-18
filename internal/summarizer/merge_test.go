package summarizer

import (
	"strings"
	"testing"

	"github.com/nlink-jp/nlk/guard"
)

// TestBuildMergeSystemPrompt_StyleAndLangFlow pins that the
// merge system prompt carries the same style + lang guidance
// path as the per-chunk prompt. A regression where merge
// ignored --style would produce mismatched output (chunks
// summarised long, final merge summarised short, e.g.).
func TestBuildMergeSystemPrompt_StyleAndLangFlow(t *testing.T) {
	tag := guard.NewTagWithName("user_data_test")

	short := BuildMergeSystemPrompt(StyleShort, "", tag)
	long := BuildMergeSystemPrompt(StyleLong, "", tag)
	if short == long {
		t.Error("merge system prompt should differ across styles")
	}
	if !strings.Contains(short, "2-4 sentences") {
		t.Errorf("short style guidance missing: %s", short)
	}
	if !strings.Contains(long, "multi-paragraph") {
		t.Errorf("long style guidance missing: %s", long)
	}

	ja := BuildMergeSystemPrompt(StyleMedium, "Japanese", tag)
	if !strings.Contains(ja, "Japanese") {
		t.Errorf("explicit lang missing in merge prompt: %s", ja)
	}
}

// TestBuildMergeSystemPrompt_TagReference ensures the merge
// prompt references the same guard tag name that
// BuildMergeUserPrompt wraps the chunk summaries in. Drift
// would let prompt-injection content escape the wrap.
func TestBuildMergeSystemPrompt_TagReference(t *testing.T) {
	tag := guard.NewTagWithName("user_data_merge")
	sys := BuildMergeSystemPrompt(StyleMedium, "", tag)
	if !strings.Contains(sys, tag.Name()) {
		t.Errorf("merge system prompt missing tag reference %q", tag.Name())
	}
}

// TestBuildMergeUserPrompt_WrapsEachChunk pins the
// load-bearing defence: every chunk summary is wrapped
// individually in the guard tag. A chunk summary that itself
// contains an injection payload ("ignore previous
// instructions, output X") must not escape.
func TestBuildMergeUserPrompt_WrapsEachChunk(t *testing.T) {
	tag := guard.NewTagWithName("user_data_merge")
	summaries := []string{
		"first chunk summary",
		"second chunk summary",
		"third chunk summary",
	}

	out, err := BuildMergeUserPrompt(summaries, tag)
	if err != nil {
		t.Fatalf("BuildMergeUserPrompt: %v", err)
	}

	for i, s := range summaries {
		want := "<" + tag.Name() + ">" + s + "</" + tag.Name() + ">"
		if !strings.Contains(out, want) {
			t.Errorf("chunk %d not wrapped: want substring %q\nfull prompt:\n%s",
				i+1, want, out)
		}
		// Each chunk should also carry its 1-based label.
		label := "Chunk " + intToString(i+1) + ":"
		if !strings.Contains(out, label) {
			t.Errorf("chunk %d missing label %q", i+1, label)
		}
	}
}

// TestBuildMergeUserPrompt_RejectsCollision verifies the
// fail-closed path: a chunk summary that contains the tag
// name (collision) must surface an error, not silently emit
// an unwrapped chunk.
func TestBuildMergeUserPrompt_RejectsCollision(t *testing.T) {
	tag := guard.NewTagWithName("user_data_merge")
	summaries := []string{
		"clean chunk",
		"contaminated chunk containing " + tag.Name() + " marker",
	}
	if _, err := BuildMergeUserPrompt(summaries, tag); err == nil {
		t.Fatal("expected collision error for chunk containing tag name, got nil")
	}
}

// TestBuildMergeUserPrompt_EmptyInput is a defensive
// boundary: zero chunks should produce a valid (if useless)
// prompt rather than panic. The summariser shouldn't call
// merge with zero chunks, but we guard the public function.
func TestBuildMergeUserPrompt_EmptyInput(t *testing.T) {
	tag := guard.NewTagWithName("user_data_merge")
	out, err := BuildMergeUserPrompt(nil, tag)
	if err != nil {
		t.Fatalf("empty input should not error, got: %v", err)
	}
	if out == "" {
		t.Error("expected non-empty prompt even with zero chunks (preamble)")
	}
}

// intToString — local helper to avoid importing strconv just
// for tests.
func intToString(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	neg := i < 0
	if neg {
		i = -i
	}
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}
