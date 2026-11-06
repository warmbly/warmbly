package vendorconn

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/jobrun"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/mailhost"
	"github.com/warmbly/warmbly/internal/pkg/mailvendor"
	"github.com/warmbly/warmbly/internal/repository"
)

// Auditor puts a reconnect's progress on the audit spine, which refreshes every teammate's dashboard.
type Auditor interface {
	LogAction(ctx context.Context, orgID, actorID uuid.UUID, action models.AuditAction, entityType models.AuditEntityType, entityID *uuid.UUID, ipAddress, userAgent string, changes, metadata map[string]string)
}

// WireSigninSwitch adds the audit spine and the instance setting that turns the automatic switch on.
func (s *Service) WireSigninSwitch(a Auditor, automatic func(ctx context.Context) bool) {
	s.audit, s.automatic = a, automatic
}

const (
	autoSwitchEvery = 15 * time.Minute
	// autoSwitchBatch bounds one run across the instance, so a large fleet switches over several.
	autoSwitchBatch = 50
)

// startAutoSwitch has each inbox vendor that can sign in for Warmbly reconnect its linked Google mailboxes that
// lack full mail access, with no click. One run at a time across replicas; a claim per mailbox stops a second start.
func (s *Service) startAutoSwitch(ctx context.Context) {
	jobrun.Loop(ctx, "mailbox_vendor_signin_switch", autoSwitchEvery, false, s.runAutoSwitch)
}

func (s *Service) runAutoSwitch(ctx context.Context) error {
	if s.automatic == nil || !s.automatic(ctx) || s.cache == nil || s.consent == nil {
		return nil
	}
	if _, _, ok := s.consent.VendorConsentApp(); !ok {
		return nil
	}
	cands, err := s.repo.AutoSwitchCandidates(ctx, mailvendor.ConsentVendors(), autoSwitchBatch)
	if err != nil {
		return err
	}
	for _, g := range groupSwitchCandidates(cands) {
		s.autoSwitchGroup(ctx, g)
	}
	return nil
}

// switchGroup is one vendor connection's candidates.
type switchGroup struct {
	org, conn, owner uuid.UUID
	ids              []uuid.UUID
}

// groupSwitchCandidates gathers candidates by connection, in the order they were found.
func groupSwitchCandidates(cands []repository.SigninSwitchCandidate) []switchGroup {
	var out []switchGroup
	at := map[uuid.UUID]int{}
	for _, c := range cands {
		i, ok := at[c.ConnectionID]
		if !ok {
			i = len(out)
			at[c.ConnectionID] = i
			out = append(out, switchGroup{org: c.OrgID, conn: c.ConnectionID, owner: c.OwnerID})
		}
		out[i].ids = append(out[i].ids, c.AccountID)
	}
	return out
}

// autoSwitchGroup claims the group's mailboxes and starts the vendor's sign-in for them; a mailbox that cannot start is failed, not left queued.
func (s *Service) autoSwitchGroup(ctx context.Context, g switchGroup) {
	claimed, err := s.repo.StartSigninSwitch(ctx, g.org, g.ids, true)
	if err != nil || len(claimed) == 0 {
		return
	}
	only := make(map[uuid.UUID]bool, len(claimed))
	for _, id := range claimed {
		only[id] = true
	}
	out, xerr := s.startReconnect(ctx, g.org, g.owner, g.conn, only, true)
	reason := "The vendor account no longer lists this mailbox."
	if xerr != nil {
		reason = xerr.Message
		if xerr.Code == errx.Internal {
			reason = "Warmbly could not start the reconnect. It tries again in a week."
		}
	}
	for _, m := range out {
		delete(only, m.EmailAccountID)
	}
	for id := range only {
		s.recordOutcome(ctx, g.org, uuid.Nil, g.conn, ReconnectMailbox{EmailAccountID: id, Status: ReconnectFailed, Message: reason}, true)
	}
	if len(out) > 0 {
		s.auditVendor(ctx, g.org, g.conn, map[string]string{"action": "reconnect", "automatic": "true", "mailboxes": strconv.Itoa(len(out))})
	}
}

// recordReconnect writes each mailbox whose standing moved during a pass, and tells the dashboard.
func (s *Service) recordReconnect(ctx context.Context, job *reconnectJob, before map[uuid.UUID]ReconnectMailbox) {
	moved := 0
	for _, m := range job.Mailboxes {
		if was, ok := before[m.EmailAccountID]; ok && was.Status == m.Status && was.Message == m.Message {
			continue
		}
		moved++
		automatic := job.Automatic[m.EmailAccountID]
		if m.final() {
			actor := job.UserID
			if automatic {
				actor = uuid.Nil
			}
			s.recordOutcome(ctx, job.OrgID, actor, job.ConnectionID, m, automatic)
			continue
		}
		if err := s.repo.SetSigninSwitch(ctx, job.OrgID, m.EmailAccountID, m.Status, m.Message); err != nil {
			log.Warn().Err(err).Str("email_account_id", m.EmailAccountID.String()).Msg("vendor reconnect: recording progress failed")
		}
	}
	// One event for the connection per pass refreshes its row; a finished mailbox also refreshes its own drawer.
	if moved > 0 {
		s.auditVendor(ctx, job.OrgID, job.ConnectionID, map[string]string{"action": "reconnect_progress", "mailboxes": strconv.Itoa(moved)})
	}
}

// recordOutcome stores how one mailbox's reconnect ended and audits it against the mailbox.
func (s *Service) recordOutcome(ctx context.Context, orgID, actor, connectionID uuid.UUID, m ReconnectMailbox, automatic bool) {
	if err := s.repo.SetSigninSwitch(ctx, orgID, m.EmailAccountID, m.Status, m.Message); err != nil {
		log.Warn().Err(err).Str("email_account_id", m.EmailAccountID.String()).Msg("vendor reconnect: recording the outcome failed")
	}
	if s.audit == nil {
		return
	}
	id := m.EmailAccountID
	s.audit.LogAction(ctx, orgID, actor, models.AuditActionUpdate, models.AuditEntityEmailAccount, &id, "", "", nil, map[string]string{
		"action": "signin_switch", "status": m.Status, "connection_id": connectionID.String(), "automatic": strconv.FormatBool(automatic),
	})
}

func (s *Service) auditVendor(ctx context.Context, orgID, connectionID uuid.UUID, metadata map[string]string) {
	if s.audit == nil {
		return
	}
	id := connectionID
	s.audit.LogAction(ctx, orgID, uuid.Nil, models.AuditActionUpdate, models.AuditEntityMailboxVendor, &id, "", "", nil, metadata)
}

// SigninOptions is how a mailbox signs in and the switches that can succeed for it.
func (s *Service) SigninOptions(ctx context.Context, orgID, accountID uuid.UUID) (*models.MailboxSignin, *errx.Error) {
	st, err := s.repo.SigninState(ctx, orgID, accountID)
	if err != nil {
		return nil, errx.InternalError()
	}
	if st == nil {
		return nil, errx.ErrNotFound
	}
	in := signinInputs{automatic: s.automatic != nil && s.automatic(ctx)}
	if s.consent != nil {
		if id, _, ok := s.consent.VendorConsentApp(); ok {
			in.clientID = id
		}
	}
	domain := ""
	if at := strings.LastIndex(st.Email, "@"); at >= 0 {
		domain = strings.ToLower(st.Email[at+1:])
	}
	in.personal = mailhost.SharedProvider(domain)
	if s.grants != nil && st.Provider == models.InboxProviderGoogle && !in.personal {
		if g, err := s.grants.GrantFor(ctx, orgID, models.GrantProviderGoogle, domain); err == nil && g != nil && g.Status == "active" {
			in.grant = &models.MailboxSigninGrant{ID: g.ID, Domain: domain}
		}
	}
	out := decideSignin(*st, in)
	return &out, nil
}

// signinInputs is what decideSignin needs beyond the mailbox's own row.
type signinInputs struct {
	// clientID is the Google sign-in client; empty when Google sign-in is not set up here.
	clientID  string
	personal  bool
	grant     *models.MailboxSigninGrant
	automatic bool
}

// decideSignin names the mailbox's sign-in and offers only the switches that can succeed for it.
func decideSignin(st models.MailboxSigninState, in signinInputs) models.MailboxSignin {
	out := models.MailboxSignin{Method: models.SigninMethodOther, Personal: in.personal, Managed: st.Managed, Switch: st.Switch}
	switch {
	case st.Provider == models.InboxProviderGoogle && st.AuthMethod == models.MailAuthDelegated:
		out.Method = models.SigninMethodGrant
	case st.Provider == models.InboxProviderGoogle && st.AuthMethod == models.MailAuthOAuth:
		out.Method = models.SigninMethodGoogle
		out.FullMailAccess = models.FullMailAccess(st.GrantedScopes)
	case st.Provider == models.InboxProviderSMTPIMAP && mailhost.Host(st.MailHost).Google():
		out.Method = models.SigninMethodAppPassword
	}
	if out.Method == models.SigninMethodOther || st.Managed {
		return out
	}
	full := out.FullMailAccess != nil && *out.FullMailAccess
	lacking := out.Method == models.SigninMethodGrant || (out.Method == models.SigninMethodGoogle && !full)

	out.Options.AppPassword = out.Method != models.SigninMethodAppPassword
	if out.Method == models.SigninMethodGoogle && !full && !in.personal && in.clientID != "" {
		out.Options.GoogleSignin = &models.MailboxSigninGoogle{ClientID: in.clientID}
	}
	if out.Method == models.SigninMethodGoogle && in.grant != nil {
		out.Options.Grant = in.grant
	}
	if v := st.Vendor; v != nil && v.Status == "active" && mailvendor.SupportsConsent(v.Vendor) && st.VendorMailboxID != "" &&
		in.clientID != "" && !in.personal && (lacking || out.Method == models.SigninMethodAppPassword) {
		out.Options.Vendor = &models.MailboxSigninVendor{ConnectionID: v.ID, Vendor: v.Vendor, Label: v.Label}
		out.AutoSwitch = in.automatic && lacking && st.Status == "active"
	}
	return out
}
