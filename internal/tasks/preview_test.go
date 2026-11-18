package tasks

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

func TestPreviewEmailUsesSelectedSender(t *testing.T) {
	service := &tasksService{}
	for _, account := range []*models.Email{
		{Name: "Tareque M.", Email: "tareque@example.com", SendAsEmail: "hello@example.com"},
		{Name: "John S.", Email: "john@example.com"},
	} {
		preview := service.PreviewEmail(context.Background(), uuid.Nil, EmailPreviewInput{
			Subject: "From {{.Sender.Name}}", BodyHTML: "<p>{{.FirstName}}: {{.Sender.Email}}</p>",
			BodyPlain: "{{.Sender.Provider}}|{{.Sender.Email}}", Contact: models.Contact{FirstName: "Alex"}, Account: account,
		})
		if preview.From == nil || preview.From.Email != account.SendFrom() || preview.Subject != "From "+account.Name || preview.BodyHTML != "<p>Alex: "+account.SendFrom()+"</p>" || preview.BodyPlain != "|"+account.SendFrom() {
			t.Fatalf("preview does not match selected sender: %+v", preview)
		}
		if len(preview.Errors) != 0 || len(preview.Unresolved) != 0 {
			t.Fatalf("sender variables left errors: %+v", preview)
		}
	}
	preview := service.PreviewEmail(context.Background(), uuid.Nil, EmailPreviewInput{Subject: "{{.Sender.Email}}"})
	if preview.Subject != "" || preview.From != nil {
		t.Fatalf("preview without a mailbox invented a sender: %+v", preview)
	}
}

// finishBody is what the preview and the test send share with the campaign
// send: the parts land in send order (body, signature, opt-out) and a
// plain-text campaign loses its HTML part before the signature is added.
func TestFinishBodyMatchesSendOrder(t *testing.T) {
	account := &models.Email{SignatureSync: true, SignatureHTML: "<p>Ana</p>", SignaturePlain: "Ana"}
	optOut := &models.UnsubscribeSettings{Mode: models.UnsubscribeModeText, Text: "Reply stop to opt out."}

	h, p := finishBody("<p>Hi</p>", "", false, account, optOut, "")
	sig, foot := strings.Index(h, "<p>Ana</p>"), strings.Index(h, "Reply stop to opt out.")
	if sig < 0 || foot < 0 || sig > foot {
		t.Fatalf("html parts out of send order: %q", h)
	}
	if !strings.HasPrefix(p, "Hi") || strings.Index(p, "Ana") > strings.Index(p, "Reply stop") {
		t.Fatalf("plain part not derived from html or out of order: %q", p)
	}

	h, p = finishBody("<p>Hi</p>", "", true, account, optOut, "")
	if h != "" {
		t.Fatalf("plain-text campaign kept an html part: %q", h)
	}
	if !strings.Contains(p, "Ana") || !strings.Contains(p, "Reply stop") {
		t.Fatalf("plain-text campaign lost signature or footer: %q", p)
	}

	// No mailbox and no campaign: templates only, nothing appended.
	h, p = finishBody("<p>Hi</p>", "Hi", false, nil, nil, "")
	if h != "<p>Hi</p>" || p != "Hi" {
		t.Fatalf("bare preview was decorated: %q / %q", h, p)
	}

	// Signature sync off leaves the body alone even with a signature stored.
	off := &models.Email{SignatureSync: false, SignatureHTML: "<p>Ana</p>"}
	if h, _ = finishBody("<p>Hi</p>", "Hi", false, off, nil, ""); h != "<p>Hi</p>" {
		t.Fatalf("signature added while sync is off: %q", h)
	}
}

func TestPreviewTemplatesWithUsesTheGivenLink(t *testing.T) {
	p := previewTemplatesWith("s", "<a href=\"{{.UnsubscribeLink}}\">x</a>", "", models.Contact{}, TemplateContext{UnsubscribeLink: "https://api.example.com/unsubscribe/tok"})
	if !strings.Contains(p.BodyHTML, "https://api.example.com/unsubscribe/tok") {
		t.Fatalf("link variable did not resolve to the given link: %q", p.BodyHTML)
	}
	if q := PreviewTemplates("s", "{{.UnsubscribeLink}}", "", models.Contact{}); q.BodyHTML != PreviewUnsubscribeLink {
		t.Fatalf("default preview link changed: %q", q.BodyHTML)
	}
}

// A step created through the API or an agent tool carries the composer's empty
// <div></div> under body_html. It is not an empty string, so it shipped as the
// text/html alternative and the recipient opened a blank message with the real
// copy only in the fallback part.
func TestFinishBodyDropsATextlessHTMLPart(t *testing.T) {
	h, p := finishBody("<div></div>", "Hi Ana, quick question.", false, nil, nil, "")
	if h != "" {
		t.Errorf("HTML part = %q, want it dropped so the plain part is what ships", h)
	}
	if p != "Hi Ana, quick question." {
		t.Errorf("plain part = %q, want the step's own text", p)
	}
}

// The guard must not touch a body that renders something, including one whose
// only content is an image.
func TestFinishBodyKeepsABodyThatRenders(t *testing.T) {
	if h, _ := finishBody("<div>Hi</div>", "Hi", false, nil, nil, ""); h != "<div>Hi</div>" {
		t.Errorf("HTML part = %q, want it kept", h)
	}
	img := `<div><img src="https://example.com/a.png"></div>`
	if h, _ := finishBody(img, "see the image", false, nil, nil, ""); h == "" {
		t.Error("an image-only body was dropped; it renders for the recipient")
	}
}

// With no plain part either there is nothing to fall back to, so dropping the
// HTML would send an empty message. The start-time guard is what stops that
// campaign existing; here the body is left alone.
func TestFinishBodyKeepsAnEmptyHTMLPartWhenThereIsNoPlainText(t *testing.T) {
	if h, _ := finishBody("<div></div>", "", false, nil, nil, ""); h == "" {
		t.Error("dropped the only part the message had")
	}
}

type previewReviewRepo struct {
	repository.CampaignReviewRepository
	lead, own bool
}

func (r previewReviewRepo) GetLead(context.Context, uuid.UUID, uuid.UUID) (*models.CampaignReviewLead, error) {
	if !r.lead {
		return nil, repository.ErrLeadNotInCampaign
	}
	return &models.CampaignReviewLead{}, nil
}

func (r previewReviewRepo) GetOverride(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (*models.LeadStepOverride, error) {
	if !r.own {
		return nil, nil
	}
	return &models.LeadStepOverride{}, nil
}

type previewProgressRepo struct {
	repository.CampaignProgressRepository
	campaign, contact, step uuid.UUID
	written                 map[string]string
}

func (r previewProgressRepo) GetResolvedAIVariables(_ context.Context, campaignID, contactID, stepID uuid.UUID) (map[string]string, error) {
	if campaignID != r.campaign || contactID != r.contact || stepID != r.step {
		return map[string]string{}, nil
	}
	return r.written, nil
}

// The step preview never shows an AI block's raw token or config: a lead gets
// the copy already written for them, anyone else a labelled slot.
func TestPreviewEmailShowsAICopyForTheLead(t *testing.T) {
	cfg := "eyJuYW1lIjoiT3BlbmVyIiwicHJvbXB0IjoiaGkifQ==" // {"name":"Opener","prompt":"hi"}
	body := `<p>Hi</p><p><span data-ai-var="opener" data-ai-config="` + cfg + `">[[ai:opener]]</span></p>`
	campaign := &models.Campaign{ID: uuid.New()}
	step := uuid.New()
	render := func(s *tasksService, contact models.Contact) *EmailPreview {
		return s.PreviewEmail(context.Background(), uuid.Nil, EmailPreviewInput{
			Subject: "x", BodyHTML: body, BodyPlain: "Hi\n\n[[ai:opener]]", Contact: contact, Campaign: campaign, SequenceID: step,
		})
	}
	lead := models.Contact{ID: uuid.New(), FirstName: "Jonas"}
	written := previewProgressRepo{campaign: campaign.ID, contact: lead.ID, step: step, written: map[string]string{"opener": "Loved the Acme launch."}}

	p := render(&tasksService{leadReview: previewReviewRepo{}, campaignProgressRepo: written}, models.Contact{FirstName: "Alex"})
	if strings.Contains(p.BodyHTML, "[[ai:") || strings.Contains(p.BodyHTML, "data-ai-config") || !strings.Contains(p.BodyHTML, `data-ai-pending="opener" data-ai-name="Opener"`) {
		t.Fatalf("sample preview should show a labelled AI slot: %q", p.BodyHTML)
	}
	if p.Lead || len(p.AIBlocks) != 1 || p.AIBlocks[0].Written || strings.Contains(p.BodyPlain, "[[ai:") {
		t.Fatalf("sample preview: %+v", p)
	}

	p = render(&tasksService{leadReview: previewReviewRepo{lead: true, own: true}, campaignProgressRepo: written}, lead)
	if !strings.Contains(p.BodyHTML, `<span data-ai-block="opener">Loved the Acme launch.</span>`) || !strings.Contains(p.BodyPlain, "Loved the Acme launch.") {
		t.Fatalf("lead preview should carry their written AI copy: %q / %q", p.BodyHTML, p.BodyPlain)
	}
	if !p.Lead || !p.Personalized || len(p.AIBlocks) != 1 || !p.AIBlocks[0].Written {
		t.Fatalf("lead preview flags: %+v", p)
	}

	// A contact outside the campaign reads nothing the campaign wrote.
	p = render(&tasksService{leadReview: previewReviewRepo{}, campaignProgressRepo: written}, lead)
	if p.Lead || strings.Contains(p.BodyHTML, "Loved the Acme launch.") || strings.Contains(p.BodyPlain, "Loved the Acme launch.") {
		t.Fatalf("non-lead preview read campaign copy: %+v", p)
	}
}
