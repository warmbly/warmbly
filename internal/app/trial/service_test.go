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

type memSubs struct {
	repository.SubscriptionRepository
	mu      sync.Mutex
	byOrg   map[uuid.UUID]*models.Subscription
	failing bool
}

func (m *memSubs) GetByOrganizationID(_ context.Context, orgID uuid.UUID) (*models.Subscription, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.byOrg[orgID], nil
}

func (m *memSubs) Create(_ context.Context, sub *models.Subscription) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failing {
		return errors.New("write failed")
	}
	m.byOrg[sub.OrganizationID] = sub
	return nil
}

type memUsers struct {
	repository.UserRepository
	mu   sync.Mutex
	used map[uuid.UUID]bool
}

func (m *memUsers) ReleaseFreeTrial(_ context.Context, userID uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.used[userID] = false
	return nil
}

func (m *memUsers) ClaimFreeTrial(_ context.Context, userID uuid.UUID) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.used[userID] {
		return false, nil
	}
	m.used[userID] = true
	return true, nil
}

// An owner's first workspace gets the trial; every later one starts with no subscription.
func TestOneTrialPerOwner(t *testing.T) {
	ctx := context.Background()
	subs := &memSubs{byOrg: map[uuid.UUID]*models.Subscription{}}
	svc := NewService(subs, &memUsers{used: map[uuid.UUID]bool{}}, nil, nil)
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

// A trial whose subscription could not be written is handed back, so the owner's next workspace still gets it.
func TestFailedTrialIsHandedBack(t *testing.T) {
	ctx := context.Background()
	subs := &memSubs{byOrg: map[uuid.UUID]*models.Subscription{}, failing: true}
	svc := NewService(subs, &memUsers{used: map[uuid.UUID]bool{}}, nil, nil)
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
