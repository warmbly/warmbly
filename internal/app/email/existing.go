package email

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/pubsub"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/mailhost"
)

// ImportSigninResolver closes the import rows that were waiting for someone
// to sign in as a mailbox, once that sign-in has connected it.
type ImportSigninResolver interface {
	ResolveSignin(ctx context.Context, orgID uuid.UUID, email string, accountID uuid.UUID) error
}

// WireImportSignin attaches the mailbox import's sign-in resolver.
func (s *emailService) WireImportSignin(r ImportSigninResolver) {
	s.importSignin = r
}

// findExisting is the workspace's mailbox with this address. Mailboxes are
// workspace assets, so a teammate's connect counts; the user-scoped check only
// stands in for an account that has no organization.
func (s *emailService) findExisting(ctx context.Context, userID string, orgID *uuid.UUID, email string) (*models.EmailRef, *errx.Error) {
	email = strings.TrimSpace(email)
	if orgID != nil {
		return s.emailRepository.FindInOrganization(ctx, *orgID, email)
	}
	exists, xerr := s.emailRepository.ExistsForUser(ctx, userID, email)
	if xerr != nil || !exists {
		return nil, xerr
	}
	return &models.EmailRef{}, nil
}

// resolveImportSignin is best effort: the mailbox is connected either way.
func (s *emailService) resolveImportSignin(ctx context.Context, orgID uuid.UUID, acc *models.Email) {
	if s.importSignin == nil || acc == nil {
		return
	}
	if err := s.importSignin.ResolveSignin(ctx, orgID, acc.Email, acc.ID); err != nil {
		log.Warn().Err(err).Str("email_account_id", acc.ID.String()).Msg("mailbox import: resolving sign-in rows failed")
	}
}

// oauthMailHost names who hosts an OAuth mailbox from its provider and domain.
func oauthMailHost(provider models.InboxProvider, email string) string {
	domain := ""
	if at := strings.LastIndex(email, "@"); at >= 0 {
		domain = strings.ToLower(strings.TrimSpace(email[at+1:]))
	}
	switch provider {
	case models.InboxProviderGoogle:
		return string(mailhost.Refine(mailhost.GoogleWorkspace, domain))
	case models.InboxProviderOutlook:
		return string(mailhost.Refine(mailhost.Microsoft365, domain))
	}
	return ""
}

// ConnectDelegated stores a mailbox reached through an administrator's grant
// and puts it to work like any other connect. The caller has already proved a
// token can be minted for it.
func (s *emailService) ConnectDelegated(ctx context.Context, userID string, orgID *uuid.UUID, data models.NewDelegatedAccount) (*models.Email, *errx.Error) {
	ctx, cancel := detach(ctx, connectBudget)
	defer cancel()
	if orgID == nil {
		return nil, errx.ErrNoOrganization
	}
	if strings.TrimSpace(data.Name) == "" {
		data.Name = deriveNameFromEmail(data.Email)
	}
	ref, xerr := s.emailRepository.FindInOrganization(ctx, *orgID, data.Email)
	if xerr != nil {
		return nil, xerr
	}
	if ref != nil {
		return s.adoptDelegated(ctx, *orgID, ref, data)
	}
	allowance, xerr := s.guardInboxLimit(ctx, orgID)
	if xerr != nil {
		return nil, xerr
	}
	data.OrganizationID, data.Allowance = orgID, allowance
	acc, xerr := s.emailRepository.NewDelegatedAccount(ctx, userID, data)
	if xerr != nil {
		return nil, xerr
	}
	if s.workerAssignment != nil {
		if _, err := s.workerAssignment.AssignWorkerToEmail(ctx, acc.ID, *orgID); err != nil {
			log.Warn().Err(err).Str("email_account_id", acc.ID.String()).Msg("delegated connect: placement failed; the reconciler retries")
		}
	}
	s.syncWarmupPoolMembership(ctx, acc)
	s.publishAccountEvent(ctx, pubsub.EventAccountConnected, acc)
	s.dispatchAccountConnected(ctx, orgID, acc)
	s.resolveImportSignin(ctx, *orgID, acc)
	s.loadAccountBestEffort(ctx, acc.ID)
	return acc, nil
}

// ReactivateDelegated puts a delegated mailbox back to work once its grant mints tokens again.
func (s *emailService) ReactivateDelegated(ctx context.Context, accountID uuid.UUID) (*models.Email, *errx.Error) {
	return s.reconnectAccount(ctx, accountID)
}

// adoptDelegated puts an existing mailbox under the grant: a delegated one that
// lost its grant is relinked, and a per-mailbox sign-in moves over in place.
// Neither adds a mailbox, so neither counts against the allowance.
func (s *emailService) adoptDelegated(ctx context.Context, orgID uuid.UUID, ref *models.EmailRef, data models.NewDelegatedAccount) (*models.Email, *errx.Error) {
	switch {
	case ref.AuthMethod == models.MailAuthDelegated && models.InboxProvider(ref.Provider) == data.Provider:
		if xerr := s.emailRepository.RelinkDelegated(ctx, orgID, ref.ID, data.GrantID, data.Subject); xerr != nil {
			return nil, xerr
		}
		return s.reconnectAccount(ctx, ref.ID)
	case ref.Movable(data.Provider):
		acc, xerr := s.emailRepository.GetByID(ctx, ref.ID)
		if xerr != nil {
			return nil, xerr
		}
		ok, xerr := s.emailRepository.ConvertToDelegated(ctx, orgID, ref.ID, data.Provider, data.GrantID, data.Subject, data.MailHost)
		if xerr != nil {
			return nil, xerr
		}
		if !ok {
			return nil, errx.ErrEmailOnboardAlreadyExists
		}
		return s.reloadConverted(ctx, acc)
	}
	return nil, errx.ErrEmailOnboardAlreadyExists
}

// reloadConverted swaps a mailbox's credentials on its worker: the worker keeps
// a loaded mailbox as it is, so the old one is dropped before the new one loads.
func (s *emailService) reloadConverted(ctx context.Context, before *models.Email) (*models.Email, *errx.Error) {
	if xerr := s.dropFromWorker(ctx, before.UserID, before.ID); xerr != nil {
		log.Warn().Str("email_account_id", before.ID.String()).Str("error", xerr.Message).Msg("converted mailbox: worker drop failed; it reloads on that worker's next restart")
	}
	// A mailbox someone switched off stays off; one stopped by its broken sign-in
	// is the reason to move it, so it comes back. Reactivating loads it and publishes.
	if before.Status != "active" && !s.stoppedBySignin(ctx, before.ID) {
		return s.emailRepository.GetByID(ctx, before.ID)
	}
	return s.reconnectAccount(ctx, before.ID)
}

// stoppedBySignin is a mailbox with an unresolved credential-class error.
func (s *emailService) stoppedBySignin(ctx context.Context, accountID uuid.UUID) bool {
	if s.accountErrors == nil {
		return false
	}
	open, xerr := s.accountErrors.GetByAccountID(ctx, accountID, true)
	if xerr != nil {
		return false
	}
	for _, e := range open {
		for _, c := range errx.CredentialMailErrorCodes {
			if e.ErrorCode == string(c) {
				return true
			}
		}
	}
	return false
}

// SwitchToAppPassword moves a mailbox off per-mailbox Google sign-in onto
// Gmail's IMAP and SMTP with an app password, keeping the mailbox and its history.
func (s *emailService) SwitchToAppPassword(ctx context.Context, orgID *uuid.UUID, accountID uuid.UUID, appPassword string) (*models.Email, *errx.Error) {
	ctx, cancel := detach(ctx, connectBudget)
	defer cancel()
	if orgID == nil {
		return nil, errx.ErrNoOrganization
	}
	acc, xerr := s.emailRepository.GetByID(ctx, accountID)
	if xerr != nil {
		return nil, xerr
	}
	if acc == nil || acc.OrganizationID == nil || *acc.OrganizationID != *orgID {
		return nil, errx.ErrNotFound
	}
	ref, xerr := s.emailRepository.FindInOrganization(ctx, *orgID, acc.Email)
	if xerr != nil {
		return nil, xerr
	}
	if ref == nil || ref.ID != acc.ID || !ref.SigninRetiring() {
		return nil, ErrNotGoogleSignin
	}
	password := strings.Join(strings.Fields(appPassword), "")
	if len(password) != 16 {
		return nil, ErrAppPasswordShape
	}
	creds := &models.SmtpImap{
		SMTP: &models.Service{Host: "smtp.gmail.com", Port: 587, Username: acc.Email, Password: password, Security: "starttls"},
		IMAP: &models.Service{Host: "imap.gmail.com", Port: 993, Username: acc.Email, Password: password, Security: "tls"},
	}
	if s.workerAssignment == nil {
		return nil, errx.ErrEmailOnboardNoWorker
	}
	if xerr := s.checkCredentials(ctx, *orgID, acc.WorkerID, creds); xerr != nil {
		return nil, xerr
	}
	ok, xerr := s.emailRepository.ConvertGoogleToAppPassword(ctx, *orgID, acc.ID, creds, oauthMailHost(models.InboxProviderGoogle, acc.Email))
	if xerr != nil {
		return nil, xerr
	}
	if !ok {
		return nil, ErrNotGoogleSignin
	}
	return s.reloadConverted(ctx, acc)
}

// Refusals for the app-password switch.
var (
	ErrNotGoogleSignin  = errx.NewWithIdentifier(errx.Conflict, "mailbox_not_google_signin", "Only a mailbox connected with Google sign-in can switch to an app password.")
	ErrAppPasswordShape = errx.NewWithIdentifier(errx.BadRequest, "app_password_invalid", "A Google app password is 16 letters. Create one at myaccount.google.com/apppasswords and paste it here.")
)
