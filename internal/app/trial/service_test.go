package trial

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// memSubs models CreateTrial's single statement: the owner's claim and the row commit together or not at all.
type memSubs struct {
	repository.SubscriptionRepository
	mu      sync.Mutex
	byOrg   map[uuid.UUID]*models.Subscription
	used    map[uuid.UUID]bool
	failing bool
}

func newMemSubs() *memSubs {
	return &memSubs{byOrg: map[uuid.UUID]*models.Subscription{}, used: map[uuid.UUID]bool{}}
}

func (m *memSubs) GetByOrganizationID(_ context.Context, orgID uuid.UUID) (*models.Subscription, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.byOrg[orgID], nil
}

func (m *memSubs) CreateTrial(_ context.Context, sub *models.Subscription) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failing {
		return false, errors.New("write failed")
	}
	if m.used[sub.UserID] {
		return false, nil
	}
	m.used[sub.UserID] = true
	m.byOrg[sub.OrganizationID] = sub
	return true, nil
}

// An owner's first workspace gets the trial; every later one starts with no subscription.
func TestOneTrialPerOwner(t *testing.T) {
	ctx := context.Background()
	subs := newMemSubs()
	svc := NewService(subs, nil, nil)
	owner, first, second := uuid.New(), uuid.New(), uuid.New()

	if err := svc.StartFreeTrialWithOrg(ctx, owner, first); err != nil {
		t.Fatal(err)
	}
	sub := subs.byOrg[first]
	if sub == nil || !sub.IsInFreeTrial() {
		t.Fatal("the first workspace must start in its trial")
	}
	if left := time.Until(*sub.FreeTrialEndsAt); left < TrialDuration-time.Minute || left > TrialDuration {
		t.Errorf("trial ends in %v, want %v", left, TrialDuration)
	}

	if err := svc.StartFreeTrialWithOrg(ctx, owner, second); err != nil {
		t.Fatal(err)
	}
	if subs.byOrg[second] != nil {
		t.Error("a second workspace for the same owner must not get a trial")
	}

	if err := svc.StartFreeTrialWithOrg(ctx, owner, first); err != nil || subs.byOrg[first] != sub {
		t.Error("starting again on a workspace with a subscription must change nothing")
	}
}

// A trial whose write failed was never claimed, so the owner's next workspace still gets it.
func TestFailedTrialKeepsTheClaim(t *testing.T) {
	ctx := context.Background()
	subs := newMemSubs()
	subs.failing = true
	svc := NewService(subs, nil, nil)
	owner, broken, next := uuid.New(), uuid.New(), uuid.New()

	if err := svc.StartFreeTrialWithOrg(ctx, owner, broken); err == nil {
		t.Fatal("a failed subscription write must be reported")
	}
	subs.failing = false
	if err := svc.StartFreeTrialWithOrg(ctx, owner, next); err != nil {
		t.Fatal(err)
	}
	if sub := subs.byOrg[next]; sub == nil || !sub.IsInFreeTrial() {
		t.Error("the owner's next workspace must get the trial the failed one never received")
	}
}
