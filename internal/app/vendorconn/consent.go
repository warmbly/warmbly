package vendorconn

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/app/mailboximport"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/mailvendor"
)

// ConsentLinks is the mailbox service's side of a vendor-completed Google sign-in.
type ConsentLinks interface {
	VendorConsentApp() (clientID, appName string, ok bool)
	VendorConsentURL(ctx context.Context, req models.VendorConsentRequest) (string, *errx.Error)
}

const (
	// trustDoneTTL is how long a trusted client is taken as trusted before the vendor is asked again.
	trustDoneTTL = 7 * 24 * time.Hour
	// consentMaxWait bounds one vendor sign-in, well inside the link's own 24 hours.
	consentMaxWait = 6 * time.Hour
	// consentPollEvery spaces status reads: the callback, not the status, is what connects a mailbox.
	consentPollEvery = 5 * time.Minute
	consentStateTTL  = 25 * time.Hour
	consentFailedTTL = time.Hour
	// consentBudgetFailedTTL holds a failure longer for a vendor that limits requests per mailbox.
	consentBudgetFailedTTL = 24 * time.Hour
)

func trustKey(orgID, connectionID uuid.UUID, domain, clientID string) string {
	return "vendor_app_trust:" + orgID.String() + ":" + connectionID.String() + ":" + domain + ":" + clientID
}

func consentKey(orgID, connectionID uuid.UUID, email string) string {
	return "vendor_consent:" + orgID.String() + ":" + connectionID.String() + ":" + strings.ToLower(strings.TrimSpace(email))
}

func consentBudgetKey(connectionID uuid.UUID, mailboxID string) string {
	return "vendor_consent_budget:" + connectionID.String() + ":" + mailboxID
}

// consentState is one mailbox's vendor sign-in, shared by every row and replica through Redis.
type consentState struct {
	// RequestID is empty for a request the vendor may hold but did not name; it is never sent again.
	RequestID string    `json:"request_id,omitempty"`
	Started   time.Time `json:"started,omitempty"`
	Stage     string    `json:"stage,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
	Polled    time.Time `json:"polled,omitempty"`
	Completed time.Time `json:"completed,omitempty"`
	Failed    string    `json:"failed,omitempty"`
	// Done is a sign-in that connected; nothing more is sent for the mailbox until it is asked for again.
	Done bool `json:"done,omitempty"`
}

// duePoll is a named request whose status has not been read for a while.
func (st consentState) duePoll(now time.Time) bool {
	return st.RequestID != "" && now.Sub(st.Polled) >= consentPollEvery
}

// consentStep advances a pending sign-in with what the vendor reported, if anything,
// and reports whether it has ended without a connect, and why.
func consentStep(st consentState, status *mailvendor.AuthorizationStatus, now time.Time) (consentState, bool, string) {
	if status != nil {
		st.Polled = now
		switch status.State {
		case mailvendor.AuthorizationFailed:
			return st, true, status.Reason
		case mailvendor.AuthorizationCompleted:
			if st.Completed.IsZero() {
				st.Completed = now
			}
			st.Stage = mailvendor.AuthorizationCompleted
		default:
			if status.Stage != "" {
				st.Stage = status.Stage
			}
			if !status.UpdatedAt.IsZero() {
				st.UpdatedAt = status.UpdatedAt
			}
		}
	}
	switch {
	case !st.Completed.IsZero() && now.Sub(st.Completed) > authSettle:
		return st, true, "it reported the sign-in done, but Google never returned to Warmbly"
	case st.Completed.IsZero() && now.Sub(st.Started) > consentMaxWait:
		return st, true, "it did not finish within six hours"
	}
	return st, false, ""
}

// authorizeByConsent has the vendor trust this instance's Google client on the
// domain, then sign in as the row's mailbox; handled is false for a vendor that cannot.
func (s *Service) authorizeByConsent(ctx context.Context, r mailboximport.VendorAuthorizeRequest, domain, clientID, appName string) (DomainAuthorization, bool) {
	// A mailbox the vendor may not sign in as asks the vendor nothing, not even to trust the client.
	if !r.Consent || r.VendorMailboxID == "" {
		return DomainAuthorization{}, true
	}
	c, xerr := s.get(ctx, r.OrgID, r.ConnectionID)
	if xerr != nil {
		return DomainAuthorization{Pending: xerr.Code == errx.Internal}, true
	}
	client, xerr := s.client(ctx, c)
	if xerr != nil {
		if xerr.Code == errx.Internal {
			return DomainAuthorization{Pending: true}, true
		}
		return DomainAuthorization{Message: xerr.Message}, true
	}
	cr, ok := client.(mailvendor.ConsentRequester)
	if !ok {
		return DomainAuthorization{}, false
	}
	if auth, done := s.trustDomain(ctx, c, client, cr, r.OrgID, domain, clientID, appName); !done {
		auth.Domain = true
		return auth, true
	}
	return s.consentMailbox(ctx, c, cr, r, domain, clientID, appName), true
}

// trustDomain has the vendor mark the client trusted on the domain once, for every row and replica; done once it is.
func (s *Service) trustDomain(ctx context.Context, c *models.VendorConnection, client mailvendor.Client, cr mailvendor.ConsentRequester, orgID uuid.UUID, domain, clientID, appName string) (DomainAuthorization, bool) {
	label := labelOf(c.Vendor)
	key := trustKey(orgID, c.ID, domain, clientID)
	var st authState
	if err := s.cache.GetJSON(ctx, key, &st); err == nil {
		switch {
		case st.Failed != "":
			return DomainAuthorization{Message: st.Failed}, false
		case !st.Completed.IsZero():
			return DomainAuthorization{}, true
		case st.RequestID == "":
			return DomainAuthorization{Pending: true, Trusting: true, Vendor: label}, false
		}
		status, err := cr.TrustStatus(ctx, st.RequestID)
		switch {
		case err != nil && transient(err):
			status = mailvendor.AuthorizationStatus{State: mailvendor.AuthorizationPending, Stage: st.Stage}
		case errors.Is(err, mailvendor.ErrUnauthorized):
			return DomainAuthorization{Message: s.failed(ctx, c, err).Message}, false
		case err != nil:
			status = mailvendor.AuthorizationStatus{State: mailvendor.AuthorizationFailed, Reason: "the request is gone"}
		}
		if status.State == mailvendor.AuthorizationPending && time.Since(st.Started) > authMaxWait {
			status = mailvendor.AuthorizationStatus{State: mailvendor.AuthorizationFailed, Reason: "it did not finish within two hours"}
		}
		switch status.State {
		case mailvendor.AuthorizationPending:
			if status.Stage != st.Stage {
				st.Stage = status.Stage
				_ = s.cache.SetJSON(ctx, key, st, time.Until(st.Started.Add(authPendingTTL)))
			}
			return DomainAuthorization{Pending: true, Trusting: true, Vendor: label, Stage: status.Stage, Note: trustNote(label, clientID, st.RequestID, status, time.Now())}, false
		case mailvendor.AuthorizationFailed:
			msg := label + " could not add Warmbly as a trusted app on " + domain + reasonSuffix(status.Reason) + ". Sign in on each mailbox instead, or retry these rows once it is fixed."
			_ = s.cache.SetJSON(ctx, key, authState{Failed: msg}, authFailedTTL)
			return DomainAuthorization{Message: msg}, false
		}
		st.Completed = time.Now()
		_ = s.cache.SetJSON(ctx, key, st, trustDoneTTL)
		return DomainAuthorization{}, true
	}

	started, err := s.cache.SetNX(ctx, key, "{}", authStartTTL).Result()
	if err != nil {
		return DomainAuthorization{}, false
	}
	if !started {
		return DomainAuthorization{Pending: true, Trusting: true, Vendor: label}, false
	}
	list, err := s.listed(ctx, c, client)
	if err != nil {
		_ = s.cache.Del(ctx, key)
		switch {
		case transient(err):
			return DomainAuthorization{Pending: true, Trusting: true, Vendor: label}, false
		case errors.Is(err, mailvendor.ErrUnauthorized):
			return DomainAuthorization{Message: s.failed(ctx, c, err).Message}, false
		}
		return DomainAuthorization{}, false
	}
	// Any mailbox on the domain names its vendor workspace; the admin one is preferred.
	var on *mailvendor.Mailbox
	for i := range list {
		if strings.EqualFold(list[i].Domain, domain) || strings.HasSuffix(strings.ToLower(list[i].Email), "@"+domain) {
			if on == nil || (list[i].Admin && !on.Admin) {
				on = &list[i]
			}
		}
	}
	if on == nil {
		_ = s.cache.Del(ctx, key)
		return DomainAuthorization{}, false
	}
	reqID, err := cr.TrustApp(ctx, mailvendor.AppTrust{Domain: domain, Mailbox: *on, ClientID: clientID, AppName: appName})
	if err != nil {
		if transient(err) {
			_ = s.cache.Del(ctx, key)
			return DomainAuthorization{Pending: true, Trusting: true, Vendor: label}, false
		}
		if errors.Is(err, mailvendor.ErrUnauthorized) {
			_ = s.failed(ctx, c, err)
		}
		log.Info().Str("vendor", c.Vendor).Str("domain", domain).Err(err).Msg("vendor connection: trusting the Google client refused")
		msg := label + " could not add Warmbly as a trusted app on " + domain + ". It needs an admin mailbox on the domain. Sign in on each mailbox instead."
		_ = s.cache.SetJSON(ctx, key, authState{Failed: msg}, authFailedTTL)
		return DomainAuthorization{Message: msg}, false
	}
	_ = s.cache.SetJSON(ctx, key, authState{RequestID: reqID, Started: time.Now()}, authPendingTTL)
	return DomainAuthorization{Pending: true, Trusting: true, Vendor: label, Stage: "queued"}, false
}

// consentMailbox has the vendor sign in as one mailbox at a single-use link,
// never sending a second request while one may still be open.
func (s *Service) consentMailbox(ctx context.Context, c *models.VendorConnection, cr mailvendor.ConsentRequester, r mailboximport.VendorAuthorizeRequest, domain, clientID, appName string) DomainAuthorization {
	label := labelOf(c.Vendor)
	key := consentKey(r.OrgID, c.ID, r.Email)
	budget, window := cr.ConsentBudget()
	failedTTL := consentFailedTTL
	if budget > 0 {
		failedTTL = consentBudgetFailedTTL
	}
	fail := func(reason string) DomainAuthorization {
		msg := label + " could not sign in to " + r.Email + " for Warmbly" + reasonSuffix(reason) + ". Use Sign in on this row."
		_ = s.cache.SetJSON(ctx, key, consentState{Failed: msg}, failedTTL)
		return DomainAuthorization{Message: msg}
	}
	now := time.Now()

	var st consentState
	if err := s.cache.GetJSON(ctx, key, &st); err == nil {
		switch {
		case st.Done:
			return DomainAuthorization{}
		case st.Failed != "":
			return DomainAuthorization{Message: st.Failed}
		case st.Started.IsZero():
			return DomainAuthorization{Pending: true, SigningIn: true, Vendor: label}
		}
		var status *mailvendor.AuthorizationStatus
		if st.duePoll(now) {
			got, err := cr.ConsentStatus(ctx, st.RequestID)
			switch {
			case err != nil && transient(err):
			case errors.Is(err, mailvendor.ErrUnauthorized):
				return DomainAuthorization{Message: s.failed(ctx, c, err).Message}
			case err != nil:
				status = &mailvendor.AuthorizationStatus{State: mailvendor.AuthorizationFailed, Reason: "the request is gone"}
			default:
				status = &got
			}
		}
		next, ended, reason := consentStep(st, status, now)
		if ended {
			return fail(reason)
		}
		if next != st {
			// The callback may have settled it while the status was read; a settled outcome is never replaced.
			var cur consentState
			if err := s.cache.GetJSON(ctx, key, &cur); err == nil && (cur.Done || cur.Failed != "") {
				if cur.Done {
					return DomainAuthorization{}
				}
				return DomainAuthorization{Message: cur.Failed}
			}
			_ = s.cache.SetJSON(ctx, key, next, time.Until(next.Started.Add(consentStateTTL)))
		}
		note := pendingNote(label, "", clientID, nil, next.RequestID, mailvendor.AuthorizationStatus{Stage: next.Stage, UpdatedAt: next.UpdatedAt}, now)
		return DomainAuthorization{Pending: true, SigningIn: true, Vendor: label, Stage: next.Stage, Note: note}
	}

	started, err := s.cache.SetNX(ctx, key, "{}", authStartTTL).Result()
	if err != nil {
		return DomainAuthorization{Pending: true, SigningIn: true, Vendor: label}
	}
	if !started {
		return DomainAuthorization{Pending: true, SigningIn: true, Vendor: label}
	}
	budgetKey := consentBudgetKey(c.ID, r.VendorMailboxID)
	if budget > 0 {
		// Charged before the request, so an answer lost on the way back still counts against the vendor's limit.
		ok, err := s.cache.ReserveAttempt(ctx, budgetKey, int64(budget), window)
		if err != nil {
			_ = s.cache.Del(ctx, key)
			return DomainAuthorization{Pending: true, SigningIn: true, Vendor: label}
		}
		if !ok {
			msg := label + " takes " + strconv.Itoa(budget) + " sign-in requests per mailbox every " + strconv.Itoa(int(window.Hours()/24)) + " days, and " + r.Email + " has had them. Use Sign in on this row."
			_ = s.cache.SetJSON(ctx, key, consentState{Failed: msg}, window)
			return DomainAuthorization{Message: msg}
		}
	}
	link, xerr := s.consent.VendorConsentURL(ctx, models.VendorConsentRequest{
		OrganizationID: r.OrgID, UserID: r.UserID, ImportID: r.ImportID, Line: r.Line, ConnectionID: c.ID, Email: r.Email,
		EmailAccountID: r.EmailAccountID,
	})
	if xerr != nil {
		_ = s.cache.Del(ctx, key)
		if budget > 0 {
			_ = s.cache.ReleaseAttempt(ctx, budgetKey)
		}
		if xerr.Code == errx.Internal {
			return DomainAuthorization{Pending: true, SigningIn: true, Vendor: label}
		}
		return DomainAuthorization{Message: xerr.Message}
	}
	mb := mailvendor.Mailbox{ID: r.VendorMailboxID, Email: r.Email, Domain: domain, Provider: mailvendor.ProviderGoogle}
	reqID, err := cr.RequestConsent(ctx, mailvendor.ConsentRequest{Mailbox: mb, URL: link, ClientID: clientID, AppName: appName})
	switch {
	case err == nil:
		st = consentState{RequestID: reqID, Started: now, Polled: now, Stage: "queued"}
	case errors.Is(err, mailvendor.ErrInProgress), transient(err) && !errors.Is(err, mailvendor.ErrRateLimited):
		// The vendor may hold a request for this mailbox; Warmbly waits on it rather than send another.
		st = consentState{Started: now, Stage: "queued"}
	case errors.Is(err, mailvendor.ErrRateLimited):
		msg := label + " refused another sign-in request for " + r.Email + ": it limits how many one mailbox can have each week. Use Sign in on this row."
		_ = s.cache.SetJSON(ctx, key, consentState{Failed: msg}, max(window, consentBudgetFailedTTL))
		return DomainAuthorization{Message: msg}
	case errors.Is(err, mailvendor.ErrUnauthorized):
		_ = s.cache.Del(ctx, key)
		return DomainAuthorization{Message: s.failed(ctx, c, err).Message}
	default:
		log.Info().Str("vendor", c.Vendor).Str("domain", domain).Err(err).Msg("vendor connection: mailbox sign-in request refused")
		return fail("")
	}
	_ = s.cache.SetJSON(ctx, key, st, consentStateTTL)
	return DomainAuthorization{Pending: true, SigningIn: true, Vendor: label, Stage: st.Stage}
}

// ConsentSettled records how a vendor's sign-in ended: a connect marks it done, a refusal ends it so the row falls back to Sign in.
func (s *Service) ConsentSettled(ctx context.Context, orgID, connectionID uuid.UUID, email, reason string) {
	if s.cache == nil {
		return
	}
	key := consentKey(orgID, connectionID, email)
	if reason == "" {
		_ = s.cache.SetJSON(ctx, key, consentState{Done: true}, consentFailedTTL)
		return
	}
	label := "Your inbox vendor"
	if c, xerr := s.get(ctx, orgID, connectionID); xerr == nil {
		label = labelOf(c.Vendor)
	}
	msg := label + " signed in, but the mailbox did not connect" + reasonSuffix(reason) + ". Use Sign in on this row."
	_ = s.cache.SetJSON(ctx, key, consentState{Failed: msg}, consentFailedTTL)
}

// trustNote names the Google Admin step a trust request waits on, or who to ask when it has stalled.
func trustNote(label, clientID, requestID string, status mailvendor.AuthorizationStatus, now time.Time) string {
	if status.Stage == "pending" {
		return label + " is waiting for Warmbly's client ID " + clientID + " to be marked Trusted in the Google Admin console under Security > Access and data control > API controls > Manage third-party app access."
	}
	return pendingNote(label, "", clientID, nil, requestID, status, now)
}
