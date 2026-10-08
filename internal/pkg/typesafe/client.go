package typesafe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"strconv"
	"time"

	"github.com/rs/zerolog/log"
)

// Package typesafe is the one client for the TypeSafe API, shared by every
// feature that asks it a question: inbox tagging, the reply classifier, the
// copy judgment, warmup content lint, bounce classification and form triage.
//
// TypeSafe is not a language model and generates no text. It takes a state and
// a set of typed questions and returns typed answers with calibrated
// probabilities. That is the whole reason it is here rather than an LLM: the
// output is a number this code can threshold on, not prose to be parsed.
//
// Docs: https://docs.typesafe.ai/api and https://docs.typesafe.ai/primitives
const defaultEndpoint = "https://api.typesafe.ai/v1/systemone"

// Model is pinned, never an alias. "jev-latest" moves, and every threshold in
// every policy that reads an answer was calibrated against this exact version;
// a silent model change would shift all of them at once with nothing in the
// diff to show for it.
const Model = "jev-1.13.0"

// QuestionType names the three primitives used here. The API also has a
// bounding-box type, which has nothing to do with email.
const (
	QuestionNoul   = "noul"
	QuestionChoice = "choice"
	QuestionScore  = "score"
)

// Question is one typed question. Criteria is a map for choice (option ->
// description), a []string for score (ordered levels, at most ten), and either
// absent or a {"true","false"} map for noul.
type Question struct {
	Type         string `json:"type"`
	Instructions string `json:"instructions"`
	Criteria     any    `json:"criteria,omitempty"`
}

// Noul, Choice and Score build one question each. Every policy in the tree
// spells its questions through these so a typo in a type name is a compile
// error rather than a 422 at runtime.
func Noul(instructions string) Question {
	return Question{Type: QuestionNoul, Instructions: instructions}
}

func Choice(instructions string, options map[string]string) Question {
	return Question{Type: QuestionChoice, Instructions: instructions, Criteria: options}
}

func Score(instructions string, levels []string) Question {
	return Question{Type: QuestionScore, Instructions: instructions, Criteria: levels}
}

// Normalized is a score answer as a 0..1 position on its rubric: the only
// arithmetic ever done with a score, done here because Jev is bad at it.
func (a Answer) Normalized(levels int) float64 {
	if levels < 2 {
		return 0
	}
	v := a.Score / float64(levels-1)
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// Asker is the narrow interface every feature depends on, so tests supply a
// recorded response instead of a network.
type Asker interface {
	Ask(ctx context.Context, state any, questions map[string]Question) (*Response, error)
}

type request struct {
	State     any                 `json:"state"`
	Model     string              `json:"model"`
	Questions map[string]Question `json:"questions"`
}

// Answer is the union of the three answer shapes. Which fields are populated
// is decided by Type.
//
// Noul carries no Confidence: the noul value IS the probability, so a separate
// confidence would be the same number twice.
type Answer struct {
	Type string `json:"type"`

	Noul float64 `json:"noul"`

	Choice string `json:"choice"`

	Score  float64           `json:"score"`
	Legend map[string]string `json:"legend"`

	Probabilities map[string]float64 `json:"probabilities"`
	Confidence    float64            `json:"confidence"`
}

// Response is one call's worth of answers. Output tokens are free; input is
// billed, and is recorded so a workspace can see what the feature costs.
type Response struct {
	Model   string            `json:"model"`
	Answers map[string]Answer `json:"answers"`
	Usage   struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// Client calls the TypeSafe API. Zero value is not usable; use NewClient.
type Client struct {
	endpoint string
	apiKey   string
	http     *http.Client
	// maxAttempts bounds the retry loop. Retries cover 429 and 529 only.
	maxAttempts int
	// sleep is the delay function, swapped in tests so a backoff assertion
	// does not cost wall-clock seconds.
	sleep   func(context.Context, time.Duration) error
	billing *billingGate
}

type Option func(*Client)

func NewClient(apiKey string, options ...Option) *Client {
	c := &Client{
		endpoint:    defaultEndpoint,
		apiKey:      apiKey,
		http:        &http.Client{Timeout: 30 * time.Second},
		maxAttempts: 4,
		sleep:       sleepCtx,
		billing:     newBillingGate(apiKey),
	}
	for _, option := range options {
		option(c)
	}
	return c
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// APIError is a non-2xx response, with the message dug out of whichever shape
// `detail` arrived in.
type APIError struct {
	Status     int
	Message    string
	RetryAfter time.Duration
	// Retryable marks 429 and 529, the two the caller may try again.
	Retryable bool
}

func (e *APIError) Error() string {
	return fmt.Sprintf("typesafe: %d: %s", e.Status, e.Message)
}

// parseDetail pulls a human message out of an error body.
//
// `detail` has three different shapes, and each was confirmed against the live
// API rather than taken from the docs:
//
//	object — {"detail":{"error_type":"api_usage_error","message":"Unknown model: …"}}
//	         (400 unknown model, 401 bad key)
//	string — {"detail":"Too many score levels. Must have at most 10 levels."}
//	         (400 schema violation, e.g. an eleven-level score)
//	array  — {"detail":[{"type":"union_tag_invalid","loc":[…],"msg":"…"}]}
//	         (422 request validation)
//
// One parser handles all three; a parser written for any one of them turns the
// other two into an empty error message at exactly the moment you need it.
func parseDetail(body []byte) string {
	var envelope struct {
		Detail json.RawMessage `json:"detail"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || len(envelope.Detail) == 0 {
		if s := string(bytes.TrimSpace(body)); s != "" {
			return truncate(s, 300)
		}
		return "no detail in response"
	}

	// String.
	var asString string
	if err := json.Unmarshal(envelope.Detail, &asString); err == nil {
		return truncate(asString, 300)
	}

	// Object.
	var asObject struct {
		ErrorType string `json:"error_type"`
		Message   string `json:"message"`
	}
	if err := json.Unmarshal(envelope.Detail, &asObject); err == nil && asObject.Message != "" {
		if asObject.ErrorType != "" {
			return truncate(asObject.ErrorType+": "+asObject.Message, 300)
		}
		return truncate(asObject.Message, 300)
	}

	// Array of validation items.
	var asArray []struct {
		Msg string   `json:"msg"`
		Loc []any    `json:"loc"`
		Typ string   `json:"type"`
		Ctx any      `json:"ctx"`
		In  any      `json:"input"`
		_   struct{} // keep the decoder tolerant of new fields
	}
	if err := json.Unmarshal(envelope.Detail, &asArray); err == nil && len(asArray) > 0 {
		msg := asArray[0].Msg
		if msg == "" {
			msg = asArray[0].Typ
		}
		if len(asArray) > 1 {
			msg = fmt.Sprintf("%s (and %d more validation errors)", msg, len(asArray)-1)
		}
		return truncate(msg, 300)
	}

	return truncate(string(envelope.Detail), 300)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// Ask sends one state and every question in one call against the pinned
// model, and retries only the two statuses that mean "try again later".
func (c *Client) Ask(ctx context.Context, state any, questions map[string]Question) (*Response, error) {
	payload, err := json.Marshal(request{State: state, Model: Model, Questions: questions})
	if err != nil {
		return nil, fmt.Errorf("typesafe: encode request: %w", err)
	}
	permit, err := c.billing.acquire(ctx)
	if err != nil {
		return nil, err
	}
	var outcome error
	defer func() { c.billing.finish(ctx, permit, outcome) }()

	var lastErr error
	for attempt := 0; attempt < c.maxAttempts; attempt++ {
		if attempt > 0 {
			wait := backoff(attempt, lastErr)
			if err := c.sleep(ctx, wait); err != nil {
				outcome = err
				return nil, err
			}
		}

		resp, err := c.once(ctx, payload)
		entry := log.Info().Str("event", "typesafe_usage").Str("feature", featureFrom(ctx)).Int("questions", len(questions)).Int("attempt", attempt+1)
		if scope, ok := ctx.Value(organizationKey{}).(string); ok && scope != "" {
			entry.Str("organization_id", scope)
		}
		if err == nil {
			outcome = nil
			entry.Int("status", http.StatusOK).Str("model", resp.Model).Int("input_tokens", resp.Usage.InputTokens).Msg("TypeSafe request completed")
			return resp, nil
		}
		var status *APIError
		if asAPIError(err, &status) {
			entry.Int("status", status.Status)
		} else {
			entry.Int("status", 0)
		}
		entry.Msg("TypeSafe request failed")
		outcome = err
		lastErr = err

		var apiErr *APIError
		if !asAPIError(err, &apiErr) || !apiErr.Retryable {
			return nil, err
		}
	}
	return nil, fmt.Errorf("typesafe: giving up after %d attempts: %w", c.maxAttempts, lastErr)
}

func (c *Client) once(ctx context.Context, payload []byte) (*Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("typesafe: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	httpResp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("typesafe: %w", err)
	}
	defer httpResp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(httpResp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("typesafe: read response: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		apiErr := &APIError{
			Status:    httpResp.StatusCode,
			Message:   parseDetail(body),
			Retryable: httpResp.StatusCode == http.StatusTooManyRequests || httpResp.StatusCode == 529,
		}
		if apiErr.Retryable {
			apiErr.RetryAfter = parseRetryAfter(httpResp.Header.Get("Retry-After"))
		}
		return nil, apiErr
	}

	var out Response
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("typesafe: decode response: %w", err)
	}
	return &out, nil
}

// parseRetryAfter reads the header in both forms the RFC allows: delta-seconds,
// and an HTTP date.
func parseRetryAfter(v string) time.Duration {
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

// backoff honours the server's Retry-After when it sent one, and otherwise
// doubles with jitter. Jitter matters here: an inbox flood retries in lockstep
// without it, and arrives back at the same overloaded service together.
func backoff(attempt int, err error) time.Duration {
	var apiErr *APIError
	if asAPIError(err, &apiErr) && apiErr.RetryAfter > 0 {
		return min(apiErr.RetryAfter, 60*time.Second)
	}
	base := time.Duration(math.Pow(2, float64(attempt))) * 250 * time.Millisecond
	jitter := time.Duration(rand.Int63n(int64(base/2 + 1)))
	return min(base+jitter, 30*time.Second)
}

func asAPIError(err error, target **APIError) bool {
	if e, ok := err.(*APIError); ok {
		*target = e
		return true
	}
	return false
}
