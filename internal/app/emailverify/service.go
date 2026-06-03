// Package emailverify (app layer) orchestrates pre-send email verification:
// it loads contacts, runs them through a pkg/emailverify.Verifier, and persists
// the result back onto the contact. This is the control-plane home for
// verification — the SMTP RCPT probe inside the Verifier dials remote MX hosts
// on :25 and must never run from a worker (a sending IP). See
// internal/pkg/emailverify for the probing details and the in-repo-vs-paid
// backend split.
package emailverify

import (
	"context"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/pkg/emailverify"
	"github.com/warmbly/warmbly/internal/repository"
)

// Service verifies contact email addresses before they are ever sent to.
type Service interface {
	// VerifyContact verifies a single stored contact by id and persists the
	// result. Returns the Result so callers (admin/on-demand) can surface it.
	VerifyContact(ctx context.Context, contactID uuid.UUID) (emailverify.Result, *errx.Error)

	// VerifyAddress verifies an arbitrary address without touching the DB. Used
	// by the on-demand handler for addresses that aren't stored contacts yet.
	VerifyAddress(ctx context.Context, email string) emailverify.Result

	// VerifyPending verifies up to `limit` not-yet-checked contacts, persisting
	// each result. Returns the number processed. Driven by the ticker scheduler.
	VerifyPending(ctx context.Context, limit int) (int, *errx.Error)
}

type service struct {
	repo     repository.ContactRepository
	verifier emailverify.Verifier
}

// NewService wires the verification service. verifier is the pluggable backend:
// the in-house emailverify.SMTPVerifier in dev/self-host, or a paid provider
// (ZeroBounce/NeverBounce/Bouncer) implementing the same interface in prod.
func NewService(repo repository.ContactRepository, verifier emailverify.Verifier) Service {
	return &service{repo: repo, verifier: verifier}
}

func (s *service) VerifyAddress(ctx context.Context, email string) emailverify.Result {
	return s.verifier.Verify(ctx, email)
}

func (s *service) VerifyContact(ctx context.Context, contactID uuid.UUID) (emailverify.Result, *errx.Error) {
	contact, xerr := s.repo.GetByID(ctx, contactID)
	if xerr != nil {
		return emailverify.Result{}, xerr
	}
	res := s.verifier.Verify(ctx, contact.Email)
	if xerr := s.repo.UpdateContactVerification(ctx, contactID, res); xerr != nil {
		return res, xerr
	}
	return res, nil
}

func (s *service) VerifyPending(ctx context.Context, limit int) (int, *errx.Error) {
	contacts, xerr := s.repo.ListUnverifiedContacts(ctx, limit)
	if xerr != nil {
		return 0, xerr
	}
	processed := 0
	for i := range contacts {
		// Honour cancellation between addresses; each probe can take seconds.
		if err := ctx.Err(); err != nil {
			break
		}
		res := s.verifier.Verify(ctx, contacts[i].Email)
		if xerr := s.repo.UpdateContactVerification(ctx, contacts[i].ID, res); xerr != nil {
			// Skip this one; a transient DB error shouldn't abort the whole tick.
			continue
		}
		processed++
	}
	return processed, nil
}
