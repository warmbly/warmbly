package cloudlink

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// standingRepo records the standings written, answering with the prior one.
type standingRepo struct {
	repository.CloudLinkRepository

	link     *models.CloudLink
	enrolled []models.CloudLinkMailbox
	current  map[uuid.UUID]models.WarmupHealthState
}

func (r *standingRepo) GetByInstance(context.Context, uuid.UUID) (*models.CloudLink, error) {
	return r.link, nil
}

func (r *standingRepo) ListLinks(context.Context) ([]models.CloudLink, error) {
	return []models.CloudLink{*r.link}, nil
}

func (r *standingRepo) WithReconciliationLock(_ context.Context, fn func() error) error { return fn() }
func (r *standingRepo) InvalidateStanding(context.Context, uuid.UUID) error             { return nil }

func (r *standingRepo) List(context.Context) ([]models.CloudLinkMailbox, error) {
	return r.enrolled, nil
}

func (r *standingRepo) SetStanding(_ context.Context, id uuid.UUID, h *models.WarmupHealthInfo, _ bool) (models.WarmupHealthState, error) {
	prev := r.current[id]
	r.current[id] = models.WarmupHealthState(h.State)
	return prev, nil
}

func newStandingFixture(t *testing.T, handler http.HandlerFunc) (*service, *standingRepo, uuid.UUID) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	account := uuid.New()
	repo := &standingRepo{
		link:     &models.CloudLink{CloudURL: srv.URL, Token: "t"},
		enrolled: []models.CloudLinkMailbox{{EmailAccountID: account, RemoteID: account}},
		current:  map[uuid.UUID]models.WarmupHealthState{},
	}
	return &service{repo: repo}, repo, account
}

func TestSyncStandingReportsOnlyTransitions(t *testing.T) {
	state := "healthy"
	var remote uuid.UUID
	svc, repo, account := newStandingFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/pool-link/instance/standing" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode([]models.PoolLinkMailboxStanding{{RemoteID: remote, Health: &models.WarmupHealthInfo{State: state, Reason: "complaints"}}})
	})
	remote = account
	ctx := context.Background()

	// The first reading is a baseline, not a transition.
	if changes, xerr := svc.SyncStanding(ctx); xerr != nil || len(changes) != 0 {
		t.Fatalf("first sync = %+v, %v", changes, xerr)
	}
	state = "quarantined"
	changes, xerr := svc.SyncStanding(ctx)
	if xerr != nil || len(changes) != 1 {
		t.Fatalf("second sync = %+v, %v", changes, xerr)
	}
	if c := changes[0]; c.EmailAccountID != account || c.Previous != models.WarmupHealthHealthy || c.Current != models.WarmupHealthQuarantined {
		t.Fatalf("transition = %+v", c)
	}
	if changes, _ := svc.SyncStanding(ctx); len(changes) != 0 {
		t.Fatalf("an unchanged standing reported %+v", changes)
	}
	if repo.current[account] != models.WarmupHealthQuarantined {
		t.Fatalf("recorded %q", repo.current[account])
	}

	// An unknown state from a newer cloud is not recorded.
	state = "exiled"
	if changes, _ := svc.SyncStanding(ctx); len(changes) != 0 || repo.current[account] != models.WarmupHealthQuarantined {
		t.Fatalf("unknown state changed the record: %+v, %q", changes, repo.current[account])
	}
}

func TestSyncStandingFallsBackOnAnOlderCloud(t *testing.T) {
	var remote uuid.UUID
	svc, repo, account := newStandingFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/pool-link/instance/mailboxes" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"code":"not_found","message":"no route"}`))
			return
		}
		_ = json.NewEncoder(w).Encode([]models.PoolLinkMailboxState{{RemoteID: remote, Health: &models.WarmupHealthInfo{State: "blocked"}}})
	})
	remote = account
	if _, xerr := svc.SyncStanding(context.Background()); xerr != nil {
		t.Fatalf("SyncStanding: %v", xerr)
	}
	if repo.current[account] != models.WarmupHealthBlocked {
		t.Fatalf("recorded %q, want blocked", repo.current[account])
	}
}

func TestSyncStandingAnnouncesAHoldItStartsEnforcing(t *testing.T) {
	var remote uuid.UUID
	svc, repo, account := newStandingFixture(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]models.PoolLinkMailboxStanding{{RemoteID: remote, Health: &models.WarmupHealthInfo{State: "quarantined"}}})
	})
	remote = account
	changes, xerr := svc.SyncStanding(context.Background())
	if xerr != nil || len(changes) != 1 || changes[0].Previous != models.WarmupHealthHealthy || changes[0].Current != models.WarmupHealthQuarantined {
		t.Fatalf("first reading = %+v, %v", changes, xerr)
	}

	// Equal readings renew freshness without repeating a transition.
	repo.enrolled[0].Standing = &models.WarmupHealthInfo{State: "quarantined"}
	if changes, _ := svc.SyncStanding(context.Background()); len(changes) != 0 || repo.current[account] != models.WarmupHealthQuarantined {
		t.Fatalf("an unchanged standing repeated a transition: %+v", changes)
	}
}
