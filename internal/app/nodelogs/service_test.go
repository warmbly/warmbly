package nodelogs

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/pkg/nodeevidence"
)

func logFixture(t *testing.T) (*Service, uuid.UUID, string) {
	t.Helper()
	url := os.Getenv("WARMBLY_TEST_REDIS")
	if url == "" {
		t.Skip("WARMBLY_TEST_REDIS not set")
	}
	c, err := cache.New(url)
	if err != nil {
		t.Fatal(err)
	}
	s, id := New(c), uuid.New()
	t.Cleanup(func() {
		keys, _ := c.Keys(context.Background(), prefix(id)+"*").Result()
		if len(keys) > 0 {
			_ = c.Del(context.Background(), keys...).Err()
		}
		_ = c.Close()
	})
	secret, err := s.Enroll(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	return s, id, secret
}

func TestNodeLogsBindCredentialAndBoundRedactedHistory(t *testing.T) {
	s, id, secret := logFixture(t)
	ctx := t.Context()
	if err := s.Authenticate(ctx, uuid.New(), secret); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("node spoofing accepted", err)
	}
	if err := s.Authenticate(ctx, id, secret); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	mailbox := uuid.New()
	b := nodeevidence.Batch{Protocol: 1, BatchID: uuid.New(), RunID: uuid.New(), StartedAt: now.Add(-time.Minute), ObservedAt: now, Dropped: 7,
		Events: []nodeevidence.Event{{ID: uuid.New(), ObservedAt: now, Name: nodeevidence.ControlPlaneHeld, Level: "secret", Category: "secret", HTTPStatus: 503, MailboxID: mailbox}}}
	if err := s.Ingest(ctx, id, secret, b); err != nil {
		t.Fatal(err)
	}
	if err := s.Ingest(ctx, id, secret, b); err != nil {
		t.Fatal("idempotent retry failed", err)
	}
	history, err := s.History(ctx, id, Filter{MailboxID: mailbox, Level: "warn", After: now.Add(-time.Second)})
	if err != nil || len(history.Events) != 1 || history.Events[0].Category != "control_plane" || history.Status.Dropped != 7 || history.Coverage != "partial" {
		t.Fatal(history, err)
	}
	if ttl := s.cache.TTL(ctx, prefix(id)+"events").Val(); ttl <= 0 || ttl > Retention {
		t.Fatal("unbounded log TTL", ttl)
	}
	for i := range MaxEvents/MaxBatch + 2 {
		b.BatchID = uuid.New()
		b.Events = nil
		for range MaxBatch {
			b.Events = append(b.Events, nodeevidence.Event{ID: uuid.New(), ObservedAt: now.Add(time.Duration(i) * time.Millisecond), Name: nodeevidence.FolderSkipped})
		}
		if err := s.Ingest(ctx, id, secret, b); err != nil {
			t.Fatal(err)
		}
	}
	if count := s.cache.ZCard(ctx, prefix(id)+"events").Val(); count != MaxEvents {
		t.Fatal("unbounded event count", count)
	}
	if h, err := s.History(ctx, id, Filter{Limit: 1}); err != nil || len(h.Events) != 1 || !h.Truncated {
		t.Fatal("unbounded read", h, err)
	}
	if err := s.Revoke(ctx, id); err != nil {
		t.Fatal(err)
	}
	b.BatchID = uuid.New()
	if err := s.Ingest(ctx, id, secret, b); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("revoked credential accepted", err)
	}
}

func TestNodeLogsUnavailableEmptyStaleAndOldEvidenceAreNotHealthy(t *testing.T) {
	s, id, secret := logFixture(t)
	ctx := t.Context()
	h, err := s.History(ctx, id, Filter{})
	if err != nil || h.Availability != "unavailable" || h.Coverage != "unavailable" {
		t.Fatal(h, err)
	}
	now := time.Now().UTC()
	b := nodeevidence.Batch{Protocol: 1, BatchID: uuid.New(), RunID: uuid.New(), StartedAt: now, ObservedAt: now, Events: []nodeevidence.Event{}}
	if err := s.Ingest(ctx, id, secret, b); err != nil {
		t.Fatal(err)
	}
	h, _ = s.History(ctx, id, Filter{})
	if h.Reason != "no_recent_evidence" || h.Coverage != "partial" {
		t.Fatal(h)
	}
	status := *h.Status
	status.ObservedAt = now.Add(-3 * time.Minute)
	if err := s.cache.SetJSON(ctx, prefix(id)+"status", status, 2*Retention); err != nil {
		t.Fatal(err)
	}
	h, _ = s.History(ctx, id, Filter{})
	if h.Availability != "stale" || h.Reason != "no_recent_capture" {
		t.Fatal(h)
	}
	b.BatchID = uuid.New()
	b.Events = []nodeevidence.Event{{ID: uuid.New(), ObservedAt: now.Add(-2 * Retention), Name: nodeevidence.WorkerError}}
	if err := s.Ingest(ctx, id, secret, b); !errors.Is(err, ErrInvalid) {
		t.Fatal("expired evidence reintroduced", err)
	}
	b.Events[0].ObservedAt = now
	b.Events[0].Name = "private body"
	if err := s.Ingest(ctx, id, secret, b); !errors.Is(err, ErrInvalid) {
		t.Fatal("arbitrary log accepted", err)
	}
}
