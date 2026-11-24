package warmupcontent

import (
	"context"
	"errors"
	"testing"

	"github.com/warmbly/warmbly/internal/pkg/generation"
	"github.com/warmbly/warmbly/internal/pkg/typesafe"
)

type reviewGen struct{ reviews int }

func (reviewGen) CheckModel(context.Context, string) error { return nil }
func (reviewGen) SubmitBatch(context.Context, []generation.BatchRequest, string) (string, string, error) {
	return "", "", nil
}
func (reviewGen) GetBatch(context.Context, string) (generation.BatchState, error) {
	return generation.BatchState{}, nil
}
func (reviewGen) CancelBatch(context.Context, string) error { return nil }
func (reviewGen) FetchBatchResults(context.Context, string) ([]generation.BatchResult, error) {
	return nil, nil
}
func (g *reviewGen) ReviewConversation(context.Context, generation.Conversation, string) (*generation.WarmupReview, error) {
	g.reviews++
	return &generation.WarmupReview{Coherent: true, Confidence: 1}, nil
}

type downJudge struct{}

func (downJudge) Ask(context.Context, any, map[string]typesafe.Question) (*typesafe.Response, error) {
	return nil, errors.New("judge down")
}

func TestReviewUsesOpenAIOnlyWithoutAJudge(t *testing.T) {
	gen := &reviewGen{}
	s := &service{gen: gen}
	thread := generation.Conversation{Subject: "Cards", Description: "We found three boxes.", Messages: []string{"I still use them."}}
	if _, err := s.reviewThread(context.Background(), thread, "m"); err != nil {
		t.Fatal(err)
	}
	if gen.reviews != 1 {
		t.Fatalf("reviews without a judge = %d", gen.reviews)
	}
	s.judge = downJudge{}
	_, _ = s.reviewThread(context.Background(), thread, "m")
	if gen.reviews != 1 {
		t.Fatalf("reviews with a judge = %d, want 1", gen.reviews)
	}
}
