package cloudlink

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type listedStandingRepo struct {
	*workspaceLinkRepo
	locked   bool
	writeErr error
	writes   int
}

type listedStandingEmails struct {
	repository.EmailRepository
	accounts []models.Email
}

func (e listedStandingEmails) GetAllActiveInScope(context.Context, repository.AccountScope) ([]models.Email, *errx.Error) {
	return e.accounts, nil
}

func TestMailboxListingCannotRefreshStandingFromAnotherLink(t *testing.T) {
	org, first, second := uuid.New(), uuid.New(), uuid.New()
	firstLink, secondLink := uuid.New(), uuid.New()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		h := &models.WarmupHealthInfo{State: "healthy"}
		if req.Header.Get("Authorization") == "Bearer other" {
			h = &models.WarmupHealthInfo{State: "blocked", Reason: "unrelated_mailbox"}
		}
		_ = json.NewEncoder(w).Encode([]models.PoolLinkMailboxState{{RemoteID: first, Health: h}})
	}))
	defer srv.Close()
	r := &listedStandingRepo{workspaceLinkRepo: &workspaceLinkRepo{
		links: map[uuid.UUID]*models.CloudLink{
			firstLink:  {InstanceID: firstLink, CloudURL: srv.URL, Token: "owning"},
			secondLink: {InstanceID: secondLink, CloudURL: srv.URL, Token: "other"},
		},
		mailboxes: map[uuid.UUID]*models.CloudLinkMailbox{
			first:  {EmailAccountID: first, RemoteID: first, InstanceID: firstLink, EnrollmentState: "active"},
			second: {EmailAccountID: second, RemoteID: second, InstanceID: secondLink, EnrollmentState: "active"},
		},
	}}
	s := &service{repo: r, emails: listedStandingEmails{accounts: []models.Email{{ID: first, OrganizationID: &org}, {ID: second, OrganizationID: &org}}}}
	rows, xerr := s.ListMailboxes(context.Background(), org)
	if xerr != nil || len(rows) != 2 || rows[0].Cloud.Health.State != "healthy" || rows[1].Cloud != nil || r.writes != 1 {
		t.Fatalf("standing crossed link ownership: %+v, %v", rows, xerr)
	}
}

func TestStandingSyncReturnsTransitionsWithoutTheMailboxListCallback(t *testing.T) {
	account, instance := uuid.New(), uuid.New()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		_ = json.NewEncoder(w).Encode([]models.PoolLinkMailboxStanding{{RemoteID: account, Health: &models.WarmupHealthInfo{State: "quarantined", Reason: "complaints"}}})
	}))
	defer srv.Close()
	l := &models.CloudLink{InstanceID: instance, CloudURL: srv.URL, Token: "owning"}
	m := &models.CloudLinkMailbox{EmailAccountID: account, RemoteID: account, InstanceID: instance, EnrollmentState: "active", Standing: &models.WarmupHealthInfo{State: "healthy"}}
	r := &listedStandingRepo{workspaceLinkRepo: &workspaceLinkRepo{links: map[uuid.UUID]*models.CloudLink{instance: l}, mailboxes: map[uuid.UUID]*models.CloudLinkMailbox{account: m}}}
	s := &service{repo: r}
	var callbacks int
	s.OnStandingChange(func(context.Context, models.CloudLinkStandingChange) { callbacks++ })
	changes, xerr := s.SyncStanding(context.Background())
	if xerr != nil || len(changes) != 1 || callbacks != 0 || changes[0].Current != models.WarmupHealthQuarantined {
		t.Fatalf("sync must leave publication to its caller: %+v, callbacks=%d, %v", changes, callbacks, xerr)
	}
	changes, xerr = s.SyncStanding(context.Background())
	if xerr != nil || len(changes) != 0 || callbacks != 0 {
		t.Fatal("unchanged sync repeated transition")
	}
}

func (r *listedStandingRepo) WithReconciliationLock(_ context.Context, fn func() error) error {
	r.locked = true
	defer func() { r.locked = false }()
	return fn()
}

func (r *listedStandingRepo) SetStanding(_ context.Context, id uuid.UUID, h *models.WarmupHealthInfo, _ bool) (models.WarmupHealthState, error) {
	if !r.locked {
		return "", errors.New("observation not serialized with enrollment")
	}
	if r.writeErr != nil {
		return "", r.writeErr
	}
	m := r.mailboxes[id]
	var prev models.WarmupHealthState
	if m.Standing != nil {
		prev = models.WarmupHealthState(m.Standing.State)
	}
	now := time.Now()
	m.Standing, m.StandingObservedAt = h, &now
	r.writes++
	return prev, nil
}

func (r *listedStandingRepo) InvalidateStanding(_ context.Context, id uuid.UUID) error {
	r.mailboxes[id].StandingObservedAt = nil
	return nil
}

func TestMailboxListRefreshesOnlyValidatedStandingOnItsOwningLink(t *testing.T) {
	for _, scenario := range []string{"stale", "upgrade", "block", "unknown", "absent", "unreachable", "write_failure", "wrong_remote", "disconnect", "pending_remove"} {
		t.Run(scenario, func(t *testing.T) {
			org, account, instance := uuid.New(), uuid.New(), uuid.New()
			old := time.Now().Add(-time.Hour)
			m := &models.CloudLinkMailbox{EmailAccountID: account, RemoteID: account, InstanceID: instance, EnrollmentState: "active", Standing: &models.WarmupHealthInfo{State: "healthy"}, StandingObservedAt: &old}
			if scenario == "upgrade" {
				m.StandingObservedAt = nil
			}
			if scenario == "absent" {
				m.Standing = &models.WarmupHealthInfo{State: "blocked", Reason: "complaints"}
			}
			if scenario == "pending_remove" {
				m.EnrollmentState = "pending_remove"
			}
			state := &models.WarmupHealthInfo{State: "healthy"}
			if scenario == "block" {
				state = &models.WarmupHealthInfo{State: "quarantined", Reason: "complaints"}
			}
			if scenario == "unknown" {
				state.State = "future_state"
			}
			if scenario == "absent" {
				state = nil
			}
			remote := account
			if scenario == "wrong_remote" {
				remote = uuid.New()
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if req.Header.Get("Authorization") != "Bearer owning-link" {
					t.Error("wrong source link")
				}
				if scenario == "unreachable" {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				_ = json.NewEncoder(w).Encode([]models.PoolLinkMailboxState{{RemoteID: remote, Health: state}})
			}))
			t.Cleanup(srv.Close)
			r := &listedStandingRepo{workspaceLinkRepo: &workspaceLinkRepo{links: map[uuid.UUID]*models.CloudLink{instance: {InstanceID: instance, CloudURL: srv.URL, Token: "owning-link", DisconnectPending: scenario == "disconnect"}}, mailboxes: map[uuid.UUID]*models.CloudLinkMailbox{account: m}}}
			if scenario == "write_failure" {
				r.writeErr = errors.New("database unavailable")
			}
			s := &service{repo: r, emails: enrollmentEmails{stubEmails{account: &models.Email{ID: account, OrganizationID: &org, Status: "active"}}}}
			var changes []models.CloudLinkStandingChange
			s.OnStandingChange(func(_ context.Context, c models.CloudLinkStandingChange) { changes = append(changes, c) })
			rows, xerr := s.ListMailboxes(context.Background(), org)
			if scenario == "write_failure" {
				if xerr == nil || len(changes) != 0 || m.StandingObservedAt != &old {
					t.Fatalf("failed persistence was accepted: %+v, %v", rows, xerr)
				}
				return
			}
			if xerr != nil || len(rows) != 1 {
				t.Fatalf("list: %+v, %v", rows, xerr)
			}
			row := rows[0]
			switch scenario {
			case "stale", "upgrade":
				if row.Cloud == nil || row.Cloud.Health.State != "healthy" || row.StandingObservedAt == nil || !row.StandingObservedAt.After(old) || r.writes != 1 {
					t.Fatalf("fresh recognized status lost: %+v", row)
				}
			case "block":
				if row.Cloud.Health.State != "quarantined" || len(changes) != 1 || changes[0].Current != models.WarmupHealthQuarantined {
					t.Fatalf("real hold or transition lost: %+v, %+v", row, changes)
				}
				if _, xerr := s.ListMailboxes(context.Background(), org); xerr != nil || len(changes) != 1 {
					t.Fatal("unchanged observation repeated transition")
				}
			case "unknown", "pending_remove":
				if row.Cloud.Health.State != "blocked" || row.Cloud.Health.Reason != "cloud_evidence_unavailable" {
					t.Fatalf("invalid admission became healthy: %+v", row.Cloud.Health)
				}
			case "absent":
				if row.Cloud.Health.State != "blocked" || row.Cloud.Health.Reason != "complaints" || r.writes != 0 {
					t.Fatal("absent status lifted actual block")
				}
			case "unreachable", "wrong_remote", "disconnect":
				if row.Cloud != nil || r.writes != 0 {
					t.Fatalf("unvalidated observation admitted: %+v", row)
				}
			}
		})
	}
}
