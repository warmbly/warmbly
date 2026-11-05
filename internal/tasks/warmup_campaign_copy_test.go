package tasks

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type copyCampaigns struct {
	repository.CampaignRepository
	byID map[uuid.UUID]*models.Campaign
}

func (r *copyCampaigns) GetByID(_ context.Context, id uuid.UUID) (*models.Campaign, error) {
	return r.byID[id], nil
}

func TestCampaignCopyIneligibleNamesWhatWarmupCannotCarry(t *testing.T) {
	cases := []struct{ subject, html, plain, want string }{
		{"Quick question", "", "Hi {{.FirstName}}, are you hiring at {{.Company}} this quarter?", ""},
		{"Quick question", "<p>Hi {{first_name}}, from {{.Sender.FirstName}}</p>", "", ""},
		{"", "", "Hi there", "no subject"},
		{"Quick question", "", "   ", "no body"},
		{"Quick question", "", "Saw [[ai:icebreaker]] and wanted to ask", "AI blocks"},
		{"Quick question", "", "Book a slot: {{form_link:abc123}}", "links a form"},
		{"Quick question", "", "Loved your work in {{.industry}}", "custom fields"},
		{"Quick question", "", `{{index . "city"}} is lovely`, "custom fields"},
		{"Quick question", "", "Can I call you on {{.Phone}}?", "phone"},
		{"Quick question", "", "Hi {{frist_name}}", "do not compile"},
		{"Quick question", "", "This is an automated test message about our tool.", "automated"},
		{"FREE CASH", "", "Act now for a guaranteed bonus and free cash prize.", "spam"},
	}
	for _, tc := range cases {
		got := CampaignCopyIneligible(tc.subject, tc.html, tc.plain)
		if tc.want == "" && got != "" {
			t.Errorf("%q: refused (%s), want eligible", tc.plain, got)
		}
		if tc.want != "" && !strings.Contains(got, tc.want) {
			t.Errorf("%q: got %q, want a reason mentioning %q", tc.plain, got, tc.want)
		}
	}
}

func TestCompanyFromAddressNamesBusinessDomainsOnly(t *testing.T) {
	cases := map[string]string{
		addr("ops", "north-ridge.com"):   "North Ridge",
		addr("sales", "mail.acme.co.uk"): "Acme",
		addr("jane", "gmail.com"):        "",
		addr("jane", "outlook.com"):      "",
		addr("info", "xn--bcher-kva.de"): "",
		"not-an-address":                 "",
	}
	for in, want := range cases {
		if got := companyFromAddress(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}

func TestCopyFillableSkipsRecipientsMissingAField(t *testing.T) {
	jane := models.Contact{FirstName: "Jane", Email: addr("jane", "gmail.com")}
	if !copyFillable(jane, "Hi {{.FirstName}}") {
		t.Fatal("a named recipient must fill a first-name merge field")
	}
	if copyFillable(jane, "Things at {{.Company}}") {
		t.Fatal("a recipient with no company must not get copy that names one")
	}
	if !copyFillable(jane, "{{.Sender.FirstName}} here, no lead fields") {
		t.Fatal("sender fields are always filled")
	}
}

func TestOwnCopyWaitsForAMatureMailbox(t *testing.T) {
	org := uuid.New()
	fresh := time.Now().AddDate(0, 0, -1)
	repo := &qualityContentRepo{templateSettings: models.WarmupTemplateSettings{TemplatesOnly: true, CampaignCopy: true}}
	s := &tasksService{warmupContentRepo: repo}
	account := Email{ID: uuid.New(), OrganizationID: &org, Name: "Ada", Warmup: &fresh}
	content, err := s.pickNewWarmupContent(t.Context(), account, &Email{ID: uuid.New()}, uuid.New())
	if err != nil {
		t.Fatalf("a new mailbox must still warm on ordinary conversations: %v", err)
	}
	if content.contentSource == models.WarmupContentSourceTemplate || content.contentSource == models.WarmupContentSourceCampaign {
		t.Fatalf("a %d-day-old mailbox carried its workspace's own copy", 1)
	}
	for _, p := range repo.picks {
		if p.TemplatesOnly {
			t.Fatal("a new mailbox drew from the workspace's templates")
		}
	}
}

func TestCampaignCopyRendersTheStepForTheRecipient(t *testing.T) {
	org, campaignID, opener, other := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	warmedSince := time.Now().AddDate(0, 0, -30)
	repo := &qualityContentRepo{
		templateSettings: models.WarmupTemplateSettings{TemplateShare: 100, CampaignCopy: true},
		campaignCopy: []models.WarmupCampaignCopyCandidate{
			// Least recently carried, but warmup has no value for the custom field.
			{CampaignID: campaignID, SequenceID: other, Subject: "About {{.industry}}", BodyPlain: "Hi {{.FirstName}}, how is {{.industry}}?"},
			{CampaignID: campaignID, SequenceID: opener, Subject: "Hiring at {{.Company}}?", BodyPlain: "Hi {{.FirstName}},\n\nAre you growing the team at {{.Company}} this quarter?\n\n{{.Sender.FirstName}}"},
		},
	}
	campaigns := &copyCampaigns{byID: map[uuid.UUID]*models.Campaign{campaignID: {ID: campaignID, OrganizationID: &org, TextOnly: true, UnsubscribeMode: "off"}}}
	s := &tasksService{warmupContentRepo: repo, campaignRepo: campaigns}
	account := Email{ID: uuid.New(), OrganizationID: &org, Name: "Ada Byron", Email: addr("ada", "byron.dev"), Warmup: &warmedSince}
	recipient := &Email{ID: uuid.New(), Name: "Jane Doe", Email: addr("jane", "northridge.com")}

	content, err := s.pickNewWarmupContent(t.Context(), account, recipient, uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if content.contentSource != models.WarmupContentSourceCampaign || content.sequenceID == nil || *content.sequenceID != opener {
		t.Fatalf("want the eligible opener as campaign copy, got source %q step %v", content.contentSource, content.sequenceID)
	}
	if content.subject != "Hiring at Northridge?" || !strings.HasPrefix(content.body, "Hi Jane,") || !strings.Contains(content.body, "\n\nAda") {
		t.Fatalf("copy not rendered for the recipient: %q / %q", content.subject, content.body)
	}
	if content.bodyHTML != "" {
		t.Fatalf("a text-only campaign sent an HTML part: %q", content.bodyHTML)
	}
	if content.maxTurns != 1 || content.conversationID != nil {
		t.Fatal("campaign copy must be a single opening with no bank conversation behind it")
	}
	if len(repo.picks) != 0 {
		t.Fatalf("campaign copy was available, yet a template or bank draw ran: %+v", repo.picks)
	}

	// A recipient on a consumer domain cannot fill the company, so warmup falls back to templates, then the bank.
	repo.picks = nil
	gmail := &Email{ID: uuid.New(), Name: "Sam Lee", Email: addr("sam", "gmail.com")}
	content, err = s.pickNewWarmupContent(t.Context(), account, gmail, uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if content.contentSource == models.WarmupContentSourceCampaign {
		t.Fatalf("copy naming a company went to a recipient with none: %q", content.subject)
	}
	if len(repo.picks) != 2 || !repo.picks[0].TemplatesOnly {
		t.Fatalf("want a template draw then a bank draw, got %+v", repo.picks)
	}
}

// addr joins an address from its parts.
func addr(local, domain string) string { return local + "@" + domain }

type copyTickets struct {
	repository.TrackedLinkRepository
	created []repository.TrackedLink
}

func (r *copyTickets) CreateBatch(_ context.Context, links []repository.TrackedLink) error {
	r.created = append(r.created, links...)
	return nil
}

// Campaign copy carries the campaign's open pixel and wrapped links on its
// tracking domain, with tickets that name no campaign.
func TestCampaignCopyKeepsTheCampaignsTrackingDomain(t *testing.T) {
	org, campaignID, step, taskID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	warmedSince := time.Now().AddDate(0, 0, -30)
	repo := &qualityContentRepo{
		templateSettings: models.WarmupTemplateSettings{TemplateShare: 100, CampaignCopy: true},
		campaignCopy: []models.WarmupCampaignCopyCandidate{{CampaignID: campaignID, SequenceID: step, Subject: "Plans for Q4?",
			BodyHTML: `<p>Hi {{.FirstName}}, here is the deck: <a href="https://acme.example/deck">deck</a></p>`}},
	}
	campaign := &models.Campaign{ID: campaignID, OrganizationID: &org, OpenTracking: true, LinkTracking: true, UnsubscribeMode: "off",
		TrackingDomain: "t.acme.example", TrackingDomainVerified: true}
	tickets := &copyTickets{}
	s := &tasksService{warmupContentRepo: repo, campaignRepo: &copyCampaigns{byID: map[uuid.UUID]*models.Campaign{campaignID: campaign}}, trackedLinkRepo: tickets}
	account := Email{ID: uuid.New(), OrganizationID: &org, Name: "Ada Byron", Email: addr("ada", "acme.example"), Warmup: &warmedSince}

	content, err := s.pickNewWarmupContent(t.Context(), account, &Email{ID: uuid.New(), Name: "Jane Doe", Email: addr("jane", "northridge.com")}, taskID)
	if err != nil || content.contentSource != models.WarmupContentSourceCampaign {
		t.Fatalf("no campaign copy: %+v %v", content, err)
	}
	if !strings.Contains(content.bodyHTML, "https://t.acme.example/c/") || strings.Contains(content.bodyHTML, `href="https://acme.example/deck"`) {
		t.Fatalf("links not wrapped on the campaign's tracking domain: %s", content.bodyHTML)
	}
	if !strings.Contains(content.bodyHTML, "t.acme.example") || content.tracking == nil || !content.tracking.OpenTracking {
		t.Fatalf("open pixel missing: %s %+v", content.bodyHTML, content.tracking)
	}
	if len(tickets.created) != 1 || tickets.created[0].TaskID != taskID || tickets.created[0].CampaignID != uuid.Nil {
		t.Fatalf("tickets must belong to the warmup task and no campaign: %+v", tickets.created)
	}
}

func TestRecipientNameReachesCopyOnlyWhenSafe(t *testing.T) {
	if c := warmupRecipientContact(&Email{Name: "Jane Doe", Email: addr("jane", "northridge.com")}); c.FirstName != "Jane" || c.LastName != "Doe" {
		t.Fatalf("a plain name was dropped: %+v", c)
	}
	if c := warmupRecipientContact(&Email{Name: "Visit evil.example now", Email: addr("x", "gmail.com")}); c.FirstName != "" || c.LastName != "" {
		t.Fatalf("a name carrying a hostname reached the copy: %+v", c)
	}
}

type ownCopyRows struct {
	repository.WarmupContentRepository
	row *models.WarmupConversation
}

func (r *ownCopyRows) GetConversation(_ context.Context, id uuid.UUID) (*models.WarmupConversation, error) {
	if r.row != nil && r.row.ID == id {
		return r.row, nil
	}
	return nil, nil
}

// Own copy passes the dispatch-time check as a single opening: a template
// while it is active and the sender's, campaign copy with no conversation.
func TestExecutableWarmupSourceAdmitsOwnCopy(t *testing.T) {
	org, other := uuid.New(), uuid.New()
	tmpl := &models.WarmupConversation{ID: uuid.New(), Source: models.WarmupContentSourceTemplate, Status: models.WarmupConversationActive, OrganizationID: &org, Subject: "Plans for Q4?"}
	s := &tasksService{warmupContentRepo: &ownCopyRows{row: tmpl}}
	account := &models.Email{OrganizationID: &org}
	id := tmpl.ID
	opening := func(source string, conv *uuid.UUID) *repository.WarmupReplyCandidate {
		return &repository.WarmupReplyCandidate{ContentSource: source, ConversationID: conv, Subject: "Plans for Q4?", MaxTurns: 1}
	}

	if err := s.executableWarmupSource(t.Context(), opening(models.WarmupContentSourceTemplate, &id), account); err != nil {
		t.Fatalf("an active template of the sender's was refused: %v", err)
	}
	if err := s.executableWarmupSource(t.Context(), opening(models.WarmupContentSourceCampaign, nil), account); err != nil {
		t.Fatalf("campaign copy was refused: %v", err)
	}
	if err := s.executableWarmupSource(t.Context(), opening(models.WarmupContentSourceTemplate, &id), &models.Email{OrganizationID: &other}); err == nil {
		t.Fatal("another workspace's template was admitted")
	}
	reply := opening(models.WarmupContentSourceCampaign, nil)
	reply.ConversationTurn = 1
	if err := s.executableWarmupSource(t.Context(), reply, account); err == nil {
		t.Fatal("a reply turn on own copy was admitted")
	}
	tmpl.Status = models.WarmupConversationArchived
	if err := s.executableWarmupSource(t.Context(), opening(models.WarmupContentSourceTemplate, &id), account); err == nil {
		t.Fatal("a retired template was admitted")
	}
}
