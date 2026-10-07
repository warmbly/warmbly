package cloudlink

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// disconnectRepo adds the calls Disconnect makes on top of the revocation stub.
type disconnectRepo struct {
	*stubLinkRepo

	rows        []models.CloudLinkMailbox
	unenrollAll int
	deleted     int
}

func (r *disconnectRepo) List(context.Context) ([]models.CloudLinkMailbox, error) {
	return r.rows, nil
}

func (r *disconnectRepo) UnenrollAll(context.Context, uuid.UUID) error {
	r.unenrollAll++
	return nil
}

func (r *disconnectRepo) Delete(context.Context, uuid.UUID) error {
	r.deleted++
	return nil
}

// revokingEmails deletes a mailbox the way the email service does: the cloud
// link has to be released first, and a refusal keeps the mailbox.
type revokingEmails struct {
	stubEmailDeletes

	svc     *service
	org     uuid.UUID
	refused *[]*errx.Error
}

func (s revokingEmails) Delete(ctx context.Context, userID, accountID string) *errx.Error {
	if xerr := s.svc.RevokeForDelete(ctx, s.org, uuid.MustParse(accountID)); xerr != nil {
		*s.refused = append(*s.refused, xerr)
		return xerr
	}
	return s.stubEmailDeletes.Delete(ctx, userID, accountID)
}

// Disconnect revokes the instance before it deletes the managed mirrors, so the
// cloud answers each mirror's release with pool_link_revoked. That has to count
// as released, or every mirror outlives the link as a mailbox that syncs nothing.
func TestDisconnectDeletesManagedMirrorsAfterTheInstanceIsRevoked(t *testing.T) {
	f := newRevokeFixture(t, http.StatusNoContent)
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		if r.URL.Path == "/v1/pool-link/instance" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":"pool_link_revoked","message":"This instance's link has been revoked."}`))
	}))
	t.Cleanup(srv.Close)
	f.repo.link.CloudURL = srv.URL
	f.repo.mailbox.Managed = true

	repo := &disconnectRepo{stubLinkRepo: f.repo, rows: []models.CloudLinkMailbox{*f.repo.mailbox}}
	f.svc.repo = repo
	calls, refused := &[]string{}, &[]*errx.Error{}
	f.svc.emailSvc = revokingEmails{stubEmailDeletes: stubEmailDeletes{deletes: calls}, svc: f.svc, org: f.org, refused: refused}

	if xerr := f.svc.Disconnect(context.Background(), f.org, true); xerr != nil {
		t.Fatalf("Disconnect: %v", xerr)
	}
	if len(*refused) != 0 {
		t.Fatalf("the mirror delete was refused after the instance was revoked: %v", *refused)
	}
	if len(*calls) != 1 {
		t.Fatalf("mirrors deleted = %d, want 1", len(*calls))
	}
	if len(paths) != 2 || paths[0] != "DELETE /v1/pool-link/instance" {
		t.Fatalf("cloud calls = %v, want the instance revoke then the mirror release", paths)
	}
	if repo.unenrollAll != 1 || repo.deleted != 1 {
		t.Errorf("local link state: unenrollAll=%d deleted=%d, want 1 and 1", repo.unenrollAll, repo.deleted)
	}
}

// The cloud releases every mailbox before it revokes an instance, so a revoked
// link has nothing left to hold and the local enrollment can go.
func TestRevokeForDeleteTreatsARevokedLinkAsReleased(t *testing.T) {
	f := newRevokeFixture(t, http.StatusNoContent)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":"pool_link_revoked","message":"revoked"}`))
	}))
	t.Cleanup(srv.Close)
	f.repo.link.CloudURL = srv.URL

	if xerr := f.svc.RevokeForDelete(context.Background(), f.org, f.account); xerr != nil {
		t.Fatalf("a revoked link was refused as if the cloud still held the mailbox: %v", xerr)
	}
	if len(f.repo.unenrolled) != 1 {
		t.Errorf("local rows dropped = %v, want the revoked enrollment", f.repo.unenrolled)
	}
}
