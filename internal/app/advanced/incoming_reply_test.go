package advanced

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/inboxtag"
	"github.com/warmbly/warmbly/internal/app/replyclassify"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type incomingReplyAdvancedRepo struct {
	repository.AdvancedOutreachRepository
	marked    int
	intents   int
	intentOff bool
	holdOff   bool
	languages []string
}

type incomingReplyNotifier struct {
	Notifier
	categories chan models.NotificationCategory
}

func (n *incomingReplyNotifier) NotifyAboutMessage(_ context.Context, _ uuid.UUID, _ *uuid.UUID, _ uuid.UUID, category models.NotificationCategory, _, _, _ string, _ map[string]any) {
	n.categories <- category
}

func TestProcessIncomingReplyHonorsMachineVerdicts(t *testing.T) {
	for _, tc := range []struct {
		name       string
		kind       string
		confidence float64
		review     string
		subject    string
		sender     string
		flags      []string
		want       models.NotificationCategory
	}{
		{name: "DMARC without tagging", subject: "Report Domain: example.test Submitter: seznam.cz Report-ID: abc"},
		{name: "system sender without tagging", sender: "no-reply@accounts.example.test", subject: "Security alert"},
		{name: "delivery failure without tagging", subject: "Delivery Status Notification (Failure)", flags: []string{"Content-Type:multipart/report; report-type=delivery-status"}},
		{name: "human discussing DMARC", subject: "Re: Report Domain: example.test Submitter: seznam.cz Report-ID: abc", want: models.NotifInboundReply},
		{name: "system notice", kind: inboxtag.KindNotification, confidence: 0.95},
		{name: "hard bounce", kind: inboxtag.KindBounceHard, confidence: 0.95},
		{name: "soft bounce", kind: inboxtag.KindBounceSoft, confidence: 0.95},
		{name: "out of office without recognizable subject", kind: inboxtag.KindAutoReplyOOO, confidence: 0.95, want: models.NotifInboundOOO},
		{name: "ticket acknowledgement without recognizable subject", kind: inboxtag.KindAutoReplyTicket, confidence: 0.95, want: models.NotifInboundOOO},
		{name: "human reply", kind: inboxtag.KindHumanReply, confidence: 0.95, want: models.NotifInboundReply},
		{name: "uncertain kind", kind: inboxtag.KindNotification, confidence: 0.3, review: "kind", want: models.NotifInboundReply},
		{name: "kind awaiting review", kind: inboxtag.KindNotification, confidence: 0.95, review: "kind", want: models.NotifInboundReply},
	} {
		t.Run(tc.name, func(t *testing.T) {
			orgID, accountID, contactID := uuid.New(), uuid.New(), uuid.New()
			account := &models.Email{ID: accountID, OrganizationID: &orgID, UserID: uuid.NewString(), Email: "sender@example.test"}
			s, progress := newIncomingReplyService(account, nil, contactID)
			s.taskRepo = incomingReplyTaskRepo{}
			s.contactRepo = incomingReplyContactRepo{}
			n := &incomingReplyNotifier{categories: make(chan models.NotificationCategory, 1)}
			s.notifier = n
			if tc.kind != "" {
				s.inboxTags = verdictStore{res: &repository.InboxTagResult{
					Kind: tc.kind, KindConfidence: tc.confidence, ReviewReason: tc.review,
				}}
			}
			msg := &models.EmailMessageStoreData{
				ID: uuid.New(), EmailID: accountID, MessageID: "<report@example.test>",
				Folder: models.FolderInbox, FromAddr: []string{"them@example.test"}, ToAddr: []string{account.Email},
				Subject: tc.subject, Snippet: "Received, thank you.", Flags: tc.flags,
			}
			if tc.sender != "" {
				msg.FromAddr = []string{tc.sender}
			}
			if err := s.ProcessIncomingReply(context.Background(), accountID, msg); err != nil {
				t.Fatal(err)
			}
			if tc.want == "" {
				if progress.claims != 0 || progress.replied != 0 || progress.advanced.intents != 0 {
					t.Fatalf("system mail entered reply processing: %+v", progress)
				}
				select {
				case category := <-n.categories:
					t.Fatalf("system mail raised %s", category)
				case <-time.After(20 * time.Millisecond):
				}
				return
			}
			select {
			case category := <-n.categories:
				if category != tc.want {
					t.Fatalf("notification = %s, want %s", category, tc.want)
				}
			case <-time.After(time.Second):
				t.Fatal("reply notification not raised")
			}
		})
	}
}

func TestProcessIncomingReplyStoredAutorespondersNeverCountAsHumanReplies(t *testing.T) {
	for _, kind := range []string{inboxtag.KindAutoReplyOOO, inboxtag.KindAutoReplyTicket} {
		t.Run(kind, func(t *testing.T) {
			orgID, accountID, contactID := uuid.New(), uuid.New(), uuid.New()
			account := &models.Email{ID: accountID, OrganizationID: &orgID, Email: "sender@example.test"}
			s, progress := newIncomingReplyService(account, &models.Contact{ID: contactID, Email: "recipient@example.test"}, contactID)
			progress.advanced.holdOff = true
			s.inboxTags = verdictStore{res: &repository.InboxTagResult{Kind: kind, KindConfidence: 0.95}}
			if err := s.ProcessIncomingReply(context.Background(), accountID, &models.EmailMessageStoreData{
				ID: uuid.New(), EmailID: accountID, MessageID: "<reply@example.test>", Folder: models.FolderInbox,
				FromAddr: []string{"recipient@example.test"}, ToAddr: []string{account.Email}, InReplyTo: []string{"<opener@example.test>"},
				Subject: "Re: Hello", Snippet: "Received, thank you.",
			}); err != nil {
				t.Fatal(err)
			}
			if progress.replied != 0 || progress.advanced.marked != 0 || progress.classified != 1 {
				t.Fatalf("autoresponder changed human engagement: replied=%d marked=%d classified=%d", progress.replied, progress.advanced.marked, progress.classified)
			}
		})
	}
}

func (r *incomingReplyAdvancedRepo) GetOutreachSettings(context.Context, uuid.UUID) (*models.AdvancedOutreachSettings, error) {
	settings := models.DefaultAdvancedOutreachSettings()
	settings.ReplyIntent.Enabled = !r.intentOff
	settings.ReplyIntent.HoldOnOutOfOffice = !r.holdOff
	settings.InboxTagging.Languages = r.languages
	return &settings, nil
}

func (r *incomingReplyAdvancedRepo) MarkVariantEvent(context.Context, uuid.UUID, uuid.UUID, string) error {
	r.marked++
	return nil
}

func (r *incomingReplyAdvancedRepo) CreateReplyIntent(context.Context, *models.ReplyIntentRecord) error {
	r.intents++
	return nil
}

type incomingReplyEmailRepo struct {
	repository.EmailRepository
	account *models.Email
}

func (r incomingReplyEmailRepo) GetByID(context.Context, uuid.UUID) (*models.Email, *errx.Error) {
	return r.account, nil
}

type incomingReplyTaskRepo struct {
	repository.TaskRepository
	task     *repository.Task
	campaign *repository.CampaignTask
}

func (r incomingReplyTaskRepo) GetTaskByMessageID(context.Context, string) (*repository.Task, error) {
	return r.task, nil
}

func (r incomingReplyTaskRepo) GetCampaignTask(context.Context, uuid.UUID) (*repository.CampaignTask, error) {
	return r.campaign, nil
}

type incomingReplyContactRepo struct {
	repository.ContactRepository
	senderContact *models.Contact
	taskContact   *models.Contact
}

func (r incomingReplyContactRepo) GetByEmailAndOrganization(context.Context, uuid.UUID, string) (*models.Contact, *errx.Error) {
	return r.senderContact, nil
}

func (r incomingReplyContactRepo) GetByID(context.Context, uuid.UUID) (*models.Contact, *errx.Error) {
	return r.taskContact, nil
}

func (r incomingReplyContactRepo) GetByIDsAndOrganization(_ context.Context, _ uuid.UUID, ids []uuid.UUID) ([]models.Contact, *errx.Error) {
	if r.taskContact == nil || len(ids) != 1 || ids[0] != r.taskContact.ID {
		return nil, nil
	}
	return []models.Contact{*r.taskContact}, nil
}

type incomingReplyProgressRepo struct {
	repository.CampaignProgressRepository
	replied       int
	classified    int
	class         string
	claims        int
	completed     int
	latest        *repository.CampaignSequencePair
	latestBefore  time.Time
	sourceInbound bool
	sourceClaimed bool
	replyAccepted bool
	receivingSent bool
	completeErr   error
	advanced      *incomingReplyAdvancedRepo
	copies        []models.CampaignLeadCC
	copiedLead    *repository.CopiedLeadRef
	copiesErr     error
}

func (r *incomingReplyProgressRepo) ListLeadCC(context.Context, uuid.UUID, uuid.UUID) ([]models.CampaignLeadCC, error) {
	return r.copies, r.copiesErr
}

func (r *incomingReplyProgressRepo) LeadForCopiedReply(context.Context, uuid.UUID, uuid.UUID) (*repository.CopiedLeadRef, error) {
	return r.copiedLead, nil
}

func (r *incomingReplyProgressRepo) IsInboundReplySource(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return r.sourceInbound, nil
}

func (r *incomingReplyProgressRepo) CampaignContactSentFromAccount(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (bool, error) {
	return r.receivingSent, nil
}

func (r *incomingReplyProgressRepo) GetLatestCampaignSequenceForContact(_ context.Context, _ uuid.UUID, receivedAt time.Time) (*repository.CampaignSequencePair, error) {
	r.latestBefore = receivedAt
	return r.latest, nil
}

func (r *incomingReplyProgressRepo) GetLatestReplyClass(context.Context, uuid.UUID, uuid.UUID) (string, error) {
	return "", nil
}

func (r *incomingReplyProgressRepo) RecordReplyIntent(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, string) error {
	return nil
}

func (r *incomingReplyProgressRepo) RecordReplyClassification(_ context.Context, _, _, _ uuid.UUID, class, _ string, _ float64) error {
	r.classified++
	r.class = class
	return nil
}

func (r *incomingReplyProgressRepo) ClaimIncomingReply(context.Context, uuid.UUID, uuid.UUID) (uuid.UUID, error) {
	r.claims++
	if !r.sourceClaimed {
		return uuid.Nil, nil
	}
	return uuid.New(), nil
}

func (r *incomingReplyProgressRepo) CompleteIncomingReply(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error {
	r.completed++
	return r.completeErr
}

func (r *incomingReplyProgressRepo) RecordEmailReplied(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID) (bool, error) {
	r.replied++
	return r.replyAccepted, nil
}

type incomingReplyCampaignRepo struct {
	repository.CampaignRepository
	campaign *models.Campaign
}

func (r incomingReplyCampaignRepo) GetByID(context.Context, uuid.UUID) (*models.Campaign, error) {
	return r.campaign, nil
}

func (incomingReplyCampaignRepo) GetSequencesRoutingByCampaignID(context.Context, uuid.UUID) ([]models.Sequence, error) {
	return nil, nil
}

func newIncomingReplyService(account *models.Email, senderContact *models.Contact, taskContact uuid.UUID) (*service, *incomingReplyProgressRepo) {
	taskID, campaignID, sequenceID := uuid.New(), uuid.New(), uuid.New()
	progress := &incomingReplyProgressRepo{
		sourceInbound: true,
		sourceClaimed: true,
		replyAccepted: true,
		receivingSent: true,
	}
	advancedRepo := &incomingReplyAdvancedRepo{}
	progress.advanced = advancedRepo
	taskContactRecord := &models.Contact{ID: taskContact, Email: "task-contact@example.test"}
	if senderContact != nil && senderContact.ID == taskContact {
		taskContactRecord = senderContact
	}
	return &service{
		repo: advancedRepo,
		campaignRepo: incomingReplyCampaignRepo{campaign: &models.Campaign{
			ID:             campaignID,
			OrganizationID: account.OrganizationID,
		}},
		emailRepo: incomingReplyEmailRepo{account: account},
		taskRepo: incomingReplyTaskRepo{
			task:     &repository.Task{ID: taskID, TaskType: "campaign", EmailAccountID: account.ID},
			campaign: &repository.CampaignTask{TaskID: taskID, CampaignID: &campaignID, ContactID: &taskContact, SequenceID: &sequenceID},
		},
		contactRepo: incomingReplyContactRepo{
			senderContact: senderContact,
			taskContact:   taskContactRecord,
		},
		campaignProgressRepo: progress,
	}, progress
}

func TestMessageAddressesMailbox(t *testing.T) {
	account := &models.Email{
		Email:       "mailbox@example.test",
		SendAsEmail: "alias@example.test",
		ReplyTo:     "replies@example.test",
	}
	for _, tc := range []struct {
		name    string
		message *models.EmailMessageStoreData
		want    bool
	}{
		{name: "mailbox in to", message: &models.EmailMessageStoreData{ToAddr: []string{"Mailbox <mailbox@example.test>"}}, want: true},
		{name: "send alias in cc", message: &models.EmailMessageStoreData{CC: []string{"alias@example.test"}}, want: true},
		{name: "reply address in bcc", message: &models.EmailMessageStoreData{BCC: []string{"replies@example.test"}}, want: true},
		{name: "different recipient", message: &models.EmailMessageStoreData{ToAddr: []string{"other@example.test"}}},
		{name: "IMAP sync form in to", message: &models.EmailMessageStoreData{ToAddr: []string{"M Kannan (mailbox@example.test)"}}, want: true},
		{name: "IMAP sync form, other recipient", message: &models.EmailMessageStoreData{ToAddr: []string{"Other (other@example.test)"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := messageAddressesMailbox(tc.message, account); got != tc.want {
				t.Fatalf("messageAddressesMailbox() = %t, want %t", got, tc.want)
			}
		})
	}
}

// A person answers from whatever address their client picks: a Gmail "send
// as" alias, a forward, a colleague's desk. The thread names the send, and
// that is the evidence; the From address is not a second condition. This is
// the self-hoster's report: every reply arrived, none was ever counted.
func TestProcessIncomingReplyCountsAThreadReplyFromAnotherAddress(t *testing.T) {
	orgID, accountID, contactID := uuid.New(), uuid.New(), uuid.New()
	account := &models.Email{ID: accountID, OrganizationID: &orgID, Email: "sender@example.test"}
	// The alias is nobody in the contact list; only the thread can attribute it.
	service, progress := newIncomingReplyService(account, nil, contactID)

	xerr := service.ProcessIncomingReply(context.Background(), accountID, &models.EmailMessageStoreData{
		ID:        uuid.New(),
		EmailID:   accountID,
		Folder:    models.FolderInbox,
		FromAddr:  []string{"M K <alias@gmail.test>"},
		ToAddr:    []string{"sender@example.test"},
		InReplyTo: []string{"<opener@example.test>"},
		Subject:   "Re: Hello",
		Snippet:   "Received it, thank you.",
	})
	if xerr != nil {
		t.Fatal(xerr)
	}
	if progress.replied != 1 {
		t.Fatalf("RecordEmailReplied calls = %d, want 1 for a reply in the lead's own thread", progress.replied)
	}
	if progress.advanced.marked != 1 {
		t.Fatalf("MarkVariantEvent calls = %d, want the reply counted for the variant", progress.advanced.marked)
	}
}

// Turning reply-intent automation off must not turn reply detection off:
// replied_at, stop-on-reply and the analytics all hang on it.
func TestProcessIncomingReplyStampsRepliedWithIntentAutomationOff(t *testing.T) {
	orgID, accountID, contactID := uuid.New(), uuid.New(), uuid.New()
	account := &models.Email{ID: accountID, OrganizationID: &orgID, Email: "sender@example.test"}
	service, progress := newIncomingReplyService(account, &models.Contact{
		ID: contactID, Email: "recipient@example.test",
	}, contactID)
	progress.advanced.intentOff = true

	xerr := service.ProcessIncomingReply(context.Background(), accountID, &models.EmailMessageStoreData{
		ID:        uuid.New(),
		EmailID:   accountID,
		Folder:    models.FolderInbox,
		FromAddr:  []string{"Recipient <recipient@example.test>"},
		ToAddr:    []string{"sender@example.test"},
		InReplyTo: []string{"<opener@example.test>"},
		Subject:   "Re: Hello",
		Snippet:   "Sounds good, let's talk.",
	})
	if xerr != nil {
		t.Fatal(xerr)
	}
	if progress.replied != 1 {
		t.Fatalf("RecordEmailReplied calls = %d, want 1 with intent automation off", progress.replied)
	}
	if progress.advanced.intents != 0 {
		t.Fatalf("CreateReplyIntent calls = %d, want 0 with intent automation off", progress.advanced.intents)
	}
}

func TestParseSenderEmailReadsEveryStoredForm(t *testing.T) {
	for _, tc := range []struct {
		in   []string
		want string
	}{
		{[]string{"M K <MKMSC74@gmail.com>"}, "mkmsc74@gmail.com"},
		{[]string{"mkmsc74@gmail.com"}, "mkmsc74@gmail.com"},
		{[]string{"M K (mkmsc74@gmail.com)"}, "mkmsc74@gmail.com"},
		{[]string{"", "x@y.test"}, ""},
		{nil, ""},
	} {
		if got := parseSenderEmail(tc.in); got != tc.want {
			t.Errorf("parseSenderEmail(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// The self-hoster's exact case: an IONOS mailbox over IMAP, whose sync stored
// every address as "Name (addr)". The From matched the contact and the To
// named the mailbox, and both checks still failed because neither could read
// that form, so no reply into an IMAP mailbox counted for a week.
func TestProcessIncomingReplyCountsARepliedStoredInTheIMAPForm(t *testing.T) {
	orgID, accountID, contactID := uuid.New(), uuid.New(), uuid.New()
	account := &models.Email{ID: accountID, OrganizationID: &orgID, Email: "kannan@eml.example.test"}
	service, progress := newIncomingReplyService(account, &models.Contact{
		ID: contactID, Email: "mkmsc74@gmail.com",
	}, contactID)

	xerr := service.ProcessIncomingReply(context.Background(), accountID, &models.EmailMessageStoreData{
		ID:        uuid.New(),
		EmailID:   accountID,
		Folder:    models.FolderInbox,
		FromAddr:  []string{"M K (mkmsc74@gmail.com)"},
		ToAddr:    []string{"M Kannan (kannan@eml.example.test)"},
		InReplyTo: []string{"<opener@example.test>"},
		Subject:   "Re: Hello",
		Snippet:   "Recd it thank you..",
	})
	if xerr != nil {
		t.Fatal(xerr)
	}
	if progress.replied != 1 {
		t.Fatalf("RecordEmailReplied calls = %d, want 1 for a reply the IMAP sync stored", progress.replied)
	}
}

func TestProcessIncomingReplyRejectsMailboxOwnSentCopy(t *testing.T) {
	orgID, accountID, contactID := uuid.New(), uuid.New(), uuid.New()
	account := &models.Email{ID: accountID, OrganizationID: &orgID, Email: "sender@example.test"}
	service, progress := newIncomingReplyService(account, nil, contactID)

	xerr := service.ProcessIncomingReply(context.Background(), accountID, &models.EmailMessageStoreData{
		EmailID:   accountID,
		Folder:    models.FolderInbox,
		FromAddr:  []string{"Sender <sender@example.test>"},
		ToAddr:    []string{"recipient@example.test"},
		InReplyTo: []string{"<opener@example.test>"},
		Subject:   "Re: Hello",
	})
	if xerr != nil {
		t.Fatal(xerr)
	}
	if progress.replied != 0 {
		t.Fatalf("RecordEmailReplied calls = %d, want 0 for the mailbox's own outbound copy", progress.replied)
	}
}

func TestProcessIncomingReplyRejectsOutboundFolder(t *testing.T) {
	for _, tc := range []struct {
		name, folder, providerFolder string
	}{
		{name: "sent", folder: models.FolderSent},
		{name: "draft", folder: models.FolderDrafts},
		{name: "provider sent", folder: models.FolderInbox, providerFolder: models.FolderSent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			orgID, accountID, contactID := uuid.New(), uuid.New(), uuid.New()
			account := &models.Email{ID: accountID, OrganizationID: &orgID, Email: "sender@example.test"}
			service, progress := newIncomingReplyService(account, &models.Contact{
				ID: contactID, Email: "recipient@example.test",
			}, contactID)

			xerr := service.ProcessIncomingReply(context.Background(), accountID, &models.EmailMessageStoreData{
				EmailID:        accountID,
				Folder:         tc.folder,
				ProviderFolder: tc.providerFolder,
				FromAddr:       []string{"Recipient <recipient@example.test>"},
				ToAddr:         []string{"sender@example.test"},
				InReplyTo:      []string{"<opener@example.test>"},
				Subject:        "Re: Hello",
			})
			if xerr != nil {
				t.Fatal(xerr)
			}
			if progress.replied != 0 {
				t.Fatalf("RecordEmailReplied calls = %d, want 0 for outbound folder", progress.replied)
			}
		})
	}
}

func TestProcessIncomingReplyTrustsPersistedDirectionOverEventPayload(t *testing.T) {
	orgID, accountID, contactID := uuid.New(), uuid.New(), uuid.New()
	account := &models.Email{ID: accountID, OrganizationID: &orgID, Email: "sender@example.test"}
	service, progress := newIncomingReplyService(account, &models.Contact{
		ID: contactID, Email: "recipient@example.test",
	}, contactID)
	progress.sourceInbound = false

	xerr := service.ProcessIncomingReply(context.Background(), accountID, &models.EmailMessageStoreData{
		ID:        uuid.New(),
		EmailID:   accountID,
		Folder:    models.FolderInbox,
		FromAddr:  []string{"Recipient <recipient@example.test>"},
		ToAddr:    []string{"sender@example.test"},
		InReplyTo: []string{"<opener@example.test>"},
		Subject:   "Re: Hello",
	})
	if xerr != nil {
		t.Fatal(xerr)
	}
	if progress.replied != 0 {
		t.Fatalf("RecordEmailReplied calls = %d, want 0 when the stored source is outbound", progress.replied)
	}
}

func TestProcessIncomingReplyStopsWhenWriteBoundaryRejectsSource(t *testing.T) {
	orgID, accountID, contactID := uuid.New(), uuid.New(), uuid.New()
	account := &models.Email{ID: accountID, OrganizationID: &orgID, Email: "sender@example.test"}
	service, progress := newIncomingReplyService(account, &models.Contact{
		ID: contactID, Email: "recipient@example.test",
	}, contactID)
	progress.replyAccepted = false

	xerr := service.ProcessIncomingReply(context.Background(), accountID, &models.EmailMessageStoreData{
		ID:        uuid.New(),
		EmailID:   accountID,
		Folder:    models.FolderInbox,
		FromAddr:  []string{"Recipient <recipient@example.test>"},
		ToAddr:    []string{"sender@example.test"},
		InReplyTo: []string{"<opener@example.test>"},
		Subject:   "Re: Hello",
	})
	if xerr != nil {
		t.Fatal(xerr)
	}
	if progress.replied != 1 {
		t.Fatalf("RecordEmailReplied calls = %d, want 1 write-boundary claim", progress.replied)
	}
	if progress.advanced.marked != 0 {
		t.Fatalf("MarkVariantEvent calls = %d, want 0 after rejected claim", progress.advanced.marked)
	}
	if progress.advanced.intents != 0 {
		t.Fatalf("CreateReplyIntent calls = %d, want 0 after rejected claim", progress.advanced.intents)
	}
	if progress.completed != 1 {
		t.Fatalf("CompleteIncomingReply calls = %d, want 1 after rejected write", progress.completed)
	}
}

func TestProcessIncomingReplyClaimsBeforePersistingAutomatedState(t *testing.T) {
	orgID, accountID, contactID := uuid.New(), uuid.New(), uuid.New()
	account := &models.Email{ID: accountID, OrganizationID: &orgID, Email: "sender@example.test"}
	service, progress := newIncomingReplyService(account, &models.Contact{
		ID: contactID, Email: "recipient@example.test",
	}, contactID)
	progress.sourceClaimed = false

	xerr := service.ProcessIncomingReply(context.Background(), accountID, &models.EmailMessageStoreData{
		ID:        uuid.New(),
		EmailID:   accountID,
		Folder:    models.FolderInbox,
		FromAddr:  []string{"Recipient <recipient@example.test>"},
		ToAddr:    []string{"sender@example.test"},
		InReplyTo: []string{"<opener@example.test>"},
		Subject:   "Automatic reply: out of office",
		Snippet:   "I am out of the office.",
	})
	if xerr != nil {
		t.Fatal(xerr)
	}
	if progress.claims != 1 {
		t.Fatalf("ClaimIncomingReply calls = %d, want 1", progress.claims)
	}
	if progress.classified != 0 {
		t.Fatalf("RecordReplyClassification calls = %d, want 0 after rejected claim", progress.classified)
	}
	if progress.replied != 0 {
		t.Fatalf("RecordEmailReplied calls = %d, want 0 for rejected automated reply", progress.replied)
	}
	if progress.advanced.intents != 0 {
		t.Fatalf("CreateReplyIntent calls = %d, want 0 after rejected claim", progress.advanced.intents)
	}
	if progress.completed != 0 {
		t.Fatalf("CompleteIncomingReply calls = %d, want 0 without a claim", progress.completed)
	}
}

// Someone else answering in the lead's thread (a colleague, an assistant,
// another contact on the same list) is still an answer to the email we sent
// that lead; it is attributed to the thread, not to the sender's own latest
// campaign. #549 was the mailbox's OWN outbound copy, which the folder and
// sender checks above still refuse.
func TestProcessIncomingReplyCountsAThreadReplyFromAnotherContact(t *testing.T) {
	orgID, accountID := uuid.New(), uuid.New()
	taskContactID, senderContactID := uuid.New(), uuid.New()
	account := &models.Email{ID: accountID, OrganizationID: &orgID, Email: "sender@example.test"}
	service, progress := newIncomingReplyService(account, &models.Contact{
		ID: senderContactID, Email: "other@example.test",
	}, taskContactID)
	latestCampaignID, latestSequenceID := uuid.New(), uuid.New()
	progress.latest = &repository.CampaignSequencePair{CampaignID: latestCampaignID, SequenceID: latestSequenceID}

	xerr := service.ProcessIncomingReply(context.Background(), accountID, &models.EmailMessageStoreData{
		EmailID:   accountID,
		Folder:    models.FolderInbox,
		FromAddr:  []string{"Other person <other@example.test>"},
		ToAddr:    []string{"sender@example.test"},
		InReplyTo: []string{"<opener@example.test>"},
		Subject:   "Re: Hello",
	})
	if xerr != nil {
		t.Fatal(xerr)
	}
	if progress.replied != 1 {
		t.Fatalf("RecordEmailReplied calls = %d, want the thread's lead marked replied", progress.replied)
	}
}

func TestProcessIncomingReplyRequiresRecipientToMatchMailbox(t *testing.T) {
	orgID, accountID, contactID := uuid.New(), uuid.New(), uuid.New()
	account := &models.Email{ID: accountID, OrganizationID: &orgID, Email: "sender@example.test"}
	service, progress := newIncomingReplyService(account, &models.Contact{
		ID: contactID, Email: "recipient@example.test",
	}, contactID)

	xerr := service.ProcessIncomingReply(context.Background(), accountID, &models.EmailMessageStoreData{
		EmailID:   accountID,
		Folder:    models.FolderInbox,
		FromAddr:  []string{"Recipient <recipient@example.test>"},
		ToAddr:    []string{"someone-else@example.test"},
		InReplyTo: []string{"<opener@example.test>"},
		Subject:   "Re: Hello",
	})
	if xerr != nil {
		t.Fatal(xerr)
	}
	if progress.replied != 0 {
		t.Fatalf("RecordEmailReplied calls = %d, want 0 when the recipient is not the sending mailbox", progress.replied)
	}
}

func TestProcessIncomingReplyAcceptsCrossMailboxThreadWhenStoredSourceIsInbound(t *testing.T) {
	orgID, accountID, contactID := uuid.New(), uuid.New(), uuid.New()
	account := &models.Email{ID: accountID, OrganizationID: &orgID, Email: "sender@example.test"}
	service, progress := newIncomingReplyService(account, &models.Contact{
		ID: contactID, Email: "recipient@example.test",
	}, contactID)
	service.taskRepo.(incomingReplyTaskRepo).task.EmailAccountID = uuid.New()
	latestCampaignID, latestSequenceID := uuid.New(), uuid.New()
	progress.latest = &repository.CampaignSequencePair{CampaignID: latestCampaignID, SequenceID: latestSequenceID}

	xerr := service.ProcessIncomingReply(context.Background(), accountID, &models.EmailMessageStoreData{
		EmailID:   accountID,
		Folder:    models.FolderInbox,
		FromAddr:  []string{"Recipient <recipient@example.test>"},
		ToAddr:    []string{"sender@example.test"},
		InReplyTo: []string{"<opener@example.test>"},
		Subject:   "Re: Hello",
	})
	if xerr != nil {
		t.Fatal(xerr)
	}
	if progress.replied != 1 {
		t.Fatalf("RecordEmailReplied calls = %d, want 1 for an inbound cross-mailbox thread", progress.replied)
	}
	if progress.completed != 1 {
		t.Fatalf("CompleteIncomingReply calls = %d, want 1 for an inbound cross-mailbox thread", progress.completed)
	}
}

func TestProcessIncomingReplyAcceptsReplyAtTheSendsReplyToMailbox(t *testing.T) {
	orgID, accountID, contactID := uuid.New(), uuid.New(), uuid.New()
	account := &models.Email{ID: accountID, OrganizationID: &orgID, Email: "replies@example.test"}
	service, progress := newIncomingReplyService(account, &models.Contact{
		ID: contactID, Email: "recipient@example.test",
	}, contactID)
	task := service.taskRepo.(incomingReplyTaskRepo).task
	task.EmailAccountID = uuid.New()
	task.ReplyTo = "Replies@Example.test"
	// The reply inbox never wrote to the contact.
	progress.receivingSent = false
	events := &capturedEvents{}
	service.dispatcher = events
	agent := &capturedAgentDraft{}
	service.inboxAgent = agent

	xerr := service.ProcessIncomingReply(context.Background(), accountID, &models.EmailMessageStoreData{
		EmailID:   accountID,
		Folder:    models.FolderInbox,
		FromAddr:  []string{"Recipient <recipient@example.test>"},
		ToAddr:    []string{"replies@example.test"},
		InReplyTo: []string{"<opener@example.test>"},
		Subject:   "Re: Hello",
	})
	if xerr != nil {
		t.Fatal(xerr)
	}
	if progress.replied != 1 {
		t.Fatalf("RecordEmailReplied calls = %d, want 1 for a reply at the send's Reply-To mailbox", progress.replied)
	}
	reply := events.data[models.WebhookEventCampaignReplyReceived]
	if reply["sender_email_account_id"] != task.EmailAccountID.String() || reply["email_account_id"] != accountID.String() {
		t.Fatalf("reply event names sender %v and inbox %v; want %s and %s",
			reply["sender_email_account_id"], reply["email_account_id"], task.EmailAccountID, accountID)
	}
	// The suggested answer leaves from the mailbox the contact wrote to.
	if agent.from != task.EmailAccountID {
		t.Fatalf("agent draft from %s, want the sending mailbox %s", agent.from, task.EmailAccountID)
	}
}

type capturedAgentDraft struct{ from uuid.UUID }

func (c *capturedAgentDraft) DraftForReply(_ context.Context, r models.InboxAgentReply) {
	c.from = r.EmailAccountID
}

type capturedEvents struct {
	data map[models.WebhookEventType]map[string]any
}

func (c *capturedEvents) Dispatch(_ context.Context, _ uuid.UUID, eventType models.WebhookEventType, data any) (uuid.UUID, error) {
	if c.data == nil {
		c.data = map[models.WebhookEventType]map[string]any{}
	}
	c.data[eventType], _ = data.(map[string]any)
	return uuid.New(), nil
}

func TestProcessIncomingReplyRejectsReplyToNamingAnotherMailbox(t *testing.T) {
	orgID, accountID, contactID := uuid.New(), uuid.New(), uuid.New()
	account := &models.Email{ID: accountID, OrganizationID: &orgID, Email: "unrelated@example.test"}
	service, progress := newIncomingReplyService(account, &models.Contact{
		ID: contactID, Email: "recipient@example.test",
	}, contactID)
	task := service.taskRepo.(incomingReplyTaskRepo).task
	task.EmailAccountID = uuid.New()
	task.ReplyTo = "replies@example.test"
	progress.receivingSent = false

	xerr := service.ProcessIncomingReply(context.Background(), accountID, &models.EmailMessageStoreData{
		EmailID:   accountID,
		Folder:    models.FolderInbox,
		FromAddr:  []string{"Recipient <recipient@example.test>"},
		ToAddr:    []string{"unrelated@example.test"},
		InReplyTo: []string{"<opener@example.test>"},
		Subject:   "Re: Hello",
	})
	if xerr != nil {
		t.Fatal(xerr)
	}
	if progress.replied != 0 {
		t.Fatalf("RecordEmailReplied calls = %d, want 0 when the Reply-To named another mailbox", progress.replied)
	}
}

func TestProcessIncomingReplyRejectsCrossOrganizationThread(t *testing.T) {
	orgID, otherOrgID, accountID, contactID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	account := &models.Email{ID: accountID, OrganizationID: &orgID, Email: "sender@example.test"}
	service, progress := newIncomingReplyService(account, &models.Contact{
		ID: contactID, Email: "recipient@example.test",
	}, contactID)
	service.taskRepo.(incomingReplyTaskRepo).task.EmailAccountID = uuid.New()
	service.campaignRepo = incomingReplyCampaignRepo{campaign: &models.Campaign{
		ID:             uuid.New(),
		OrganizationID: &otherOrgID,
	}}

	xerr := service.ProcessIncomingReply(context.Background(), accountID, &models.EmailMessageStoreData{
		EmailID:   accountID,
		Folder:    models.FolderInbox,
		FromAddr:  []string{"Recipient <recipient@example.test>"},
		ToAddr:    []string{"sender@example.test"},
		InReplyTo: []string{"<opener@example.test>"},
		Subject:   "Re: Hello",
	})
	if xerr != nil {
		t.Fatal(xerr)
	}
	if progress.replied != 0 {
		t.Fatalf("RecordEmailReplied calls = %d, want 0 for a cross-organization thread", progress.replied)
	}
}

func TestProcessIncomingReplyRejectsCrossMailboxThreadAtUnrelatedWorkspaceMailbox(t *testing.T) {
	orgID, accountID, contactID := uuid.New(), uuid.New(), uuid.New()
	account := &models.Email{ID: accountID, OrganizationID: &orgID, Email: "unrelated@example.test"}
	service, progress := newIncomingReplyService(account, &models.Contact{
		ID: contactID, Email: "recipient@example.test",
	}, contactID)
	service.taskRepo.(incomingReplyTaskRepo).task.EmailAccountID = uuid.New()
	progress.receivingSent = false

	xerr := service.ProcessIncomingReply(context.Background(), accountID, &models.EmailMessageStoreData{
		EmailID:   accountID,
		Folder:    models.FolderInbox,
		FromAddr:  []string{"Recipient <recipient@example.test>"},
		ToAddr:    []string{"unrelated@example.test"},
		InReplyTo: []string{"<opener@example.test>"},
		Subject:   "Re: Hello",
	})
	if xerr != nil {
		t.Fatal(xerr)
	}
	if progress.replied != 0 {
		t.Fatalf("RecordEmailReplied calls = %d, want 0 for an unrelated workspace mailbox", progress.replied)
	}
}

func TestProcessIncomingReplyAcceptsMatchingThreadSender(t *testing.T) {
	orgID, accountID, contactID := uuid.New(), uuid.New(), uuid.New()
	account := &models.Email{ID: accountID, OrganizationID: &orgID, Email: "sender@example.test"}
	service, progress := newIncomingReplyService(account, &models.Contact{
		ID: contactID, Email: "recipient@example.test",
	}, contactID)

	xerr := service.ProcessIncomingReply(context.Background(), accountID, &models.EmailMessageStoreData{
		EmailID:   accountID,
		Folder:    models.FolderInbox,
		FromAddr:  []string{"Recipient <recipient@example.test>"},
		ToAddr:    []string{"sender@example.test"},
		InReplyTo: []string{"<opener@example.test>"},
		Subject:   "Re: Hello",
	})
	if xerr != nil {
		t.Fatal(xerr)
	}
	if progress.replied != 1 {
		t.Fatalf("RecordEmailReplied calls = %d, want 1 for the matching contact", progress.replied)
	}
	if progress.completed != 1 {
		t.Fatalf("CompleteIncomingReply calls = %d, want 1 for the matching contact", progress.completed)
	}
}

func TestProcessIncomingReplyFencesExpiredClaimBeforeSideEffects(t *testing.T) {
	orgID, accountID, contactID := uuid.New(), uuid.New(), uuid.New()
	account := &models.Email{ID: accountID, OrganizationID: &orgID, Email: "sender@example.test"}
	service, progress := newIncomingReplyService(account, &models.Contact{
		ID: contactID, Email: "recipient@example.test",
	}, contactID)
	progress.completeErr = errors.New("claim was replaced")

	xerr := service.ProcessIncomingReply(context.Background(), accountID, &models.EmailMessageStoreData{
		ID:        uuid.New(),
		EmailID:   accountID,
		Folder:    models.FolderInbox,
		FromAddr:  []string{"Recipient <recipient@example.test>"},
		ToAddr:    []string{"sender@example.test"},
		InReplyTo: []string{"<opener@example.test>"},
		Subject:   "Re: Hello",
	})
	if xerr == nil {
		t.Fatal("expected the replaced claim to stop processing")
	}
	if progress.completed != 1 {
		t.Fatalf("CompleteIncomingReply calls = %d, want 1", progress.completed)
	}
	if progress.advanced.marked != 0 {
		t.Fatalf("MarkVariantEvent calls = %d, want 0 after ownership was lost", progress.advanced.marked)
	}
	if progress.advanced.intents != 0 {
		t.Fatalf("CreateReplyIntent calls = %d, want 0 after ownership was lost", progress.advanced.intents)
	}
}

// A reply in one of the workspace's tagging languages is read with that
// language's declined and negative phrases; without the language it is not.
func TestProcessIncomingReplyReadsTheWorkspaceLanguages(t *testing.T) {
	for _, tc := range []struct {
		name      string
		languages []string
		want      string
	}{
		{"with German chosen", []string{"de"}, replyclassify.ClassNegative},
		{"without it", nil, replyclassify.ClassUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			orgID, accountID, contactID := uuid.New(), uuid.New(), uuid.New()
			account := &models.Email{ID: accountID, OrganizationID: &orgID, Email: "sender@example.test"}
			s, progress := newIncomingReplyService(account, &models.Contact{ID: contactID, Email: "recipient@example.test"}, contactID)
			progress.advanced.languages = tc.languages
			if err := s.ProcessIncomingReply(context.Background(), accountID, &models.EmailMessageStoreData{
				ID: uuid.New(), EmailID: accountID, MessageID: "<reply@example.test>", Folder: models.FolderInbox,
				FromAddr: []string{"recipient@example.test"}, ToAddr: []string{account.Email}, InReplyTo: []string{"<opener@example.test>"},
				Subject: "Re: Hallo", Snippet: "Danke, aber wir haben kein Interesse.",
			}); err != nil {
				t.Fatal(err)
			}
			if progress.class != tc.want {
				t.Errorf("reply class = %q, want %q", progress.class, tc.want)
			}
		})
	}
}
