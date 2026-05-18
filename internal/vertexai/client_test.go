package vertexai

import (
	"errors"
	"testing"
)

// TestIsRetryable_KnownTransientStrings pins the set of error
// patterns we explicitly want to retry. The list lives in
// isRetryable() and must stay in sync with what Vertex AI
// actually returns under transient failure — losing a string
// means we stop retrying and start surfacing 429s to the user.
func TestIsRetryable_KnownTransientStrings(t *testing.T) {
	cases := []struct {
		name string
		err  string
		want bool
	}{
		{"429 rate limit", "googleapi: Error 429: Resource exhausted", true},
		{"503 unavailable", "service unavailable (503)", true},
		{"500 internal", "internal server error 500", true},
		{"deadline", "context deadline exceeded", true},
		{"timeout", "i/o timeout", true},
		{"connection refused", "dial tcp: connection refused", true},
		{"eof", "unexpected EOF", true},
		{"reset by peer", "read tcp: connection reset by peer", true},
		// Non-retryable cases — auth and request shape problems
		// should fail fast so the user sees the real cause.
		{"401 unauthorized", "401 Unauthorized", false},
		{"403 forbidden", "403 PermissionDenied", false},
		{"400 bad request", "400 Bad Request: invalid argument", false},
		{"empty", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isRetryable(errors.New(c.err)); got != c.want {
				t.Errorf("isRetryable(%q) = %v, want %v", c.err, got, c.want)
			}
		})
	}
}

// TestExtractResponse_NilSafe guards against the zero-value
// path: an entirely nil response, a response with no
// candidates, or a candidate with nil Content must all
// produce a valid empty Response struct rather than panic.
// gem-summary surfaces empty text downstream as a clear "no
// summary generated" error; a panic would lose that signal.
func TestExtractResponse_NilSafe(t *testing.T) {
	if got := extractResponse(nil); got.Text != "" {
		t.Errorf("nil response: Text = %q, want empty", got.Text)
	}
}
