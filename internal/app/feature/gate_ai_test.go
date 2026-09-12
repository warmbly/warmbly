package feature

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// Only the one read the AI gates make is implemented; anything else panics,
// which is the point.
type stubSubs struct {
	repository.SubscriptionRepository
	sub *models.Subscription
}

func (s stubSubs) GetByOrganizationID(context.Context, uuid.UUID) (*models.Subscription, error) {
	return s.sub, nil
}

func trialSub() *models.Subscription {
	future := time.Now().Add(14 * 24 * time.Hour)
	return &models.Subscription{
		PlanID:          uuid.MustParse("00000000-0000-0000-0000-000000000001"),
		Status:          models.SubscriptionStatusTrialing,
		FreeTrialEndsAt: &future,
	}
}

// The free plan carries no credit allowance, so the assistant must be locked
// rather than shown. Offering it to someone who can only ever be told they
// have no credits reads as a broken feature, not a paid one.
func TestWritingAssistantIsPaidOnly(t *testing.T) {
	g := &featureGateService{subRepo: stubSubs{sub: trialSub()}}

	ok, xerr := g.CanUseWritingAssistant(context.Background(), uuid.New())
	if xerr != nil {
		t.Fatalf("unexpected error: %v", xerr)
	}
	if ok {
		t.Error("a free trial must not be offered the writing assistant")
	}
}

func TestWritingAssistantAndInboxAgentAgree(t *testing.T) {
	ctx := context.Background()
	org := uuid.New()
	g := &featureGateService{subRepo: stubSubs{sub: trialSub()}}

	wa, _ := g.CanUseWritingAssistant(ctx, org)
	ia, _ := g.CanUseInboxAgent(ctx, org)
	if wa != ia {
		t.Errorf("both AI features should gate the same way on a free trial: assistant=%v agent=%v", wa, ia)
	}
}

// No subscription at all is still a refusal, not a panic.
func TestAIGatesRefuseWithNoSubscription(t *testing.T) {
	g := &featureGateService{subRepo: stubSubs{sub: nil}}
	if ok, _ := g.CanUseWritingAssistant(context.Background(), uuid.New()); ok {
		t.Error("no subscription must not unlock the assistant")
	}
}

// A self-hosted instance has no payment provider, so every gate stays open.
func TestSelfHostKeepsAIOpen(t *testing.T) {
	g := &featureGateService{selfHost: true}
	if ok, _ := g.CanUseWritingAssistant(context.Background(), uuid.New()); !ok {
		t.Error("self-host must keep the assistant available")
	}
}
