package inboxtag

import (
	"context"

	"github.com/warmbly/warmbly/internal/pkg/typesafe"
)

// The API client lives in internal/pkg/typesafe and is shared by every feature
// that asks TypeSafe a question. This file keeps the names the policy and the
// recorded fixtures were written against.
type (
	Question = typesafe.Question
	Answer   = typesafe.Answer
	Response = typesafe.Response
)

const (
	QuestionNoul   = typesafe.QuestionNoul
	QuestionChoice = typesafe.QuestionChoice
	QuestionScore  = typesafe.QuestionScore
)

// Client asks the shared client the tagging questions.
type Client struct {
	inner typesafe.Asker
}

// NewClient builds a client for one API key.
func NewClient(apiKey string, options ...typesafe.Option) *Client {
	return &Client{inner: typesafe.NewClient(apiKey, options...)}
}

// NewAsker wraps an already-built client, so one process shares one HTTP
// client and one retry policy across features.
func NewAsker(inner typesafe.Asker) *Client {
	return &Client{inner: inner}
}

// Ask sends one state and every question in one call.
func (c *Client) Ask(ctx context.Context, state any, questions map[string]Question) (*Response, error) {
	return c.inner.Ask(ctx, state, questions)
}
