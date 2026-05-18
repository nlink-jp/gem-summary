// Package vertexai is a thin Vertex AI Gemini client used by
// gem-summary's summariser. It wraps google.golang.org/genai
// with retry handling and the THOUGHT-leak filter, but
// deliberately keeps the surface small — gem-summary calls
// Generate(systemPrompt, userPrompt) and gets back the cleaned
// summary text.
//
// Other gem-* tools (gem-search, gem-image, gem-rag, ...) ship
// their own thin wrappers in this same shape; we don't share
// one library across them because the per-tool prompts /
// tool-config additions (Grounding, multimodal, etc.) diverge
// faster than a shared abstraction can keep up.
package vertexai

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/nlink-jp/nlk/backoff"
	"github.com/nlink-jp/nlk/strip"
	"google.golang.org/genai"
)

// maxRetries caps the number of retry attempts on retryable
// failures (429 / 5xx / transport). Beyond this we surface the
// last error to the caller so the CLI can exit with a useful
// status; the chunked path in summariser will surface
// per-chunk failures the same way.
const maxRetries = 5

// Response is what Generate returns. Kept minimal — gem-summary
// only needs the cleaned text — but the struct shape makes
// future additions (token counts, finish reason) less invasive.
type Response struct {
	Text         string
	InputTokens  int
	OutputTokens int
}

// Client is the per-process Gemini handle for gem-summary. One
// instance is shared across the single-call and chunked paths.
type Client struct {
	inner   *genai.Client
	model   string
	timeout time.Duration
}

// NewClient creates a Vertex AI Gemini client. timeoutSec
// caps each underlying GenerateContent call; pass 0 to use the
// SDK default (currently ~5 minutes), or the config-level
// request_timeout from gem-summary's TOML.
func NewClient(ctx context.Context, project, location, model string, timeoutSec int) (*Client, error) {
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		Backend:  genai.BackendVertexAI,
		Project:  project,
		Location: location,
	})
	if err != nil {
		return nil, fmt.Errorf("vertex AI client: %w", err)
	}
	c := &Client{
		inner: client,
		model: model,
	}
	if timeoutSec > 0 {
		c.timeout = time.Duration(timeoutSec) * time.Second
	}
	return c, nil
}

// Model returns the configured model name. Used by the CLI's
// progress output ("Using model X, …").
func (c *Client) Model() string {
	return c.model
}

// Generate sends a one-shot prompt and returns the response.
//
// systemPrompt becomes the SystemInstruction (high-priority
// guidance the model honours across the whole turn). userPrompt
// becomes the single user Content. The summariser layer is
// responsible for wrapping any untrusted document text in
// guard-tagged blocks before passing it as userPrompt.
//
// Retries are limited to the documented set of transient
// failures (429 / 5xx / transport) via backoff. Anything else
// (auth failures, schema errors) returns immediately.
func (c *Client) Generate(ctx context.Context, systemPrompt, userPrompt string) (*Response, error) {
	if c.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}

	contents := []*genai.Content{
		genai.NewContentFromText(userPrompt, "user"),
	}
	cfg := &genai.GenerateContentConfig{
		SystemInstruction: genai.NewContentFromText(systemPrompt, "system"),
		// IncludeThoughts defaults to false in the SDK but
		// Gemini 2.5 Flash has been observed leaking a Part
		// with Thought=false whose text starts with
		// "THOUGHT\n" / "思考\n" anyway. extractText below
		// applies strip.ThinkTags to the joined text as the
		// defence-in-depth pass; this flag is the polite ask.
		ThinkingConfig: &genai.ThinkingConfig{
			IncludeThoughts: false,
		},
	}

	bo := backoff.New(
		backoff.WithBase(2*time.Second),
		backoff.WithMax(30*time.Second),
	)

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		resp, err := c.inner.Models.GenerateContent(ctx, c.model, contents, cfg)
		if err == nil {
			return extractResponse(resp), nil
		}
		lastErr = err
		if !isRetryable(err) || attempt == maxRetries {
			return nil, fmt.Errorf("vertex AI generate: %w", err)
		}
		wait := bo.Duration(attempt)
		log.Printf("vertex AI call failed (attempt %d/%d), retrying in %v: %v",
			attempt+1, maxRetries+1, wait.Round(time.Second), err)
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return nil, fmt.Errorf("vertex AI generate cancelled: %w", ctx.Err())
		}
	}
	return nil, fmt.Errorf("vertex AI generate failed after %d retries: %w", maxRetries, lastErr)
}

// extractResponse pulls the text out of a Gemini response,
// filtering Thought parts at the structural level and running
// strip.ThinkTags as a final pass for the cases where the
// model returned a single Part that mixed THOUGHT preamble
// with the real answer.
func extractResponse(resp *genai.GenerateContentResponse) *Response {
	out := &Response{}
	if resp == nil || len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil {
		return out
	}
	var parts []string
	for _, p := range resp.Candidates[0].Content.Parts {
		if p.Thought {
			continue
		}
		if p.Text != "" {
			parts = append(parts, p.Text)
		}
	}
	out.Text = strip.ThinkTags(strings.Join(parts, ""))
	if resp.UsageMetadata != nil {
		out.InputTokens = int(resp.UsageMetadata.PromptTokenCount)
		out.OutputTokens = int(resp.UsageMetadata.CandidatesTokenCount)
	}
	return out
}

// isRetryable classifies whether the error is worth retrying.
// Conservative: only well-known transient failure strings
// trigger a retry; anything else (auth failures, bad-request
// schema errors) returns immediately so the user sees the real
// cause rather than spinning the back-off.
func isRetryable(err error) bool {
	msg := strings.ToLower(err.Error())
	for _, needle := range []string{
		"429", "503", "500", "502",
		"unavailable", "deadline", "timeout",
		"connection refused", "eof", "reset by peer",
	} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}
