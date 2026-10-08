// Package form owns hosted lead-capture forms: the builder CRUD the
// dashboard edits, and the public render/submit pipeline that turns a
// visitor's answers into a contact, category links and a campaign lead.
package form

import (
	"context"
	"crypto/rand"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/geo"
	"github.com/warmbly/warmbly/internal/pkg/typesafe"
	"github.com/warmbly/warmbly/internal/repository"
	"github.com/warmbly/warmbly/internal/utils"
	"github.com/warmbly/warmbly/internal/utils/validate"
)

type Service interface {
	List(ctx context.Context, orgID uuid.UUID) ([]models.Form, *errx.Error)
	Get(ctx context.Context, orgID, id uuid.UUID) (*models.Form, *errx.Error)
	Create(ctx context.Context, orgID uuid.UUID, createdBy *uuid.UUID, name string) (*models.Form, *errx.Error)
	Update(ctx context.Context, orgID, id uuid.UUID, in *models.FormWrite) (*models.Form, *errx.Error)
	Delete(ctx context.Context, orgID, id uuid.UUID) *errx.Error
	ListSubmissions(ctx context.Context, orgID, formID uuid.UUID, limit int, before *time.Time) ([]models.FormSubmission, bool, *errx.Error)
	DeleteSubmission(ctx context.Context, orgID, formID, subID uuid.UUID) *errx.Error
	// SetAsset stores an uploaded brand asset URL; an empty url clears it.
	SetAsset(ctx context.Context, orgID, id uuid.UUID, kind, url string) (*models.Form, *errx.Error)
	// Stats aggregates the funnel events for the analytics tab.
	Stats(ctx context.Context, orgID, formID uuid.UUID, rangeDays int) (*models.FormStats, *errx.Error)
	// CampaignForms reports how every form this campaign links to performed
	// for that campaign's recipients.
	CampaignForms(ctx context.Context, orgID, campaignID uuid.UUID) ([]models.CampaignFormStats, *errx.Error)

	// FormsHost is the host this organization's form URLs are built on: their
	// verified custom domain, or the install's shared forms host.
	FormsHost(ctx context.Context, orgID uuid.UUID) string
	FormsDomainStatus(ctx context.Context, orgID uuid.UUID) (*models.FormsDomainStatus, *errx.Error)
	SetFormsDomain(ctx context.Context, orgID uuid.UUID, raw string) (*models.FormsDomainStatus, *errx.Error)
	VerifyFormsDomain(ctx context.Context, orgID uuid.UUID) (*models.FormsDomainStatus, *errx.Error)

	// MintLink returns the personalized form URL for a contact (dashboard
	// on-demand path). MintForContact backs the campaign-send resolver: it is
	// public-id scoped to the org and reports ok=false for an unknown or
	// unpublished form so the caller can fall back to the share URL.
	MintLink(ctx context.Context, orgID, formID, contactID uuid.UUID) (string, *errx.Error)
	MintForContact(ctx context.Context, orgID uuid.UUID, formPublicID string, contactID uuid.UUID, campaignID *uuid.UUID) (personalURL, shareURL string, ok bool)

	// PublicForm resolves a published form for the hosted page.
	PublicForm(ctx context.Context, publicID string) (*models.Form, *errx.Error)
	// RecordView bumps the view counter, deduped per source by the caller.
	RecordView(ctx context.Context, formID uuid.UUID)
	// RecordEvent stores one funnel event, enriched server-side.
	RecordEvent(ctx context.Context, publicID string, in EventInput) *errx.Error
	// ResolveLink turns a ?t= token into the link it names (contact and the
	// campaign whose email carried it) plus the prefill values for the form;
	// anything invalid resolves to nil, never an error, so a bad token
	// degrades to an anonymous visit.
	ResolveLink(ctx context.Context, f *models.Form, token string) (*models.FormLink, map[string]string)
	// Submit runs the public pipeline: validate, contact upsert, store.
	Submit(ctx context.Context, publicID string, answers map[string][]string, meta SubmitMeta) (*SubmitResult, *errx.Error)

	// SetCaptcha wires the Turnstile verifier; nil leaves captcha off.
	SetCaptcha(v CaptchaVerifier)
	// SetTriage wires the TypeSafe judge; nil leaves submission triage off.
	SetTriage(a typesafe.Asker)
	SetRealtime(p RealtimePublisher)
	SetWebhooks(d WebhookDispatcher)
	SetContacts(a ContactAdder)
	SetContactReader(r ContactReader)
	SetGeo(g *geo.Client)
	SetLinks(r repository.FormLinkRepository)
	SetEvents(r repository.FormEventRepository)
	SetDomains(d OrgStore)
	SetCampaigns(k CampaignKeeper)
}

// ContactReader is the slice of the contact repository the link paths need.
type ContactReader interface {
	GetByID(ctx context.Context, contactID uuid.UUID) (*models.Contact, *errx.Error)
}

// ContactAdder is the slice of the contact service the submit pipeline needs.
type ContactAdder interface {
	Add(ctx context.Context, userID string, orgID uuid.UUID, contacts []models.AddContact) ([]models.Contact, *errx.Error)
}

// CaptchaVerifier validates a Turnstile token for a source IP.
type CaptchaVerifier interface {
	Verify(ctx context.Context, token, remoteIP string) *errx.Error
}

// RealtimePublisher fans the submission signal out to the org channel.
type RealtimePublisher interface {
	PublishFormSubmission(ctx context.Context, orgID, formID uuid.UUID, submissionID, contactID string)
}

// WebhookDispatcher delivers form.submitted to subscribed endpoints.
type WebhookDispatcher interface {
	Dispatch(ctx context.Context, orgID uuid.UUID, eventType models.WebhookEventType, data any) (uuid.UUID, error)
}

// SubmitMeta is everything the handler knows about the request that is not
// an answer: abuse signals and attribution.
type SubmitMeta struct {
	RemoteIP     string
	SourceURL    string
	CaptchaToken string
	// HoneypotFilled means the hidden trap field carried a value.
	HoneypotFilled bool
	// RenderedAt is when the page was served; bots submit near-instantly.
	RenderedAt time.Time
	// LinkToken is the personalized ?t= ticket; its contact wins attribution.
	LinkToken string
	// VisitorKey ties the submission to the visitor's funnel events.
	VisitorKey string
}

// SubmitResult is what the visitor sees after a successful submit.
type SubmitResult struct {
	Message     string `json:"message"`
	RedirectURL string `json:"redirect_url,omitempty"`
}

// CampaignKeeper turns on a campaign's "Keep running for new leads" setting
// when a form starts feeding it. Implemented by the campaign service.
type CampaignKeeper interface {
	KeepRunning(ctx context.Context, orgID, campaignID uuid.UUID, reason string) *errx.Error
}

type service struct {
	repo          repository.FormRepository
	campaigns     CampaignKeeper
	links         repository.FormLinkRepository
	events        repository.FormEventRepository
	domains       OrgStore
	contacts      ContactAdder
	contactReader ContactReader
	captcha       CaptchaVerifier
	triage        typesafe.Asker
	realtime      RealtimePublisher
	webhooks      WebhookDispatcher
	geo           *geo.Client
}

func NewService(repo repository.FormRepository) Service {
	return &service{repo: repo}
}

func (s *service) SetCaptcha(v CaptchaVerifier)               { s.captcha = v }
func (s *service) SetTriage(a typesafe.Asker)                 { s.triage = a }
func (s *service) SetRealtime(p RealtimePublisher)            { s.realtime = p }
func (s *service) SetWebhooks(d WebhookDispatcher)            { s.webhooks = d }
func (s *service) SetContacts(a ContactAdder)                 { s.contacts = a }
func (s *service) SetContactReader(r ContactReader)           { s.contactReader = r }
func (s *service) SetGeo(g *geo.Client)                       { s.geo = g }
func (s *service) SetLinks(r repository.FormLinkRepository)   { s.links = r }
func (s *service) SetEvents(r repository.FormEventRepository) { s.events = r }
func (s *service) SetDomains(d OrgStore)                      { s.domains = d }
func (s *service) SetCampaigns(k CampaignKeeper)              { s.campaigns = k }

// formTrendDays is the sparkline window on the forms list.
const formTrendDays = 14

func (s *service) List(ctx context.Context, orgID uuid.UUID) ([]models.Form, *errx.Error) {
	forms, xerr := s.repo.List(ctx, orgID)
	if xerr != nil || s.events == nil {
		return forms, xerr
	}
	// Attach the event rollups; losing them must never fail the list.
	aggs, aerr := s.events.ListAggregates(ctx, orgID, formTrendDays)
	if aerr != nil {
		return forms, nil
	}
	for i := range forms {
		if a, ok := aggs[forms[i].ID]; ok {
			forms[i].StartsCount = a.Starts
			forms[i].IdentifiedCount = a.Identified
			forms[i].Trend = a.Trend
		} else {
			forms[i].Trend = make([]int64, formTrendDays)
		}
	}
	return forms, nil
}

func (s *service) Get(ctx context.Context, orgID, id uuid.UUID) (*models.Form, *errx.Error) {
	return s.repo.Get(ctx, orgID, id)
}

func (s *service) Create(ctx context.Context, orgID uuid.UUID, createdBy *uuid.UUID, name string) (*models.Form, *errx.Error) {
	name, xerr := models.ValidateFormName(name)
	if xerr != nil {
		return nil, xerr
	}
	f := &models.Form{
		PublicID:       newPublicID(),
		Name:           name,
		Status:         models.FormStatusDraft,
		Fields:         defaultFields(),
		SuccessMessage: models.FormDefaultSuccessMsg,
	}
	return s.repo.Create(ctx, orgID, createdBy, f)
}

// defaultFields seeds a new form so the builder never opens empty.
func defaultFields() []models.FormField {
	return []models.FormField{
		{ID: "first_name", Type: models.FormFieldText, Label: "First name", MapTo: "first_name", Width: "half"},
		{ID: "last_name", Type: models.FormFieldText, Label: "Last name", MapTo: "last_name", Width: "half"},
		{ID: "email", Type: models.FormFieldEmail, Label: "Email", MapTo: "email", Required: true},
	}
}

func (s *service) Update(ctx context.Context, orgID, id uuid.UUID, in *models.FormWrite) (*models.Form, *errx.Error) {
	f, xerr := s.repo.Get(ctx, orgID, id)
	if xerr != nil {
		return nil, xerr
	}
	if in.Name != nil {
		name, xerr := models.ValidateFormName(*in.Name)
		if xerr != nil {
			return nil, xerr
		}
		f.Name = name
	}
	if in.Fields != nil {
		fields := *in.Fields
		if xerr := models.ValidateFormFields(fields); xerr != nil {
			return nil, xerr
		}
		f.Fields = fields
	}
	if in.Design != nil {
		d := *in.Design
		if xerr := models.ValidateFormDesign(&d); xerr != nil {
			return nil, xerr
		}
		models.NormalizeFormDesign(&d)
		f.Design = d
	}
	if in.SuccessMessage != nil {
		msg := strings.TrimSpace(*in.SuccessMessage)
		if len(msg) > models.FormMaxTextLen {
			return nil, errx.New(errx.BadRequest, "success message is too long")
		}
		if msg == "" {
			msg = models.FormDefaultSuccessMsg
		}
		f.SuccessMessage = msg
	}
	if in.RedirectURL != nil {
		u, xerr := models.ValidateFormRedirectURL(*in.RedirectURL)
		if xerr != nil {
			return nil, xerr
		}
		f.RedirectURL = u
	}
	if in.CampaignID.Set {
		f.CampaignID = in.CampaignID.Value
	}
	if in.CategoryIDs != nil {
		f.CategoryIDs = *in.CategoryIDs
	}
	if in.AllowedDomains != nil {
		domains, xerr := models.ValidateFormDomains(*in.AllowedDomains)
		if xerr != nil {
			return nil, xerr
		}
		f.AllowedDomains = domains
	}
	if in.CaptchaEnabled != nil {
		f.CaptchaEnabled = *in.CaptchaEnabled
	}
	if in.TriageEnabled != nil {
		f.TriageEnabled = *in.TriageEnabled
	}
	if in.Status != nil {
		if !in.Status.Valid() {
			return nil, errx.New(errx.BadRequest, "status must be draft, published or archived")
		}
		if *in.Status == models.FormStatusPublished && f.Status != models.FormStatusPublished {
			if xerr := publishable(f.Fields); xerr != nil {
				return nil, xerr
			}
			now := time.Now()
			f.PublishedAt = &now
		}
		f.Status = *in.Status
	}
	// A form is a live lead source, so the campaign it feeds must wait for
	// leads instead of finishing between submissions (issue #340), exactly as
	// a linked segment does. After every validation so a rejected write never
	// changes the campaign, and before the write so it proves the campaign is
	// this organization's; a failure here fails the save visibly.
	if in.CampaignID.Set && in.CampaignID.Value != nil && s.campaigns != nil {
		if xerr := s.campaigns.KeepRunning(ctx, orgID, *in.CampaignID.Value, "the form \""+f.Name+"\" adds its leads to this campaign"); xerr != nil {
			if xerr.Code == errx.ErrNotFound.Code {
				return nil, errx.New(errx.BadRequest, "campaign not found")
			}
			return nil, xerr
		}
	}
	return s.repo.Update(ctx, orgID, f)
}

// publishable refuses to put a form online that cannot collect anything.
func publishable(fields []models.FormField) *errx.Error {
	for _, f := range fields {
		if f.Type.IsInput() && f.Type != models.FormFieldHidden {
			return nil
		}
	}
	return errx.New(errx.BadRequest, "add at least one input field before publishing")
}

func (s *service) SetAsset(ctx context.Context, orgID, id uuid.UUID, kind, url string) (*models.Form, *errx.Error) {
	var logo, cover, background *string
	switch kind {
	case "logo":
		logo = &url
	case "cover":
		cover = &url
	case "background":
		background = &url
	default:
		return nil, errx.New(errx.BadRequest, "kind must be logo, cover or background")
	}
	return s.repo.UpdateAssets(ctx, orgID, id, logo, cover, background)
}

func (s *service) Delete(ctx context.Context, orgID, id uuid.UUID) *errx.Error {
	return s.repo.Delete(ctx, orgID, id)
}

func (s *service) ListSubmissions(ctx context.Context, orgID, formID uuid.UUID, limit int, before *time.Time) ([]models.FormSubmission, bool, *errx.Error) {
	return s.repo.ListSubmissions(ctx, orgID, formID, limit, before)
}

func (s *service) DeleteSubmission(ctx context.Context, orgID, formID, subID uuid.UUID) *errx.Error {
	return s.repo.DeleteSubmission(ctx, orgID, formID, subID)
}

func (s *service) PublicForm(ctx context.Context, publicID string) (*models.Form, *errx.Error) {
	f, xerr := s.repo.GetByPublicID(ctx, publicID)
	if xerr != nil {
		return nil, xerr
	}
	if f.Status != models.FormStatusPublished {
		return nil, errx.New(errx.NotFound, "form not found")
	}
	return f, nil
}

func (s *service) RecordView(ctx context.Context, formID uuid.UUID) {
	// Best-effort: a lost view must never fail a page render.
	_ = s.repo.RecordView(ctx, formID)
}

// minFillSeconds is the human floor: a submit faster than this after render
// is treated as a bot and quietly discarded.
const minFillSeconds = 2

func (s *service) Submit(ctx context.Context, publicID string, answers map[string][]string, meta SubmitMeta) (*SubmitResult, *errx.Error) {
	f, xerr := s.PublicForm(ctx, publicID)
	if xerr != nil {
		return nil, xerr
	}
	ok := &SubmitResult{Message: f.SuccessMessage, RedirectURL: f.RedirectURL}

	// Traps: pretend success so a bot learns nothing and stops probing.
	if meta.HoneypotFilled {
		return ok, nil
	}
	if !meta.RenderedAt.IsZero() && time.Since(meta.RenderedAt) < minFillSeconds*time.Second {
		return ok, nil
	}
	if f.CaptchaEnabled && s.captcha != nil {
		if xerr := s.captcha.Verify(ctx, meta.CaptchaToken, meta.RemoteIP); xerr != nil {
			return nil, errx.New(errx.BadRequest, "captcha verification failed")
		}
	}

	data, lead, xerr := buildSubmission(f.Fields, answers)
	if xerr != nil {
		return nil, xerr
	}

	sub := &models.FormSubmission{
		FormID:         f.ID,
		OrganizationID: f.OrganizationID,
		Data:           data,
		SourceURL:      truncate(meta.SourceURL, 2048),
	}

	link, _ := s.ResolveLink(ctx, f, meta.LinkToken)
	if link != nil {
		sub.CampaignID = link.CampaignID
	}

	// Triage is best-effort too: a timeout leaves the submission untriaged
	// and everything below runs as if the form had it off.
	if f.TriageEnabled && s.triage != nil {
		tctx, cancel := context.WithTimeout(ctx, triageTimeout)
		if verdict, confidence, ok := triage(typesafe.WithUsage(tctx, "form_triage", f.OrganizationID.String()), s.triage, f.Fields, data); ok {
			sub.Triage = verdict
			sub.TriageConfidence = confidence
		}
		cancel()
	}
	junk := sub.Triage == models.FormTriageJunk

	// The contact write is best-effort: a plan cap or a bad address must not
	// lose the submission, and never the visitor's success page. Junk is
	// stored and flagged but never becomes a contact or a lead.
	var contactID string
	if lead != nil && s.contacts != nil && !junk {
		// A form outlives the member who made it: created_by is ON DELETE SET
		// NULL, so offboarding that person would otherwise stop lead capture
		// silently while submissions kept storing. Contacts are org-owned, so
		// the workspace owner is the right stand-in actor.
		owner := f.CreatedBy
		if owner == nil && s.domains != nil {
			if org, oerr := s.domains.GetByID(ctx, f.OrganizationID); oerr == nil && org != nil {
				owner = &org.OwnerUserID
			}
		}
		if owner != nil {
			lead.Campaigns = campaignList(f.CampaignID)
			lead.Categories = categoryList(f.CategoryIDs)
			lead.Source = models.ContactSourceForm
			lead.SourceDetail = f.Name
			created, cerr := s.contacts.Add(ctx, owner.String(), f.OrganizationID, []models.AddContact{*lead})
			if cerr == nil && len(created) > 0 {
				id := created[0].ID
				sub.ContactID = &id
				contactID = id.String()
			}
		}
	}

	// A personalized link identifies the visitor outright, so its contact
	// wins over whatever the email upsert matched.
	if link != nil {
		id := link.ContactID
		sub.ContactID = &id
		contactID = id.String()
	}

	stored, xerr := s.repo.CreateSubmission(ctx, sub)
	if xerr != nil {
		return nil, xerr
	}
	if s.events != nil {
		_ = s.events.Insert(ctx, &models.FormEvent{
			OrganizationID: f.OrganizationID,
			FormID:         f.ID,
			Type:           models.FormEventSubmit,
			VisitorKey:     truncate(meta.VisitorKey, 64),
			PagesTotal:     formPageCount(f.Fields),
			ContactID:      sub.ContactID,
			CampaignID:     sub.CampaignID,
			ReferrerDomain: referrerDomain(meta.SourceURL),
			Device:         "unknown",
		})
	}
	if sub.ContactID != nil {
		_ = s.repo.LogSubmissionActivity(ctx, f.OrganizationID, *sub.ContactID, f.ID, f.Name, stored.ID)
	}
	if s.realtime != nil {
		s.realtime.PublishFormSubmission(ctx, f.OrganizationID, f.ID, stored.ID.String(), contactID)
	}
	if s.webhooks != nil {
		payload := map[string]any{
			"form_id":       f.ID.String(),
			"form_name":     f.Name,
			"submission_id": stored.ID.String(),
			"data":          data,
			"source_url":    sub.SourceURL,
		}
		if contactID != "" {
			payload["contact_id"] = contactID
		}
		// The mapped contact columns ride along flat so an automation can read
		// {{.contact_email}} or {{.first_name}} without digging into data.
		if lead != nil {
			payload["contact_email"] = lead.Email
			payload["first_name"] = lead.FirstName
			payload["last_name"] = lead.LastName
			payload["company"] = lead.Company
			payload["phone"] = lead.Phone
		}
		if sub.CampaignID != nil {
			payload["campaign_id"] = sub.CampaignID.String()
		}
		if sub.Triage != "" {
			payload["triage"] = sub.Triage
		}
		_, _ = s.webhooks.Dispatch(ctx, f.OrganizationID, models.WebhookEventFormSubmitted, payload)
	}
	return ok, nil
}

// buildSubmission validates the answers against the field list and splits
// them into the stored payload and the contact to upsert. A nil lead means
// the form collected no usable email.
func buildSubmission(fields []models.FormField, answers map[string][]string) (map[string]any, *models.AddContact, *errx.Error) {
	data := make(map[string]any, len(fields))
	cols := map[string]string{}
	custom := map[string]string{}

	for _, f := range fields {
		if !f.Type.IsInput() {
			continue
		}
		vals := answers[f.ID]
		label := f.Label
		if label == "" {
			label = f.ID
		}

		if f.Type == models.FormFieldCheckboxes {
			picked := make([]string, 0, len(vals))
			allowed := optionSet(f.Options)
			for _, v := range vals {
				v = strings.TrimSpace(v)
				if v == "" {
					continue
				}
				if !allowed[v] {
					return nil, nil, errx.New(errx.BadRequest, label+": unknown option")
				}
				picked = append(picked, v)
			}
			if f.Required && len(picked) == 0 {
				return nil, nil, errx.New(errx.BadRequest, label+" is required")
			}
			if len(picked) > 0 {
				data[f.ID] = picked
				custom[customKey(f)] = strings.Join(picked, ", ")
			}
			continue
		}

		v := ""
		if len(vals) > 0 {
			v = strings.TrimSpace(vals[0])
		}
		// A hidden field's value is the owner's, not the submitter's: it carries
		// the campaign tag, source or routing key they configured. The posted
		// value is ignored so the form cannot be made to file a lead under
		// something else.
		if f.Type == models.FormFieldHidden {
			v = f.Value
		}
		if len(v) > models.FormMaxAnswerLen {
			return nil, nil, errx.New(errx.BadRequest, label+" is too long")
		}

		switch f.Type {
		case models.FormFieldCheckbox:
			checked := v == "on" || v == "true" || v == "1" || v == "yes"
			if f.Required && !checked {
				return nil, nil, errx.New(errx.BadRequest, label+" must be checked")
			}
			if checked {
				data[f.ID] = "yes"
				custom[customKey(f)] = "yes"
			}
			continue
		case models.FormFieldEmail:
			if v != "" && !validate.Email(v) {
				return nil, nil, errx.New(errx.BadRequest, label+" must be a valid email address")
			}
			v = strings.ToLower(v)
		case models.FormFieldNumber:
			if v != "" {
				if _, err := strconv.ParseFloat(v, 64); err != nil {
					return nil, nil, errx.New(errx.BadRequest, label+" must be a number")
				}
			}
		case models.FormFieldDate:
			if v != "" {
				if _, err := time.Parse("2006-01-02", v); err != nil {
					return nil, nil, errx.New(errx.BadRequest, label+" must be a date")
				}
			}
		case models.FormFieldSelect, models.FormFieldRadio:
			if v != "" && !optionSet(f.Options)[v] {
				return nil, nil, errx.New(errx.BadRequest, label+": unknown option")
			}
		}

		if f.Required && v == "" {
			return nil, nil, errx.New(errx.BadRequest, label+" is required")
		}
		if v == "" {
			continue
		}
		data[f.ID] = v
		if f.MapTo != "" {
			cols[f.MapTo] = v
		} else {
			custom[customKey(f)] = v
		}
	}

	if cols["email"] == "" {
		return data, nil, nil
	}
	lead := &models.AddContact{
		Email:        cols["email"],
		FirstName:    cols["first_name"],
		LastName:     cols["last_name"],
		Company:      cols["company"],
		Phone:        cols["phone"],
		CustomFields: custom,
	}
	return data, lead, nil
}

var jsonKeyStrip = regexp.MustCompile(`[^A-Za-z0-9_ -]+`)

// customKey turns the field label into an addressable contact custom-field
// key ("Company size" stays as-is; punctuation is stripped so "Role?" still
// yields a key the template engine can resolve). The field id is the
// fallback when nothing usable remains.
func customKey(f models.FormField) string {
	label := f.Label
	if label == "" {
		label = f.ID
	}
	key := utils.NormalizeJSONKey(label)
	if !utils.IsValidJSONKey(key) {
		key = utils.NormalizeJSONKey(jsonKeyStrip.ReplaceAllString(key, " "))
	}
	if utils.IsValidJSONKey(key) {
		return key
	}
	return f.ID
}

func optionSet(opts []string) map[string]bool {
	set := make(map[string]bool, len(opts))
	for _, o := range opts {
		set[o] = true
	}
	return set
}

func campaignList(id *uuid.UUID) []string {
	if id == nil {
		return nil
	}
	return []string{id.String()}
}

func categoryList(ids []uuid.UUID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, id.String())
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// publicIDAlphabet keeps the token URL-safe and unambiguous.
const publicIDAlphabet = "abcdefghijkmnpqrstuvwxyz23456789"

// newPublicID mints the unguessable page token: 21 chars of 32-symbol
// alphabet is over 100 bits of entropy.
func newPublicID() string {
	buf := make([]byte, 21)
	if _, err := rand.Read(buf); err != nil {
		return uuid.NewString()
	}
	for i, b := range buf {
		buf[i] = publicIDAlphabet[int(b)%len(publicIDAlphabet)]
	}
	return string(buf)
}
