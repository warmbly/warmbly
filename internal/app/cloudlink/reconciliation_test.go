package cloudlink

import (
	"context"
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

type enrollmentFaultRepo struct {
	*stubLinkRepo
	beginErr   error
	confirmErr error
}

func (r *enrollmentFaultRepo) BeginEnrollment(_ context.Context, account, remote, instance uuid.UUID) error {
	if r.beginErr != nil {
		return r.beginErr
	}
	if r.mailbox != nil && r.mailbox.EnrollmentState == "pending_remove" {
		return errors.New("removal pending")
	}
	r.mailbox = &models.CloudLinkMailbox{EmailAccountID: account, RemoteID: remote, InstanceID: instance, EnrollmentState: "pending_enroll"}
	return nil
}

func (r *enrollmentFaultRepo) Enroll(_ context.Context, _, _, _ uuid.UUID, _ bool) (*models.CloudLinkMailbox, error) {
	if r.confirmErr != nil {
		return nil, r.confirmErr
	}
	r.mailbox.EnrollmentState = "active"
	return r.mailbox, nil
}

func (r *enrollmentFaultRepo) List(context.Context) ([]models.CloudLinkMailbox, error) {
	if r.mailbox == nil {
		return nil, nil
	}
	return []models.CloudLinkMailbox{*r.mailbox}, nil
}

func (r *enrollmentFaultRepo) Unenroll(ctx context.Context, account uuid.UUID) error {
	if err := r.stubLinkRepo.Unenroll(ctx, account); err != nil {
		return err
	}
	r.mailbox = nil
	return nil
}

func (r *enrollmentFaultRepo) InvalidateStanding(context.Context, uuid.UUID) error { return nil }
func (r *enrollmentFaultRepo) SetSyncResult(context.Context, uuid.UUID, time.Time, string) error {
	return nil
}

func (r *enrollmentFaultRepo) ListLinks(context.Context) ([]models.CloudLink, error) {
	return []models.CloudLink{*r.link}, nil
}

func (r *enrollmentFaultRepo) ListForOrg(ctx context.Context, _ uuid.UUID, _ *uuid.UUID) ([]models.CloudLinkMailbox, error) {
	return r.List(ctx)
}

type enrollmentEmails struct{ stubEmails }

func (e enrollmentEmails) GetSMTPCredentials(context.Context, uuid.UUID) (*repository.SMTPCredentials, *errx.Error) {
	return &repository.SMTPCredentials{}, nil
}

func (e enrollmentEmails) GetAllActiveInScope(context.Context, repository.AccountScope) ([]models.Email, *errx.Error) {
	return []models.Email{*e.account}, nil
}

func TestEnrollmentRecordsIntentBeforeRemoteAndRetriesAmbiguousConfirmation(t *testing.T) {
	for _, failure := range []string{"lost_ack", "local_confirmation", "intent_write"} {
		t.Run(failure, func(t *testing.T) {
			org, account := uuid.New(), uuid.New()
			posts, deletes := 0, 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.Method {
				case http.MethodPost:
					posts++
					if failure == "lost_ack" && posts == 1 {
						conn, _, err := w.(http.Hijacker).Hijack()
						if err == nil {
							_ = conn.Close()
						}
						return
					}
					_, _ = w.Write([]byte(`{}`))
				case http.MethodDelete:
					deletes++
				case http.MethodGet:
					_, _ = w.Write([]byte(`[]`))
				}
			}))
			t.Cleanup(srv.Close)
			r := &enrollmentFaultRepo{stubLinkRepo: &stubLinkRepo{link: &models.CloudLink{CloudURL: srv.URL, OrganizationID: &org}}}
			if failure == "local_confirmation" {
				r.confirmErr = errors.New("write unavailable")
			}
			if failure == "intent_write" {
				r.beginErr = errors.New("write unavailable")
			}
			s := NewService(r, enrollmentEmails{stubEmails{account: &models.Email{ID: account, OrganizationID: &org, Status: "active", Provider: "smtp_imap"}}}, nil).(*service)
			if _, xerr := s.Enroll(context.Background(), org, account); xerr == nil {
				t.Fatal("ambiguous or unsaved enrollment reported success")
			}
			if failure == "intent_write" {
				if posts != 0 {
					t.Fatal("remote enrollment preceded durable intent")
				}
				return
			}
			if r.mailbox == nil || r.mailbox.EnrollmentState != "pending_enroll" || deletes != 0 {
				t.Fatalf("durable confirmation intent replaced by compensation: %+v, deletes=%d", r.mailbox, deletes)
			}
			r.confirmErr = nil
			// A new service represents a restart, without the previous service's memory.
			s = NewService(r, s.emails, nil).(*service)
			if _, xerr := s.SyncStanding(context.Background()); xerr != nil {
				t.Fatal(xerr)
			}
			if posts != 2 || r.mailbox.EnrollmentState != "active" || deletes != 0 {
				t.Fatalf("enrollment retry not idempotent: %+v, posts=%d, deletes=%d", r.mailbox, posts, deletes)
			}
		})
	}
}

func TestRemovalRetainsOptOutAfterLostAcknowledgmentAndCleanupFailure(t *testing.T) {
	f := newRevokeFixture(t, http.StatusNoContent)
	r := &enrollmentFaultRepo{stubLinkRepo: f.repo}
	s := NewService(r, f.svc.emails, nil).(*service)
	deletes := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodDelete {
			deletes++
			if deletes == 1 {
				conn, _, err := w.(http.Hijacker).Hijack()
				if err == nil {
					_ = conn.Close()
				}
				return
			}
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"code":"pool_link_mailbox_not_found"}`))
		}
	}))
	t.Cleanup(srv.Close)
	r.link.CloudURL = srv.URL
	if xerr := s.Unenroll(context.Background(), f.org, f.account); xerr == nil {
		t.Fatal("lost acknowledgment reported completed removal")
	}
	if r.mailbox.EnrollmentState != "pending_remove" {
		t.Fatal("lost acknowledgment discarded opt-out")
	}
	r.unenrollErr = errors.New("local cleanup unavailable")
	if xerr := s.reconcileEnrollments(context.Background(), r.link, []models.CloudLinkMailbox{*r.mailbox}); xerr == nil {
		t.Fatal("failed local cleanup reported completed removal")
	}
	if r.mailbox.EnrollmentState != "pending_remove" {
		t.Fatal("failed cleanup discarded opt-out")
	}
	r.unenrollErr = nil
	if xerr := s.reconcileEnrollments(context.Background(), r.link, []models.CloudLinkMailbox{*r.mailbox}); xerr != nil {
		t.Fatal(xerr)
	}
	if r.mailbox != nil || deletes != 3 {
		t.Fatalf("removal was not retried: %+v, deletes=%d", r.mailbox, deletes)
	}
}
