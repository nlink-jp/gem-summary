package summarizer

import (
	"strings"
	"unicode"
)

// EstimateTokens returns an approximate input-token count for
// the given text. The Vertex AI API will give us the exact
// count after the call (UsageMetadata), but the chunking
// decision needs a pre-call estimate.
//
// Heuristic: max of two cheap estimators —
//
//   - chars / 4   : conservative for English-heavy text
//                   (close to the OpenAI tokenizer rule of
//                   thumb; underestimates CJK by ~3-5x because
//                   CJK characters average 1 token each).
//   - words * 1.3 : closer to truth for natural-language English
//                   when words are space-separated.
//
// Taking the max biases toward over-estimation, which is the
// correct direction for a chunking threshold: an extra
// false-positive chunk costs one extra LLM call, but a missed
// over-limit produces a hard API error halfway through the
// pipeline. CJK is handled separately by counting unicode
// runes that are not whitespace and not Latin letters / digits
// — for these scripts the rune count IS effectively the token
// count, so we add that to the max.
//
// This is good enough for chunking decisions; the JSON output
// uses Vertex AI's exact post-call counts.
func EstimateTokens(s string) int {
	if s == "" {
		return 0
	}
	charBased := (len(s) + 3) / 4

	// Words via Fields handles arbitrary whitespace runs.
	words := len(strings.Fields(s))
	wordBased := (words * 13) / 10

	// CJK / non-Latin rune count.
	cjk := 0
	for _, r := range s {
		if unicode.IsSpace(r) {
			continue
		}
		// crude "is CJK / extended script" check — covers
		// Hiragana / Katakana / Han / Hangul ranges plus
		// general non-Latin scripts.
		if r >= 0x3040 && r <= 0x9fff {
			cjk++
		} else if r >= 0xac00 && r <= 0xd7af {
			cjk++
		}
	}

	max := charBased
	if wordBased > max {
		max = wordBased
	}
	// CJK rune count is roughly 1:1 with tokens; add as a
	// separate component rather than max-ing so a mixed
	// CJK+English document gets credit for both.
	return max + cjk
}
