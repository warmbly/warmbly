package advanced

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/inboxtag"
	"github.com/warmbly/warmbly/internal/app/replyclassify"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// optOutAdvancedRepo records suppressions and serves the recheck's list.
type optOutAdvancedRepo struct {
	*incomingReplyAdvancedRepo
	suppressed []string
	entries    []models.SuppressedRecipient
	deleted    []uuid.UUID
	checked    map[uuid.UUID]string
	// rewritten marks entries a newer unsubscribe upserted mid-recheck.
	rewritten map[uuid.UUID]bool
}

func (r *optOutAdvancedRepo) UpsertSuppressedRecipient(_ context.Context, e *models.SuppressedRecipient) error {
	r.suppressed = append(r.suppressed, e.Email)
	return nil
}

func (r *optOutAdvancedRepo) ListUncheckedReplyOptOuts(_ context.Context, afterID uuid.UUID, limit int) ([]models.SuppressedRecipient, error) {
	var out []models.SuppressedRecipient
	for _, e := range r.entries {
		if e.ID.String() > afterID.String() && r.checked[e.ID] == "" && !r.wasDeleted(e.ID) {
			out = append(out, e)
		}
	}
	return out[:min(len(out), limit)], nil
}

func (r *optOutAdvancedRepo) wasDeleted(id uuid.UUID) bool {
	for _, d := range r.deleted {
		if d == id {
			return true
		}
	}
	return false
}

func (r *optOutAdvancedRepo) MarkReplyOptOutChecked(_ context.Context, id uuid.UUID, outcome string) error {
	r.checked[id] = outcome
	return nil
}

func (r *optOutAdvancedRepo) DeleteReplyOptOut(_ context.Context, _ uuid.UUID, id uuid.UUID, updatedAt time.Time) (bool, error) {
	for _, e := range r.entries {
		if e.ID == id && !e.UpdatedAt.Equal(updatedAt) {
			return false, nil
		}
	}
	if r.rewritten[id] {
		return false, nil
	}
	r.deleted = append(r.deleted, id)
	return true, nil
}

type optOutContactRepo struct {
	incomingReplyContactRepo
	resubscribed []string
}

func (r *optOutContactRepo) SetSubscribedByEmail(_ context.Context, _ uuid.UUID, email string, subscribed bool) error {
	if subscribed {
		r.resubscribed = append(r.resubscribed, email)
	}
	return nil
}

type optOutUnibox struct {
	repository.UniboxRepository
	byAddress map[string][]repository.InboundMessage
	written   map[string]bool
}

func (u optOutUnibox) HasWrittenTo(_ context.Context, _ uuid.UUID, address string) (bool, error) {
	return u.written[address], nil
}

func (u optOutUnibox) ListInboundFrom(_ context.Context, _ uuid.UUID, address string, _ int) ([]repository.InboundMessage, error) {
	return u.byAddress[address], nil
}

func newOptOutService(senderContact *models.Contact) (*service, *optOutAdvancedRepo, *optOutContactRepo, uuid.UUID) {
	orgID, accountID := uuid.New(), uuid.New()
	account := &models.Email{ID: accountID, OrganizationID: &orgID, Email: "sender@example.test"}
	svc, progress := newIncomingReplyService(account, senderContact, uuid.New())
	repo := &optOutAdvancedRepo{incomingReplyAdvancedRepo: progress.advanced, checked: map[uuid.UUID]string{}, rewritten: map[uuid.UUID]bool{}}
	contacts := &optOutContactRepo{incomingReplyContactRepo: svc.contactRepo.(incomingReplyContactRepo)}
	svc.repo = repo
	svc.contactRepo = contacts
	return svc, repo, contacts, accountID
}

func inbound(accountID uuid.UUID, from, subject, body string, inReplyTo []string, flags ...string) *models.EmailMessageStoreData {
	return &models.EmailMessageStoreData{
		ID:        uuid.New(),
		EmailID:   accountID,
		Folder:    models.FolderInbox,
		FromAddr:  []string{from},
		ToAddr:    []string{"sender@example.test"},
		InReplyTo: inReplyTo,
		Subject:   subject,
		BodyText:  body,
		Flags:     flags,
	}
}

// Only a person answering our outreach can ask us to stop. A newsletter's
// footer, a bounce returning our own List-Unsubscribe, or a reply quoting our
// footer on one line suppressed their senders, which is what put every mail a
// warming mailbox received on the suppression list.
func TestReplyOptOutOnlyForPeopleAnsweringUs(t *testing.T) {
	quotedFooterOneLine := "Sounds good, talk Monday. On Tue, Sep 22, 2026 at 10:00 AM Sender <sender@example.test> wrote: > Quick question > Unsubscribe: https://t.example.test/unsubscribe/abc"
	cases := []struct {
		name     string
		contact  *models.Contact
		msg      func(accountID uuid.UUID) *models.EmailMessageStoreData
		suppress bool
	}{
		{
			name: "newsletter from a stranger",
			msg: func(a uuid.UUID) *models.EmailMessageStoreData {
				return inbound(a, "News <news@example.org>", "This week's update", "Read more. Unsubscribe from these emails.", nil)
			},
		},
		{
			name:    "newsletter from a contact",
			contact: &models.Contact{ID: uuid.New(), Email: "news@example.org"},
			msg: func(a uuid.UUID) *models.EmailMessageStoreData {
				return inbound(a, "News <news@example.org>", "This week's update", "Read more. Unsubscribe from these emails.", nil,
					"List-Unsubscribe:<https://example.org/u>")
			},
		},
		{
			name: "bounce in our thread",
			msg: func(a uuid.UUID) *models.EmailMessageStoreData {
				return inbound(a, "Mail Delivery Subsystem <mailer-daemon@googlemail.com>", "Delivery Status Notification (Failure)",
					"The group may not exist. List-Unsubscribe: <https://t.example.test/unsubscribe/abc>", []string{"<opener@example.test>"},
					"Auto-Submitted:auto-replied")
			},
		},
		{
			name: "reply quoting our footer on one line",
			msg: func(a uuid.UUID) *models.EmailMessageStoreData {
				return inbound(a, "Lead <lead@example.org>", "Re: Quick question", quotedFooterOneLine, []string{"<opener@example.test>"})
			},
		},
		{
			name: "person in our thread asking to stop",
			msg: func(a uuid.UUID) *models.EmailMessageStoreData {
				return inbound(a, "Lead <lead@example.org>", "Re: Quick question", "Please remove me from your list.", []string{"<opener@example.test>"})
			},
			suppress: true,
		},
		{
			name:    "contact writing fresh to ask",
			contact: &models.Contact{ID: uuid.New(), Email: "lead@example.org"},
			msg: func(a uuid.UUID) *models.EmailMessageStoreData {
				return inbound(a, "Lead <lead@example.org>", "Your emails", "Please stop emailing me.", nil)
			},
			suppress: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, _, accountID := newOptOutService(tc.contact)
			if xerr := svc.ProcessIncomingReply(context.Background(), accountID, tc.msg(accountID)); xerr != nil {
				t.Fatal(xerr)
			}
			if got := len(repo.suppressed) > 0; got != tc.suppress {
				t.Fatalf("suppressed = %v (%v), want %v", got, repo.suppressed, tc.suppress)
			}
		})
	}
}

func TestReplyOptOutEligible(t *testing.T) {
	human := replyclassify.Result{Class: replyclassify.ClassNeutral}
	bounce := replyclassify.Result{Class: replyclassify.ClassAutoReply}
	list := map[string][]string{"List-Id": {"<updates.example.org>"}}
	if !replyOptOutEligible(human, true, false, list) {
		t.Error("a person in our thread is always eligible")
	}
	if replyOptOutEligible(bounce, true, true, nil) {
		t.Error("an automated message is never eligible")
	}
	if replyOptOutEligible(human, false, false, nil) {
		t.Error("a stranger outside our threads is not eligible")
	}
	if replyOptOutEligible(human, false, true, list) {
		t.Error("list mail from a contact is not eligible")
	}
	if !replyOptOutEligible(human, false, true, nil) {
		t.Error("a contact writing to us is eligible")
	}
}

type recordingAudit struct{ lifted []string }

func (a *recordingAudit) LogAction(_ context.Context, _, _ uuid.UUID, _ models.AuditAction, _ models.AuditEntityType, _ *uuid.UUID, _, _ string, _, metadata map[string]string) {
	a.lifted = append(a.lifted, metadata["value"])
}

type failingTaskRepo struct{ incomingReplyTaskRepo }

func (failingTaskRepo) GetTaskByMessageID(context.Context, string) (*repository.Task, error) {
	return nil, errors.New("connection reset")
}

// The recheck lifts an entry only on positive evidence and never on a failed
// read: it keeps what a message supports, what a link or one-click started, what
// has no mail behind it, what was attributed to a campaign since deleted, and
// what a newer unsubscribe rewrote while it was reading.
func TestRecheckReplyOptOuts(t *testing.T) {
	svc, repo, contacts, accountID := newOptOutService(nil)
	audit := &recordingAudit{}
	svc.WireAudit(audit)
	now := time.Now()
	orgID := *svc.campaignRepo.(incomingReplyCampaignRepo).campaign.OrganizationID
	entry := func(email string, created time.Time) models.SuppressedRecipient {
		return models.SuppressedRecipient{ID: uuid.New(), OrganizationID: orgID, Email: email, Kind: models.SuppressionKindEmail,
			Source: models.DeliverabilityEventUnsubscribe, CreatedAt: created, UpdatedAt: created,
			Metadata: map[string]interface{}{"via": "reply"}}
	}
	msg := func(from, subject, body string, inReplyTo []string, processed time.Time, flags ...string) repository.InboundMessage {
		m := repository.InboundMessage{EmailMessageStoreData: *inbound(accountID, from, subject, body, inReplyTo, flags...), ProcessedAt: processed}
		m.CreatedAt = processed
		return m
	}

	newsletter := entry("news@example.org", now)
	bounce := entry("bounced@example.org", now)
	real := entry("lead@example.org", now)
	relabelled := entry("old@example.org", now)
	unknown := entry("gone@example.org", now)
	deletedCampaign := entry("former@example.org", now)
	campaignID := uuid.New()
	deletedCampaign.CampaignID = &campaignID
	raced := entry("raced@example.org", now)
	repo.rewritten[raced.ID] = true
	// A contact we mailed, since deleted along with the campaign.
	mailedBefore := entry("mailed@example.org", now)
	repo.entries = []models.SuppressedRecipient{newsletter, bounce, real, relabelled, unknown, deletedCampaign, raced, mailedBefore}

	flatQuote := "Thanks, will read. On Tue, Sep 22, 2026 at 10:00 AM Sender <s@example.test> wrote: > Unsubscribe: https://t.example.test/u/1"
	svc.uniboxRepo = optOutUnibox{byAddress: map[string][]repository.InboundMessage{
		"news@example.org":    {msg("News <news@example.org>", "Weekly", "Read more. Unsubscribe here.", nil, now.Add(-time.Second))},
		"bounced@example.org": {msg("Mail Delivery Subsystem <mailer-daemon@googlemail.com>", "Delivery Status Notification (Failure)", flatQuote, []string{"<opener@example.test>"}, now.Add(-time.Second), "Auto-Submitted:auto-replied")},
		"lead@example.org":    {msg("Lead <lead@example.org>", "Re: Hi", "Please remove me.", []string{"<opener@example.test>"}, now.Add(-time.Second))},
		// Mail from long before the entry: a link click started it.
		"old@example.org":    {msg("Old <old@example.org>", "Weekly", "Unsubscribe here.", nil, now.Add(-48*time.Hour))},
		"former@example.org": {msg("Former <former@example.org>", "Your emails", "Please stop emailing me.", nil, now.Add(-time.Second))},
		"raced@example.org":  {msg("Raced <raced@example.org>", "Weekly", "Unsubscribe here.", nil, now.Add(-time.Second))},
		"mailed@example.org": {msg("Mailed <mailed@example.org>", "Your emails", "Please stop emailing me.", nil, now.Add(-time.Second))},
	}, written: map[string]bool{"mailed@example.org": true}}

	if _, _, err := svc.RecheckReplyOptOuts(context.Background(), uuid.Nil, 100); err != nil {
		t.Fatal(err)
	}

	lifted := map[uuid.UUID]bool{}
	for _, id := range repo.deleted {
		lifted[id] = true
	}
	if len(lifted) != 2 || !lifted[newsletter.ID] || !lifted[bounce.ID] {
		t.Fatalf("lifted %v, want only the newsletter %v and the bounce %v", repo.deleted, newsletter.ID, bounce.ID)
	}
	if len(contacts.resubscribed) != 2 || len(audit.lifted) != 2 {
		t.Fatalf("resubscribed %v and audited %v, want both lifted addresses in each", contacts.resubscribed, audit.lifted)
	}
	for id, want := range map[uuid.UUID]string{
		real.ID:            replyOptOutKept,
		relabelled.ID:      replyOptOutOtherOrigin,
		unknown.ID:         replyOptOutNoEvidence,
		deletedCampaign.ID: replyOptOutKept,
		mailedBefore.ID:    replyOptOutKept,
		raced.ID:           "",
	} {
		if got := repo.checked[id]; got != want {
			t.Errorf("entry %v outcome = %q, want %q", id, got, want)
		}
	}
}

// A read that fails keeps the entry: a database blip is not evidence.
func TestRecheckReplyOptOutsKeepsOnAFailedRead(t *testing.T) {
	svc, repo, contacts, accountID := newOptOutService(nil)
	svc.taskRepo = failingTaskRepo{svc.taskRepo.(incomingReplyTaskRepo)}
	now := time.Now()
	orgID := *svc.campaignRepo.(incomingReplyCampaignRepo).campaign.OrganizationID
	e := models.SuppressedRecipient{ID: uuid.New(), OrganizationID: orgID, Email: "lead@example.org", CreatedAt: now, UpdatedAt: now,
		Metadata: map[string]interface{}{"via": "reply"}}
	repo.entries = []models.SuppressedRecipient{e}
	m := repository.InboundMessage{EmailMessageStoreData: *inbound(accountID, "Lead <lead@example.org>", "Re: Hi", "Please remove me.", []string{"<opener@example.test>"}), ProcessedAt: now}
	svc.uniboxRepo = optOutUnibox{byAddress: map[string][]repository.InboundMessage{"lead@example.org": {m}}}

	if _, _, err := svc.RecheckReplyOptOuts(context.Background(), uuid.Nil, 100); err != nil {
		t.Fatal(err)
	}
	if len(repo.deleted) != 0 || len(contacts.resubscribed) != 0 || repo.checked[e.ID] != "" {
		t.Fatalf("a failed read changed the entry: deleted %v resubscribed %v outcome %q", repo.deleted, contacts.resubscribed, repo.checked[e.ID])
	}
}

// The inbox tagger's own suppress action follows the same rule: a newsletter
// or a stranger outside our threads never lands on the list.
func TestInboxTagSuppressFollowsTheOptOutRule(t *testing.T) {
	plan := inboxtag.Plan{Suppress: "asked to be removed in a reply"}
	cases := []struct {
		name      string
		contact   *models.Contact
		inReplyTo []string
		headers   map[string][]string
		from      string
		want      bool
	}{
		{name: "stranger outside our threads"},
		{name: "contact's list mail", contact: &models.Contact{ID: uuid.New(), Email: "news@example.org"},
			headers: map[string][]string{"List-Id": {"<news.example.org>"}}},
		{name: "person in our thread", inReplyTo: []string{"<opener@example.test>"}, want: true},
		{name: "contact writing in", contact: &models.Contact{ID: uuid.New(), Email: "news@example.org"}, want: true},
		{name: "contact writing in under a display name", contact: &models.Contact{ID: uuid.New(), Email: "news@example.org"},
			from: "News Desk <News@Example.org>", want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, _, _ := newOptOutService(tc.contact)
			orgID := *svc.campaignRepo.(incomingReplyCampaignRepo).campaign.OrganizationID
			from := tc.from
			if from == "" {
				from = "news@example.org"
			}
			svc.ApplyInboxTagActions(context.Background(), InboxTagAction{
				OrganizationID: orgID, Sender: from, Plan: plan,
				InReplyTo: tc.inReplyTo, Headers: tc.headers,
			})
			if got := len(repo.suppressed) > 0; got != tc.want {
				t.Fatalf("suppressed = %v, want %v", got, tc.want)
			}
			if tc.want && repo.suppressed[0] != "news@example.org" {
				t.Fatalf("suppressed %q, want the bare address", repo.suppressed[0])
			}
		})
	}
}
