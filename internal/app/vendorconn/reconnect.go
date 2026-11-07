package vendorconn

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/app/mailboximport"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/jobrun"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/mailvendor"
	"github.com/warmbly/warmbly/internal/repository"
)

// Reconnect statuses, one per mailbox.
const (
	ReconnectQueued    = "queued"
	ReconnectTrusting  = "trusting"
	ReconnectSigningIn = "signing_in"
	ReconnectConnected = "connected"
	ReconnectFailed    = "failed"
)

// Refusals of a vendor reconnect a client branches on.
const (
	ErrIDReconnectUnavailable = "mailbox_vendor_reconnect_unavailable"
	ErrIDReconnectUnsupported = "mailbox_vendor_reconnect_unsupported"
	ErrIDReconnectNone        = "mailbox_vendor_reconnect_none"
)

const (
	reconnectJobsIndex = "vendor_reconnect_jobs"
	reconnectJobTTL    = consentStateTTL
	// maxReconnectMailboxes bounds one request; a larger vendor account reconnects in several.
	maxReconnectMailboxes = 1000
)

func reconnectJobKey(orgID, connectionID uuid.UUID) string {
	return "vendor_reconnect_job:" + orgID.String() + ":" + connectionID.String()
}

// ReconnectMailbox is where one mailbox's vendor sign-in stands.
type ReconnectMailbox struct {
	EmailAccountID uuid.UUID `json:"email_account_id"`
	Email          string    `json:"email"`
	Status         string    `json:"status"`
	Message        string    `json:"message,omitempty"`
}

func (m ReconnectMailbox) final() bool {
	return m.Status == ReconnectConnected || m.Status == ReconnectFailed
}

// reconnectJob is a member's request to have the vendor sign in again as a connection's mailboxes; the upkeep loop drives it.
type reconnectJob struct {
	OrgID        uuid.UUID          `json:"org_id"`
	UserID       uuid.UUID          `json:"user_id"`
	ConnectionID uuid.UUID          `json:"connection_id"`
	Started      time.Time          `json:"started"`
	Mailboxes    []ReconnectMailbox `json:"mailboxes"`
	// VendorMailboxIDs are the vendor's ids the consent requests name, kept out of the answer.
	VendorMailboxIDs map[uuid.UUID]string `json:"vendor_mailbox_ids"`
	// Automatic marks the mailboxes the automatic switch started rather than a member.
	Automatic map[uuid.UUID]bool `json:"automatic,omitempty"`
}

func reconnectUnavailable() *errx.Error {
	return errx.NewWithIdentifier(errx.BadRequest, ErrIDReconnectUnavailable,
		"Google sign-in is not set up on this instance, so an inbox vendor cannot sign in to mailboxes for Warmbly.")
}

// Reconnect has the vendor sign in again as one Google mailbox linked to the
// connection, or every one, renewing or converting each in place. It answers at
// once; the vendor's work goes on in the background and ReconnectStatus reads it.
func (s *Service) Reconnect(ctx context.Context, orgID, userID, connectionID uuid.UUID, accountID *uuid.UUID) ([]ReconnectMailbox, *errx.Error) {
	var only map[uuid.UUID]bool
	if accountID != nil {
		only = map[uuid.UUID]bool{*accountID: true}
	}
	return s.startReconnect(ctx, orgID, userID, connectionID, only, false)
}

// ReconnectMailboxes is Reconnect for several of the connection's mailboxes at once.
func (s *Service) ReconnectMailboxes(ctx context.Context, orgID, userID, connectionID uuid.UUID, accountIDs []uuid.UUID) ([]ReconnectMailbox, *errx.Error) {
	if len(accountIDs) > maxReconnectMailboxes {
		return nil, errx.NewWithIdentifier(errx.BadRequest, ErrIDReconnectNone,
			"One request reconnects up to "+strconv.Itoa(maxReconnectMailboxes)+" mailboxes. Reconnect them in smaller groups.")
	}
	only := make(map[uuid.UUID]bool, len(accountIDs))
	for _, id := range accountIDs {
		only[id] = true
	}
	return s.startReconnect(ctx, orgID, userID, connectionID, only, false)
}

// startReconnect starts the vendor signing in again as the connection's Google mailboxes, or only those in only.
func (s *Service) startReconnect(ctx context.Context, orgID, userID, connectionID uuid.UUID, only map[uuid.UUID]bool, automatic bool) ([]ReconnectMailbox, *errx.Error) {
	if s.cache == nil || s.consent == nil {
		return nil, reconnectUnavailable()
	}
	if _, _, ok := s.consent.VendorConsentApp(); !ok {
		return nil, reconnectUnavailable()
	}
	c, xerr := s.get(ctx, orgID, connectionID)
	if xerr != nil {
		return nil, xerr
	}
	client, xerr := s.client(ctx, c)
	if xerr != nil {
		return nil, xerr
	}
	if _, ok := client.(mailvendor.ConsentRequester); !ok {
		return nil, errx.NewWithIdentifier(errx.BadRequest, ErrIDReconnectUnsupported, labelOf(c.Vendor)+" cannot sign in to mailboxes for Warmbly.")
	}
	linked, err := s.repo.LinkedGoogleMailboxes(ctx, orgID, connectionID)
	if err != nil {
		return nil, errx.InternalError()
	}
	if only != nil {
		var some []repository.VendorReconnect
		for _, l := range linked {
			if only[l.AccountID] {
				some = append(some, l)
			}
		}
		linked = some
	}
	if len(linked) == 0 {
		msg := "This vendor account has no Google mailboxes in this workspace."
		if only != nil {
			msg = "That mailbox is not a Google mailbox imported from this vendor account."
		}
		return nil, errx.NewWithIdentifier(errx.NotFound, ErrIDReconnectNone, msg)
	}
	if len(linked) > maxReconnectMailboxes {
		return nil, errx.NewWithIdentifier(errx.BadRequest, ErrIDReconnectNone,
			"One request reconnects up to "+strconv.Itoa(maxReconnectMailboxes)+" mailboxes. Reconnect them one at a time, or in smaller groups.")
	}

	key := reconnectJobKey(orgID, connectionID)
	job := reconnectJob{OrgID: orgID, UserID: userID, ConnectionID: connectionID, Started: time.Now(),
		VendorMailboxIDs: map[uuid.UUID]string{}, Automatic: map[uuid.UUID]bool{}}
	var prev reconnectJob
	if s.cache.GetJSON(ctx, key, &prev) == nil {
		for _, m := range prev.Mailboxes {
			if !m.final() {
				job.Mailboxes = append(job.Mailboxes, m)
				job.VendorMailboxIDs[m.EmailAccountID] = prev.VendorMailboxIDs[m.EmailAccountID]
				if prev.Automatic[m.EmailAccountID] {
					job.Automatic[m.EmailAccountID] = true
				}
			}
		}
	}
	var started []uuid.UUID
	for _, l := range linked {
		if _, open := job.VendorMailboxIDs[l.AccountID]; open {
			continue
		}
		// An explicit ask starts a finished sign-in over; one still open at the vendor is never sent twice.
		var st consentState
		if s.cache.GetJSON(ctx, consentKey(orgID, connectionID, l.Email), &st) == nil && (st.Done || st.Failed != "") {
			_ = s.cache.Del(ctx, consentKey(orgID, connectionID, l.Email))
		}
		job.Mailboxes = append(job.Mailboxes, ReconnectMailbox{EmailAccountID: l.AccountID, Email: l.Email, Status: ReconnectQueued})
		job.VendorMailboxIDs[l.AccountID] = l.VendorMailboxID
		if automatic {
			job.Automatic[l.AccountID] = true
		}
		started = append(started, l.AccountID)
	}
	if err := s.cache.SetJSON(ctx, key, job, reconnectJobTTL); err != nil {
		return nil, errx.InternalError()
	}
	if err := s.cache.SAdd(ctx, reconnectJobsIndex, key).Err(); err != nil {
		return nil, errx.InternalError()
	}
	// The automatic switch recorded its start when it claimed the mailboxes.
	if !automatic && len(started) > 0 {
		if _, err := s.repo.StartSigninSwitch(ctx, orgID, started, false); err != nil {
			log.Warn().Err(err).Str("organization_id", orgID.String()).Msg("vendor reconnect: recording the start failed")
		}
	}
	// The first step runs now rather than at the next pass; the loop carries on from it.
	if s.kickReconnect != nil {
		s.kickReconnect(context.WithoutCancel(ctx), key)
	}
	out := make([]ReconnectMailbox, 0, len(linked))
	for _, m := range job.Mailboxes {
		if only == nil || only[m.EmailAccountID] {
			out = append(out, m)
		}
	}
	return out, nil
}

// ReconnectStatus is where the connection's last reconnect stands, mailbox by mailbox.
func (s *Service) ReconnectStatus(ctx context.Context, orgID, connectionID uuid.UUID) ([]ReconnectMailbox, *errx.Error) {
	if _, xerr := s.get(ctx, orgID, connectionID); xerr != nil {
		return nil, xerr
	}
	if s.cache == nil {
		return []ReconnectMailbox{}, nil
	}
	var job reconnectJob
	if err := s.cache.GetJSON(ctx, reconnectJobKey(orgID, connectionID), &job); err != nil {
		if errors.Is(err, redis.Nil) {
			return []ReconnectMailbox{}, nil
		}
		return nil, errx.InternalError()
	}
	if job.OrgID != orgID {
		return []ReconnectMailbox{}, nil
	}
	return job.Mailboxes, nil
}

// startReconnectDriver runs the reconnect jobs every minute until ctx ends.
func (s *Service) startReconnectDriver(ctx context.Context) {
	if s.cache == nil {
		return
	}
	jobrun.Loop(ctx, "mailbox_vendor_consent_reconnect", time.Minute, false, func(ctx context.Context) error {
		keys, err := s.cache.SMembers(ctx, reconnectJobsIndex).Result()
		if err != nil {
			return err
		}
		for _, key := range keys {
			s.driveReconnectKey(ctx, key)
		}
		return nil
	})
}

// driveReconnectKey moves one job a step on and drops it from the index once every mailbox has an answer.
func (s *Service) driveReconnectKey(ctx context.Context, key string) {
	var job reconnectJob
	if err := s.cache.GetJSON(ctx, key, &job); err != nil {
		if errors.Is(err, redis.Nil) {
			_ = s.cache.SRem(ctx, reconnectJobsIndex, key).Err()
		}
		return
	}
	// A job is read back only under the key its own organization and connection name.
	if key != reconnectJobKey(job.OrgID, job.ConnectionID) {
		_ = s.cache.SRem(ctx, reconnectJobsIndex, key).Err()
		return
	}
	before := make(map[uuid.UUID]ReconnectMailbox, len(job.Mailboxes))
	for _, m := range job.Mailboxes {
		before[m.EmailAccountID] = m
	}
	open := s.driveReconnect(ctx, &job)
	s.recordReconnect(ctx, &job, before)
	if ttl := time.Until(job.Started.Add(reconnectJobTTL)); ttl > 0 {
		_ = s.cache.SetJSON(ctx, key, job, ttl)
	}
	if !open {
		_ = s.cache.SRem(ctx, reconnectJobsIndex, key).Err()
	}
}

// driveReconnect moves every unfinished mailbox of the job one step; open is false once all have an answer.
func (s *Service) driveReconnect(ctx context.Context, job *reconnectJob) (open bool) {
	failAll := func(msg string) bool {
		for i := range job.Mailboxes {
			if !job.Mailboxes[i].final() {
				job.Mailboxes[i].Status, job.Mailboxes[i].Message = ReconnectFailed, msg
			}
		}
		return false
	}
	if time.Since(job.Started) > reconnectJobTTL {
		return failAll("The vendor did not finish signing in within a day.")
	}
	c, xerr := s.get(ctx, job.OrgID, job.ConnectionID)
	if xerr != nil {
		if xerr.Code == errx.Internal {
			return true
		}
		return failAll("The vendor account is no longer connected to this workspace.")
	}
	client, xerr := s.client(ctx, c)
	if xerr != nil {
		if xerr.Code == errx.Internal {
			return true
		}
		return failAll(xerr.Message)
	}
	cr, ok := client.(mailvendor.ConsentRequester)
	if !ok {
		return failAll(labelOf(c.Vendor) + " cannot sign in to mailboxes for Warmbly.")
	}
	if s.consent == nil {
		return failAll(reconnectUnavailable().Message)
	}
	clientID, appName, ok := s.consent.VendorConsentApp()
	if !ok {
		return failAll(reconnectUnavailable().Message)
	}
	for i := range job.Mailboxes {
		m := &job.Mailboxes[i]
		if m.final() {
			continue
		}
		s.reconnectStep(ctx, c, client, cr, job, m, clientID, appName)
		open = open || !m.final()
	}
	return open
}

// reconnectStep moves one mailbox on: the domain's trust first, then the vendor signing in as it.
func (s *Service) reconnectStep(ctx context.Context, c *models.VendorConnection, client mailvendor.Client, cr mailvendor.ConsentRequester, job *reconnectJob, m *ReconnectMailbox, clientID, appName string) {
	label := labelOf(c.Vendor)
	at := strings.LastIndex(m.Email, "@")
	if at < 0 {
		m.Status, m.Message = ReconnectFailed, "The mailbox has no domain."
		return
	}
	domain := strings.ToLower(m.Email[at+1:])
	var st consentState
	if s.cache.GetJSON(ctx, consentKey(job.OrgID, c.ID, m.Email), &st) == nil && st.Done {
		m.Status, m.Message = ReconnectConnected, ""
		return
	}
	trust, done := s.trustDomain(ctx, c, client, cr, job.OrgID, domain, clientID, appName)
	if !done {
		if trust.Pending {
			m.Status, m.Message = ReconnectTrusting, ""
			return
		}
		m.Status, m.Message = ReconnectFailed, firstNonEmpty(trust.Message, label+" could not add Warmbly as a trusted app on "+domain+".")
		return
	}
	id := m.EmailAccountID
	auth := s.consentMailbox(ctx, c, cr, mailboximport.VendorAuthorizeRequest{
		OrgID: job.OrgID, UserID: job.UserID, ConnectionID: c.ID, Email: m.Email, Provider: models.GrantProviderGoogle,
		VendorMailboxID: job.VendorMailboxIDs[id], Consent: true, EmailAccountID: &id,
	}, domain, clientID, appName)
	switch {
	case auth.Pending:
		m.Status, m.Message = ReconnectSigningIn, ""
	case auth.Message != "":
		m.Status, m.Message = ReconnectFailed, auth.Message
	default:
		m.Status, m.Message = ReconnectConnected, ""
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
