package store

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/graphkit"
)

// memMailboxes records the statuses the store sets on Warmbly mailboxes.
type memMailboxes struct {
	mu       sync.Mutex
	status   map[uuid.UUID]string
	tracking map[uuid.UUID]string
	// lifecycle is each warmup action asked for; signatures each signature written, by Warmbly mailbox.
	lifecycle  []string
	signatures map[uuid.UUID][]string
}

func (m *memMailboxes) UpdateTrackingDomain(_ context.Context, _, id, domain string) (*models.TrackingDomainStatus, *errx.Error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.tracking == nil {
		m.tracking = map[uuid.UUID]string{}
	}
	m.tracking[uuid.MustParse(id)] = domain
	return &models.TrackingDomainStatus{TrackingDomain: domain}, nil
}

func (m *memMailboxes) OnboardSMTPIMAP(context.Context, string, *uuid.UUID, *models.NewSMTPIMAPAccount) (*models.Email, *errx.Error) {
	return &models.Email{ID: uuid.New()}, nil
}

func (m *memMailboxes) SetWarmupLifecycle(_ context.Context, _, _, action string) (*models.Email, *errx.Error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lifecycle = append(m.lifecycle, action)
	return &models.Email{}, nil
}

func (m *memMailboxes) Update(_ context.Context, _, _, id string, u *models.UpdateEmail) (*models.Email, *errx.Error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if u.Status != nil {
		m.status[uuid.MustParse(id)] = *u.Status
	}
	if u.SignatureHTML != nil {
		if m.signatures == nil {
			m.signatures = map[uuid.UUID][]string{}
		}
		key := uuid.MustParse(id)
		m.signatures[key] = append(m.signatures[key], *u.SignaturePlain)
	}
	return &models.Email{}, nil
}

func (m *memMailboxes) get(id uuid.UUID) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status[id]
}

// boughtMailbox buys and connects one mailbox and returns its store row.
func boughtMailbox(t *testing.T, h *harness) (*memMailboxes, *models.StoreMailbox) {
	t.Helper()
	mm := &memMailboxes{status: map[uuid.UUID]string{}}
	h.s.Mailboxes = mm
	h.fund(50)
	h.buy(t, domainAndMailboxes("acmeoutreach.com", graphkit.ProviderGoogle, 1))
	h.settle(t)
	for _, m := range h.repo.mailboxes {
		if m.EmailAccountID == nil {
			t.Fatalf("mailbox not connected: %+v", m)
		}
		return mm, cp(m)
	}
	t.Fatal("no mailbox bought")
	return nil, nil
}

func (h *harness) count(title string) int {
	h.notes.mu.Lock()
	defer h.notes.mu.Unlock()
	n := 0
	for _, s := range h.notes.titles {
		if strings.Contains(s, title) {
			n++
		}
	}
	return n
}

func TestCancelledMailboxIsSwitchedOffAndNeverReconnected(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	mm, m := boughtMailbox(t, h)
	account := *m.EmailAccountID
	if _, err := h.s.CancelMailbox(ctx, h.org, m.ID); err != nil {
		t.Fatal(err)
	}
	// Cancelling is still a mailbox the workspace pays for: it stays locked and on.
	if locked, _ := h.s.MailboxLocked(ctx, h.org, account); !locked || mm.get(account) != "" {
		t.Fatalf("cancelling: locked=%v status=%q, want locked and untouched", locked, mm.get(account))
	}
	h.clock.advance(40 * 24 * time.Hour)
	for range 2 {
		if err := h.s.Reconcile(ctx); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := h.s.GetMailbox(ctx, h.org, m.ID)
	if got.Status != graphkit.MailboxCancelled || got.CancelledAt == nil {
		t.Fatalf("mailbox = %+v, want cancelled with its date", got)
	}
	if mm.get(account) != "inactive" {
		t.Fatalf("Warmbly mailbox status = %q, want switched off", mm.get(account))
	}
	if n := h.count(m.Email + " was cancelled and switched off"); n != 1 {
		t.Fatalf("switch-off notices = %d, want 1", n)
	}
	if locked, _ := h.s.MailboxLocked(ctx, h.org, account); locked {
		t.Fatal("a cancelled mailbox is still locked; its history could not be deleted")
	}
	// The customer deletes its history; the reconnect loop must not bring it back.
	calls := len(h.delegator.calls)
	h.repo.detach(account)
	// Even a connect someone reset to waiting stays left alone.
	h.repo.mu.Lock()
	h.repo.mailboxes[m.ID].ConnectState = models.StoreConnectWaiting
	h.repo.mu.Unlock()
	h.clock.advance(time.Hour)
	if err := h.s.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	h.s.connects.Wait()
	after, _ := h.repo.GetMailbox(ctx, h.org, m.ID)
	if len(h.delegator.calls) != calls || after.EmailAccountID != nil {
		t.Fatalf("connect calls %d -> %d, account %v: a cancelled mailbox was reconnected", calls, len(h.delegator.calls), after.EmailAccountID)
	}
	if n := h.count("was cancelled and switched off"); n != 1 {
		t.Fatalf("switch-off notices = %d after deletion, want 1", n)
	}
}

func TestEndedMailboxIsSwitchedOffByTheSweepAfterAFailure(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	mm, m := boughtMailbox(t, h)
	account := *m.EmailAccountID
	// The switch-off path is down when the cancellation lands.
	h.s.Mailboxes = nil
	if _, err := h.s.CancelMailbox(ctx, h.org, m.ID); err != nil {
		t.Fatal(err)
	}
	h.clock.advance(40 * 24 * time.Hour)
	if err := h.s.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if row, _ := h.repo.GetMailbox(ctx, h.org, m.ID); row.Status != graphkit.MailboxCancelled || row.CancelledAt != nil {
		t.Fatalf("row = %+v, want cancelled and not yet marked", row)
	}
	h.s.Mailboxes = mm
	if err := h.s.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if mm.get(account) != "inactive" || h.count("was cancelled and switched off") != 1 {
		t.Fatalf("status %q notices %d, want the sweep to switch it off once", mm.get(account), h.count("was cancelled and switched off"))
	}
}

func TestResumeBeforeTheEndKeepsTheMailboxOn(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	mm, m := boughtMailbox(t, h)
	account := *m.EmailAccountID
	if _, err := h.s.CancelMailbox(ctx, h.org, m.ID); err != nil {
		t.Fatal(err)
	}
	h.clock.advance(24 * time.Hour)
	if _, err := h.s.ResumeMailbox(ctx, h.org, m.ID); err != nil {
		t.Fatal(err)
	}
	h.clock.advance(40 * 24 * time.Hour)
	if err := h.s.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	got, _ := h.s.GetMailbox(ctx, h.org, m.ID)
	if got.Status != graphkit.MailboxActive || got.CancelsAt != nil || got.CancelledAt != nil || got.EmailAccountID == nil || *got.EmailAccountID != account {
		t.Fatalf("mailbox = %+v, want active and connected", got)
	}
	if mm.get(account) != "" || h.count("switched off") != 0 {
		t.Fatalf("status %q, notices %v: a resumed mailbox was switched off", mm.get(account), h.notes.titles)
	}
	if locked, _ := h.s.MailboxLocked(ctx, h.org, account); !locked {
		t.Fatal("a resumed mailbox is not locked")
	}
}

func TestMailboxLockIsScopedToTheWorkspace(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	_, m := boughtMailbox(t, h)
	if locked, _ := h.s.MailboxLocked(ctx, uuid.New(), *m.EmailAccountID); locked {
		t.Fatal("another workspace read this workspace's purchase")
	}
	var none *Service
	if locked, err := none.MailboxLocked(ctx, h.org, *m.EmailAccountID); locked || err != nil {
		t.Fatal("an instance without the store locked a mailbox")
	}
}
