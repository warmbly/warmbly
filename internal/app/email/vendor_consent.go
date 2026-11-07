package email

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
	"golang.org/x/oauth2"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
	"github.com/warmbly/warmbly/internal/pkg/crypt"
	"github.com/warmbly/warmbly/internal/pkg/mailhost"
)

// VendorConsentStatePrefix marks a sign-in an inbox vendor completes for an import row; no other flow mints it.
const VendorConsentStatePrefix = "vc."

// VendorConsentTTL is how long a vendor has to work through its queue and open the link.
const VendorConsentTTL = 24 * time.Hour

// IsVendorConsentState reports whether a callback state is a vendor-completed sign-in.
func IsVendorConsentState(state string) bool {
	return strings.HasPrefix(state, VendorConsentStatePrefix)
}

func vendorConsentKey(state string) string {
	return "email_vendor_consent:" + state
}

// Refusals of a vendor-completed sign-in, shown on the callback page.
var (
	ErrVendorConsentState = errx.NewWithIdentifier(errx.BadRequest, "vendor_consent_expired",
		"This sign-in link has expired or was already used. Warmbly asks the vendor again, or the mailbox can be signed in from the import.")
	ErrVendorConsentWrongMailbox = errx.NewWithIdentifier(errx.Conflict, "vendor_consent_wrong_mailbox",
		"The account that signed in is not the mailbox this link was made for, so nothing was connected.")
	ErrVendorConsentDeclined = errx.NewWithIdentifier(errx.BadRequest, "vendor_consent_declined",
		"Google did not grant access to the mailbox, so nothing was connected.")
	ErrVendorConsentNotWanted = errx.NewWithIdentifier(errx.Conflict, "vendor_consent_not_wanted",
		"The import this link was made for no longer waits on this mailbox, so nothing was connected.")
	ErrVendorConsentNoRefreshToken = errx.NewWithIdentifier(errx.BadRequest, "vendor_consent_no_refresh_token",
		"Google did not return a long-lived token for this mailbox, so its sign-in was left as it was.")
	ErrVendorReconnectNotEligible = errx.NewWithIdentifier(errx.BadRequest, "vendor_reconnect_not_eligible",
		"Only a Google mailbox signed in on its own, through an administrator's grant or with an app password can be signed in again through its inbox vendor.")
)

// VendorConsentRows is the import's side of a vendor-completed sign-in.
type VendorConsentRows interface {
	// VendorConsentWanted reports whether the import row still waits on a sign-in as email.
	VendorConsentWanted(ctx context.Context, orgID, importID uuid.UUID, line int, email string) bool
	// VendorConsentSettled records how a vendor's sign-in ended; a non-empty reason lets the row fall back to Sign in.
	VendorConsentSettled(ctx context.Context, orgID, connectionID uuid.UUID, email, reason string)
}

// VendorConsentApp is the Google client a vendor is asked to trust; ok is false when vendor sign-in cannot connect here.
func (s *emailService) VendorConsentApp() (clientID, appName string, ok bool) {
	if !config.GoogleOAuthConnect() {
		return "", "", false
	}
	cfg, xerr := s.oauthConfigFor(models.InboxProviderGoogle)
	if xerr != nil {
		return "", "", false
	}
	return cfg.ClientID, config.GoogleOAuthAppName(), true
}

// VendorConsentURL mints a single-use Google sign-in for one import row, or to
// renew or convert one existing mailbox, which only that address can finish.
func (s *emailService) VendorConsentURL(ctx context.Context, req models.VendorConsentRequest) (string, *errx.Error) {
	email := loginHintOrEmpty(req.Email)
	if email == "" || req.OrganizationID == uuid.Nil || req.UserID == uuid.Nil || req.ConnectionID == uuid.Nil ||
		(req.ImportID == uuid.Nil && req.EmailAccountID == nil) {
		return "", errx.ErrEmailOnboardState
	}
	if !config.GoogleOAuthConnect() {
		return "", errx.ErrEmailOnboardGoogleOAuthDisabled
	}
	cfg, xerr := s.oauthConfigFor(models.InboxProviderGoogle)
	if xerr != nil {
		return "", xerr
	}
	// The target is the organization's own mailbox at this address, checked before any link exists.
	if req.EmailAccountID != nil {
		if _, _, xerr := s.vendorReconnectTarget(ctx, req.OrganizationID, *req.EmailAccountID, email); xerr != nil {
			return "", xerr
		}
	}
	if s.r == nil {
		return "", errx.InternalError()
	}
	n, err := crypt.Nonce()
	if err != nil {
		errs.CaptureException(err)
		return "", errx.InternalError()
	}
	state := VendorConsentStatePrefix + n
	verifier := oauth2.GenerateVerifier()
	raw, err := json.Marshal(models.VendorConsentState{
		OrganizationID: req.OrganizationID, UserID: req.UserID, ImportID: req.ImportID, Line: req.Line,
		ConnectionID: req.ConnectionID, Email: email, CodeVerifier: verifier, ExpiresAt: time.Now().Add(VendorConsentTTL),
		EmailAccountID: req.EmailAccountID,
	})
	if err != nil {
		errs.CaptureException(err)
		return "", errx.InternalError()
	}
	if err := s.r.Set(ctx, vendorConsentKey(state), raw, VendorConsentTTL).Err(); err != nil {
		errs.CaptureException(err)
		return "", errx.InternalError()
	}
	opts := append(authCodeOptions(models.InboxProviderGoogle, email), oauth2.S256ChallengeOption(verifier))
	return cfg.AuthCodeURL(state, opts...), nil
}

// takeVendorConsent consumes a vendor-consent state; a second read of it finds nothing.
func (s *emailService) takeVendorConsent(ctx context.Context, state string) (*models.VendorConsentState, *errx.Error) {
	if !IsVendorConsentState(state) {
		return nil, ErrVendorConsentState
	}
	if s.r == nil {
		return nil, errx.InternalError()
	}
	raw, err := s.r.GetDel(ctx, vendorConsentKey(state)).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, ErrVendorConsentState
		}
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	var out models.VendorConsentState
	if err := json.Unmarshal(raw, &out); err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	if out.CodeVerifier == "" || out.Email == "" || out.OrganizationID == uuid.Nil || out.UserID == uuid.Nil || time.Now().After(out.ExpiresAt) {
		return nil, ErrVendorConsentState
	}
	return &out, nil
}

// VendorConsentResult is what a vendor-completed sign-in did, and in whose name.
type VendorConsentResult struct {
	Account *models.Email
	// ActorID is the member who started the import or the reconnect.
	ActorID uuid.UUID
	// Reconnected is an existing mailbox renewed or converted in place.
	Reconnected bool
}

// FinishVendorConsent connects or reconnects the mailbox a vendor signed in as,
// server-side, in the name of the member who asked. authorizeFor re-checks that member.
func (s *emailService) FinishVendorConsent(ctx context.Context, code, state, providerErr string, authorizeFor func(userID string) FinishAuthorizer) (*VendorConsentResult, *errx.Error) {
	ctx, cancel := detach(ctx, connectBudget)
	defer cancel()
	sess, xerr := s.takeVendorConsent(ctx, strings.TrimSpace(state))
	if xerr != nil {
		return nil, xerr
	}
	acc, xerr := s.finishVendorConsent(ctx, sess, strings.TrimSpace(code), providerErr, authorizeFor)
	reason := ""
	if xerr != nil {
		reason = xerr.Message
		if xerr.Code == errx.Internal {
			reason = "Warmbly could not finish the sign-in on its side."
		}
		log.Info().Str("organization_id", sess.OrganizationID.String()).Str("import_id", sess.ImportID.String()).
			Int("line", sess.Line).Bool("reconnect", sess.EmailAccountID != nil).Str("reason", xerr.ResponseCode()).
			Msg("vendor consent: sign-in changed nothing")
	}
	if rows, ok := s.importSignin.(VendorConsentRows); ok {
		rows.VendorConsentSettled(ctx, sess.OrganizationID, sess.ConnectionID, sess.Email, reason)
	}
	if xerr != nil {
		return nil, xerr
	}
	return &VendorConsentResult{Account: acc, ActorID: sess.UserID, Reconnected: sess.EmailAccountID != nil}, nil
}

func (s *emailService) finishVendorConsent(ctx context.Context, sess *models.VendorConsentState, code, providerErr string, authorizeFor func(userID string) FinishAuthorizer) (*models.Email, *errx.Error) {
	if providerErr != "" {
		return nil, ErrVendorConsentDeclined
	}
	if code == "" {
		return nil, errx.ErrEmailOnboardCode
	}
	if authorizeFor == nil {
		return nil, errx.ErrForbidden
	}
	orgID := sess.OrganizationID
	reconnect := sess.EmailAccountID != nil
	// The member is held to the bar the dashboard holds: membership to connect, manage-emails to renew.
	if authorize := authorizeFor(sess.UserID.String()); authorize == nil {
		return nil, errx.ErrForbidden
	} else if xerr := authorize(ctx, orgID, reconnect); xerr != nil {
		return nil, xerr
	}

	var (
		target    *models.Email
		kind      reconnectKind
		allowance *models.MailboxAllowance
		xerr      *errx.Error
	)
	if reconnect {
		if target, kind, xerr = s.vendorReconnectTarget(ctx, orgID, *sess.EmailAccountID, sess.Email); xerr != nil {
			return nil, xerr
		}
		if kind == reconnectConvert && !config.GoogleOAuthConnect() {
			return nil, errx.ErrEmailOnboardGoogleOAuthDisabled
		}
	} else {
		rows, ok := s.importSignin.(VendorConsentRows)
		if !ok || !rows.VendorConsentWanted(ctx, orgID, sess.ImportID, sess.Line, sess.Email) {
			return nil, ErrVendorConsentNotWanted
		}
		if !config.GoogleOAuthConnect() {
			return nil, errx.ErrEmailOnboardGoogleOAuthDisabled
		}
		if allowance, xerr = s.guardInboxLimit(ctx, &orgID); xerr != nil {
			return nil, xerr
		}
	}

	cfg, xerr := s.oauthConfigFor(models.InboxProviderGoogle)
	if xerr != nil {
		return nil, xerr
	}
	tok, err := cfg.Exchange(ctx, code, oauth2.VerifierOption(sess.CodeVerifier))
	if err != nil {
		return nil, errx.ErrEmailOnboardExchange
	}
	if xerr := checkGrantedScopes(ctx, models.InboxProviderGoogle, cfg.Scopes, tok); xerr != nil {
		return nil, xerr
	}
	owner, xerr := fetchInboxOwner(ctx, models.InboxProviderGoogle, tok.AccessToken)
	if xerr != nil {
		return nil, xerr
	}
	if xerr := vendorConsentOwner(sess, owner); xerr != nil {
		return nil, xerr
	}
	if !reconnect {
		if existing, xerr := s.findExisting(ctx, sess.UserID.String(), &orgID, owner.Email); xerr != nil {
			return nil, xerr
		} else if existing != nil {
			return nil, errx.ErrEmailOnboardAlreadyExists
		}
		return s.createOAuthMailbox(ctx, sess.UserID.String(), &orgID, allowance, models.InboxProviderGoogle, tok, owner)
	}
	// Nothing about the mailbox changes until every check above has passed and a long-lived token is in hand.
	if strings.TrimSpace(tok.RefreshToken) == "" {
		return nil, ErrVendorConsentNoRefreshToken
	}
	return s.vendorReconnect(ctx, sess, target, kind, tok, owner)
}

// reconnectKind is how an existing mailbox moves onto its own Google sign-in.
type reconnectKind int

const (
	// reconnectRenew is a per-mailbox Google sign-in whose tokens are replaced.
	reconnectRenew reconnectKind = iota + 1
	// reconnectConvert is a delegated or app-password Google mailbox moved onto its own sign-in.
	reconnectConvert
)

// vendorReconnectTarget is the organization's own Google mailbox at the expected address, and how a sign-in reaches it.
func (s *emailService) vendorReconnectTarget(ctx context.Context, orgID, accountID uuid.UUID, email string) (*models.Email, reconnectKind, *errx.Error) {
	acc, xerr := s.emailRepository.GetByID(ctx, accountID)
	if xerr != nil {
		return nil, 0, xerr
	}
	if acc == nil || acc.OrganizationID == nil || *acc.OrganizationID != orgID {
		return nil, 0, errx.ErrNotFound
	}
	if !strings.EqualFold(strings.TrimSpace(acc.Email), strings.TrimSpace(email)) {
		return nil, 0, ErrVendorConsentWrongMailbox
	}
	if s.cloudLink != nil {
		m, err := s.cloudLink.GetByAccount(ctx, accountID)
		if err != nil {
			// Unknown is not unmanaged: no consent is asked for a mailbox Warmbly Cloud may hold.
			return nil, 0, errx.InternalError()
		}
		if m != nil && m.Managed {
			return nil, 0, errx.ErrEmailReauthCloudManaged
		}
	}
	switch provider := models.InboxProvider(acc.Provider); {
	case provider == models.InboxProviderGoogle && acc.AuthMethod == models.MailAuthDelegated:
		return acc, reconnectConvert, nil
	case provider == models.InboxProviderGoogle:
		return acc, reconnectRenew, nil
	case provider == models.InboxProviderSMTPIMAP && mailhost.Host(acc.MailHost).Google():
		return acc, reconnectConvert, nil
	}
	return nil, 0, ErrVendorReconnectNotEligible
}

// vendorReconnect replaces an existing mailbox's sign-in in place through the reauth or convert path.
func (s *emailService) vendorReconnect(ctx context.Context, sess *models.VendorConsentState, target *models.Email, kind reconnectKind, tok *oauth2.Token, owner *inboxOwner) (*models.Email, *errx.Error) {
	orgID := sess.OrganizationID
	if kind == reconnectRenew {
		return s.finishReauth(ctx, &models.EmailOnboardingState{
			UserID: sess.UserID.String(), OrganizationID: &orgID, Provider: string(models.InboxProviderGoogle), EmailAccountID: &target.ID,
		}, models.InboxProviderGoogle, tok, owner)
	}
	ok, xerr := s.emailRepository.ConvertToGoogleOAuth(ctx, orgID, target.ID, tok.AccessToken, tok.RefreshToken, tok.Expiry, oauthMailHost(models.InboxProviderGoogle, target.Email), recordedScopes(tok))
	if xerr != nil {
		return nil, xerr
	}
	if !ok {
		return nil, ErrVendorReconnectNotEligible
	}
	after := *target
	after.Provider, after.AuthMethod = string(models.InboxProviderGoogle), models.MailAuthOAuth
	s.captureSendIdentityList(ctx, &after, tok)
	return s.reloadConverted(ctx, target)
}

// vendorConsentOwner refuses tokens for any account but the one the link was minted for.
func vendorConsentOwner(sess *models.VendorConsentState, owner *inboxOwner) *errx.Error {
	if owner == nil || !strings.EqualFold(strings.TrimSpace(owner.Email), strings.TrimSpace(sess.Email)) {
		return ErrVendorConsentWrongMailbox
	}
	return nil
}
