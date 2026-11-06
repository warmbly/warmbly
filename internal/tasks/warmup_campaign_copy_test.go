package tasks

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
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
		got := campaignCopyIneligible(tc.subject, tc.html, tc.plain)
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

func TestOwnCopyDrawNeedsAMatureSenderAndAnOutOfInboxBusinessRecipient(t *testing.T) {
	org := uuid.New()
	now := time.Now()
	mature := now.AddDate(0, 0, -models.WarmupOwnCopyMinDays)
	young := now.AddDate(0, 0, -models.WarmupOwnCopyMinDays+1)
	sender := Email{OrganizationID: &org, Warmup: &mature}
	at := func(domain string) string { return "priya" + "@" + domain }
	folder := &Email{Email: at("northridge.example"), MailHost: "google_workspace"}
	for _, business := range []*Email{folder, {Email: at("northridge.example"), Provider: "outlook"}, {Email: at("northridge.example"), Provider: "gmail"}} {
		if !ownCopyDraw(sender, business, now, 0) {
			t.Fatalf("own copy skipped the business inbox %s", business.Email)
		}
	}
	for _, elsewhere := range []*Email{
		{Email: at("gmail.com"), Provider: "gmail"},
		{Email: at("outlook.com"), MailHost: "outlook", Provider: "outlook"},
		{Email: at("yahoo.com"), MailHost: "yahoo"},
		{Email: at("smallhost.example"), Provider: "smtp_imap"},
	} {
		if ownCopyDraw(sender, elsewhere, now, 0) {
			t.Fatalf("own copy went to %s, which is not a business inbox on Google Workspace or Microsoft 365", elsewhere.Email)
		}
	}
	if !ownCopyDraw(sender, folder, now, models.WarmupOwnCopySharePercent-1) {
		t.Fatal("a roll under the share did not draw own copy")
	}
	if ownCopyDraw(sender, folder, now, models.WarmupOwnCopySharePercent) {
		t.Fatal("a roll at the share drew own copy")
	}
	if ownCopyDraw(Email{OrganizationID: &org, Warmup: &young}, folder, now, 0) {
		t.Fatalf("a mailbox under %d days of warmup carried its own copy", models.WarmupOwnCopyMinDays)
	}
	if ownCopyDraw(sender, &Email{Email: folder.Email, MailHost: folder.MailHost, WarmupPlacement: models.WarmupPlacementInbox}, now, 0) {
		t.Fatal("own copy went to a recipient that reads warmup in its inbox")
	}
	if ownCopyDraw(Email{Warmup: &mature}, folder, now, 0) || ownCopyDraw(sender, nil, now, 0) {
		t.Fatal("own copy drawn with no workspace or no recipient")
	}
}

func TestCampaignCopyRendersTheStepForTheRecipient(t *testing.T) {
	org, campaignID, opener, other := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	warmedSince := time.Now().AddDate(0, 0, -30)
	repo := &qualityContentRepo{
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

	content, ok, err := s.campaignCopyContent(t.Context(), account, recipient, uuid.New())
	if err != nil || !ok {
		t.Fatalf("no campaign copy: %v", err)
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
	if content.maxTurns != models.WarmupOwnCopyTurns || content.conversationID != nil {
		t.Fatal("campaign copy opens a reply thread with no bank conversation behind it")
	}

	// A recipient on a consumer domain cannot fill the company, so this thread opens with ordinary content.
	gmail := &Email{ID: uuid.New(), Name: "Sam Lee", Email: addr("sam", "gmail.com")}
	if content, ok, err = s.campaignCopyContent(t.Context(), account, gmail, uuid.New()); err != nil || ok {
		t.Fatalf("copy naming a company went to a recipient with none: %q %v", content.subject, err)
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
		campaignCopy: []models.WarmupCampaignCopyCandidate{{CampaignID: campaignID, SequenceID: step, Subject: "Plans for Q4?",
			BodyHTML: `<p>Hi {{.FirstName}}, here is the deck: <a href="https://acme.example/deck">deck</a></p>`}},
	}
	campaign := &models.Campaign{ID: campaignID, OrganizationID: &org, OpenTracking: true, LinkTracking: true, UnsubscribeMode: "off",
		TrackingDomain: "t.acme.example", TrackingDomainVerified: true}
	tickets := &copyTickets{}
	s := &tasksService{warmupContentRepo: repo, campaignRepo: &copyCampaigns{byID: map[uuid.UUID]*models.Campaign{campaignID: campaign}}, trackedLinkRepo: tickets}
	account := Email{ID: uuid.New(), OrganizationID: &org, Name: "Ada Byron", Email: addr("ada", "acme.example"), Warmup: &warmedSince}

	content, ok, err := s.campaignCopyContent(t.Context(), account, &Email{ID: uuid.New(), Name: "Jane Doe", Email: addr("jane", "northridge.com")}, taskID)
	if err != nil || !ok {
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

// A campaign copy thread passes the dispatch-time check on every turn it plans, with no conversation behind it.
func TestExecutableWarmupSourceAdmitsCampaignCopyThreads(t *testing.T) {
	s := &tasksService{}
	turn := func(n int, conv *uuid.UUID) *repository.WarmupReplyCandidate {
		return &repository.WarmupReplyCandidate{ContentSource: models.WarmupContentSourceCampaign, ConversationID: conv, ConversationTurn: n, Subject: "Plans for Q4?", MaxTurns: models.WarmupOwnCopyTurns}
	}
	for n := range models.WarmupOwnCopyTurns {
		if err := s.executableWarmupSource(t.Context(), turn(n, nil), &models.Email{}); err != nil {
			t.Fatalf("turn %d of a campaign copy thread was refused: %v", n, err)
		}
	}
	if err := s.executableWarmupSource(t.Context(), turn(models.WarmupOwnCopyTurns, nil), &models.Email{}); err == nil {
		t.Fatal("a turn past the thread's length was admitted")
	}
	id := uuid.New()
	if err := s.executableWarmupSource(t.Context(), turn(0, &id), &models.Email{}); err == nil {
		t.Fatal("campaign copy naming a bank conversation was admitted")
	}
}

// Every prospect line reads as ordinary mail and passes the checks every warmup turn is held to.
func TestProspectExchangesPassTheWarmupChecks(t *testing.T) {
	seen := map[string]bool{}
	sender := Email{Name: "Jane Doe"}
	for i, ex := range prospectExchanges {
		if len(ex.replies) < 4 || len(ex.answers) < 4 {
			t.Fatalf("exchange %d is too thin to keep threads apart", i)
		}
		for _, line := range append(append([]string{}, ex.replies...), ex.answers...) {
			if seen[line] {
				t.Fatalf("line repeated: %q", line)
			}
			seen[line] = true
			if strings.ContainsAny(line, "<>{}\r—") {
				t.Fatalf("line carries markup, a merge field or an em dash: %q", line)
			}
			body, err := generation.RenderCanonicalTurn(line, sender.Name)
			if err != nil {
				t.Fatal(err)
			}
			if err := lintDiagnosticContent("Re: Plans for Q4?", body, true, sender); err != nil {
				t.Fatalf("%q: %v", line, err)
			}
		}
	}
}

// A thread's reply and answer come from one exchange, picked from the opening,
// and the same turn always writes the same words.
func TestCampaignReplyContinuesTheThreadAsAProspect(t *testing.T) {
	opening := &repository.WarmupReplyCandidate{ContentSource: models.WarmupContentSourceCampaign, MessageID: "<" + addr("a1", "acme.example") + ">",
		Subject: "Plans for Q4?", MaxTurns: models.WarmupOwnCopyTurns, ScenarioVersion: generation.WarmupScenarioVersion, RenderingVersion: generation.CanonicalRenderingVersion}
	reply, inReplyTo, refs, err := campaignReplyContent(opening, Email{Name: "Jane Doe"})
	if err != nil {
		t.Fatal(err)
	}
	if inReplyTo != opening.MessageID || len(refs) != 1 || reply.maxTurns != models.WarmupOwnCopyTurns || reply.subject != opening.Subject || !strings.HasSuffix(reply.body, "Jane Doe") {
		t.Fatalf("reply does not continue the thread: %+v %q %v", reply, inReplyTo, refs)
	}
	again, _, _, _ := campaignReplyContent(opening, Email{Name: "Jane Doe"})
	if again.body != reply.body {
		t.Fatal("a retried turn wrote different words")
	}

	answerTurn := &repository.WarmupReplyCandidate{ContentSource: models.WarmupContentSourceCampaign, MessageID: addr("b2", "northridge.example"), References: refs, ConversationTurn: 1,
		Subject: opening.Subject, MaxTurns: models.WarmupOwnCopyTurns, ScenarioVersion: generation.WarmupScenarioVersion, RenderingVersion: generation.CanonicalRenderingVersion}
	answer, _, _, err := campaignReplyContent(answerTurn, Email{Name: "Ada Byron"})
	if err != nil {
		t.Fatal(err)
	}
	line := strings.TrimSuffix(strings.TrimSuffix(answer.body, "Ada Byron"), "\n")
	replyLine := strings.TrimSuffix(strings.TrimSuffix(reply.body, "Jane Doe"), "\n")
	matched := false
	for _, ex := range prospectExchanges {
		if slices.Contains(ex.replies, strings.TrimSpace(replyLine)) && slices.Contains(ex.answers, strings.TrimSpace(line)) {
			matched = true
		}
	}
	if !matched {
		t.Fatalf("the answer does not belong to the reply's exchange: %q / %q", replyLine, line)
	}

	answerTurn.ConversationTurn = 2
	if _, _, _, err := campaignReplyContent(answerTurn, Email{Name: "Ada Byron"}); err == nil {
		t.Fatal("a thread ran past its planned length")
	}
}

// Threads drawn from different openings spread across the exchanges.
func TestProspectRepliesVaryAcrossThreads(t *testing.T) {
	lines := map[string]bool{}
	for i := range 200 {
		line, ok := prospectLine(addr(uuid.NewString(), "acme.example"), 1)
		if !ok {
			t.Fatalf("thread %d got no reply", i)
		}
		lines[line] = true
	}
	if len(lines) < 20 {
		t.Fatalf("200 threads used only %d distinct replies", len(lines))
	}
}
