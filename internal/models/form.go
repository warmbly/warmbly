package models

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
)

// Form is a hosted lead-capture form: an ordered field list plus a design
// theme, published at a public URL and embeddable on any website. A
// submission creates or updates a contact, files it under the form's
// categories and optionally adds it to a campaign.
type Form struct {
	ID             uuid.UUID  `json:"id"`
	OrganizationID uuid.UUID  `json:"organization_id"`
	CreatedBy      *uuid.UUID `json:"created_by,omitempty"`
	// PublicID is the unguessable token in the public URL and embed codes.
	PublicID       string      `json:"public_id"`
	Name           string      `json:"name"`
	Status         FormStatus  `json:"status"`
	Fields         []FormField `json:"fields"`
	Design         FormDesign  `json:"design"`
	SuccessMessage string      `json:"success_message"`
	RedirectURL    string      `json:"redirect_url"`
	// RedirectURLActive is false when RedirectURL is not on one of the workspace's own domains, so visitors see SuccessMessage instead.
	RedirectURLActive *bool       `json:"redirect_url_active,omitempty"`
	CampaignID        *uuid.UUID  `json:"campaign_id,omitempty"`
	CategoryIDs       []uuid.UUID `json:"category_ids"`
	AllowedDomains    []string    `json:"allowed_domains"`
	CaptchaEnabled    bool        `json:"captcha_enabled"`
	// TriageEnabled classifies each submission through TypeSafe; junk is kept
	// but creates no contact and joins no campaign.
	TriageEnabled bool `json:"triage_enabled"`
	// LogoURL, CoverURL and BackgroundURL are uploaded brand assets (public
	// object URLs).
	LogoURL       string `json:"logo_url"`
	CoverURL      string `json:"cover_url"`
	BackgroundURL string `json:"background_url"`

	ViewsCount       int64      `json:"views_count"`
	SubmissionsCount int64      `json:"submissions_count"`
	LastSubmissionAt *time.Time `json:"last_submission_at,omitempty"`
	PublishedAt      *time.Time `json:"published_at,omitempty"`

	// Event-derived aggregates, populated by List only (nil Trend elsewhere).
	StartsCount     int64   `json:"starts_count"`
	IdentifiedCount int64   `json:"identified_count"`
	Trend           []int64 `json:"trend,omitempty"`

	// ShareURL is the hosted page URL, derived from config on read.
	ShareURL string `json:"share_url,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// FormStatus is the lifecycle of a form. Only published forms render and
// accept submissions; archiving keeps the data but takes the page offline.
type FormStatus string

const (
	FormStatusDraft     FormStatus = "draft"
	FormStatusPublished FormStatus = "published"
	FormStatusArchived  FormStatus = "archived"
)

// Valid reports whether the value is one the database accepts.
func (s FormStatus) Valid() bool {
	switch s {
	case FormStatusDraft, FormStatusPublished, FormStatusArchived:
		return true
	}
	return false
}

// FormFieldType is one kind of builder block. Input types collect a value;
// layout types (heading, paragraph, divider) only render.
type FormFieldType string

const (
	FormFieldText       FormFieldType = "text"
	FormFieldEmail      FormFieldType = "email"
	FormFieldPhone      FormFieldType = "phone"
	FormFieldTextarea   FormFieldType = "textarea"
	FormFieldNumber     FormFieldType = "number"
	FormFieldSelect     FormFieldType = "select"
	FormFieldRadio      FormFieldType = "radio"
	FormFieldCheckboxes FormFieldType = "checkboxes"
	FormFieldCheckbox   FormFieldType = "checkbox"
	FormFieldDate       FormFieldType = "date"
	FormFieldHidden     FormFieldType = "hidden"
	FormFieldHeading    FormFieldType = "heading"
	FormFieldParagraph  FormFieldType = "paragraph"
	FormFieldDivider    FormFieldType = "divider"
	// FormFieldPageBreak splits the form into pages; its label is the page title.
	FormFieldPageBreak FormFieldType = "page_break"
)

// Valid reports whether the type is a known block.
func (t FormFieldType) Valid() bool {
	switch t {
	case FormFieldText, FormFieldEmail, FormFieldPhone, FormFieldTextarea, FormFieldNumber,
		FormFieldSelect, FormFieldRadio, FormFieldCheckboxes, FormFieldCheckbox, FormFieldDate,
		FormFieldHidden, FormFieldHeading, FormFieldParagraph, FormFieldDivider, FormFieldPageBreak:
		return true
	}
	return false
}

// IsInput reports whether the block collects a value on submit.
func (t FormFieldType) IsInput() bool {
	switch t {
	case FormFieldHeading, FormFieldParagraph, FormFieldDivider, FormFieldPageBreak:
		return false
	}
	return true
}

// HasOptions reports whether the block needs an options list.
func (t FormFieldType) HasOptions() bool {
	return t == FormFieldSelect || t == FormFieldRadio || t == FormFieldCheckboxes
}

// FormField is one block on the form. ID is a builder-generated slug, stable
// across edits; submissions key their answers by it.
type FormField struct {
	ID          string        `json:"id"`
	Type        FormFieldType `json:"type"`
	Label       string        `json:"label"`
	Placeholder string        `json:"placeholder,omitempty"`
	HelpText    string        `json:"help_text,omitempty"`
	Required    bool          `json:"required"`
	Options     []string      `json:"options,omitempty"`
	// MapTo names the contact column the answer fills (first_name, last_name,
	// email, company, phone). Empty means the answer lands in the contact's
	// custom fields under the field label.
	MapTo string `json:"map_to,omitempty"`
	// Value is the constant a hidden field submits, or the body text of a
	// paragraph block.
	Value string `json:"value,omitempty"`
	// Width is full or half; two half fields share a row.
	Width string `json:"width,omitempty"`
	Rows  int    `json:"rows,omitempty"`
}

// FormDesign is the theme the builder's Design panel edits. Unset values fall
// back to the defaults in NormalizeFormDesign, so an older row still renders.
type FormDesign struct {
	FontFamily       string `json:"font_family,omitempty"`
	PageBackground   string `json:"page_background,omitempty"`
	FormBackground   string `json:"form_background,omitempty"`
	TextColor        string `json:"text_color,omitempty"`
	LabelColor       string `json:"label_color,omitempty"`
	InputBackground  string `json:"input_background,omitempty"`
	InputBorderColor string `json:"input_border_color,omitempty"`
	InputTextColor   string `json:"input_text_color,omitempty"`
	PlaceholderColor string `json:"placeholder_color,omitempty"`
	AccentColor      string `json:"accent_color,omitempty"`
	ButtonBackground string `json:"button_background,omitempty"`
	ButtonTextColor  string `json:"button_text_color,omitempty"`
	ButtonText       string `json:"button_text,omitempty"`
	ButtonSize       string `json:"button_size,omitempty"`
	ButtonFullWidth  bool   `json:"button_full_width,omitempty"`
	BorderRadius     *int   `json:"border_radius,omitempty"`
	MaxWidth         *int   `json:"max_width,omitempty"`
	Spacing          string `json:"spacing,omitempty"`
	Shadow           *bool  `json:"shadow,omitempty"`
	// Theme records which preset seeded the current colors; renderers never
	// read it, so new presets need no backend release.
	Theme string `json:"theme,omitempty"`
	// Layout is card (centered box), wide (no box) or split (cover panel).
	Layout string `json:"layout,omitempty"`
	// Mode is classic (pages of fields) or focus (one question per screen).
	Mode string `json:"mode,omitempty"`
	// PageBackgroundEnd turns the page background into a vertical gradient.
	PageBackgroundEnd string `json:"page_background_end,omitempty"`
	Align             string `json:"align,omitempty"`
	ShowProgress      *bool  `json:"show_progress,omitempty"`

	// BackgroundSize and BackgroundOverlay style the uploaded background_url:
	// the overlay (0-100) veils the image with the page color so text on top
	// stays legible.
	BackgroundSize    string `json:"background_size,omitempty"`
	BackgroundOverlay *int   `json:"background_overlay,omitempty"`

	// A bar carrying the logo and a title: whether it spans the page or sits
	// with the form, how its contents align, and whether it sticks on scroll.
	HeaderEnabled    *bool  `json:"header_enabled,omitempty"`
	HeaderTitle      string `json:"header_title,omitempty"`
	HeaderBackground string `json:"header_background,omitempty"`
	HeaderPlacement  string `json:"header_placement,omitempty"`
	HeaderAlign      string `json:"header_align,omitempty"`
	HeaderSticky     *bool  `json:"header_sticky,omitempty"`
	HeaderShowLogo   *bool  `json:"header_show_logo,omitempty"`

	// Copy over the split layout's cover panel.
	CoverTitle    string `json:"cover_title,omitempty"`
	CoverSubtitle string `json:"cover_subtitle,omitempty"`

	// How the uploaded logo renders: its height, and whether it sits on the
	// form surface or above the card on the page background.
	LogoSize     string `json:"logo_size,omitempty"`
	LogoPosition string `json:"logo_position,omitempty"`
}

// FormWrite is the PATCH payload; every field is optional.
type FormWrite struct {
	Name           *string      `json:"name,omitempty"`
	Status         *FormStatus  `json:"status,omitempty"`
	Fields         *[]FormField `json:"fields,omitempty"`
	Design         *FormDesign  `json:"design,omitempty"`
	SuccessMessage *string      `json:"success_message,omitempty"`
	RedirectURL    *string      `json:"redirect_url,omitempty"`
	CampaignID     NullableUUID `json:"campaign_id"`
	CategoryIDs    *[]uuid.UUID `json:"category_ids,omitempty"`
	AllowedDomains *[]string    `json:"allowed_domains,omitempty"`
	CaptchaEnabled *bool        `json:"captcha_enabled,omitempty"`
	TriageEnabled  *bool        `json:"triage_enabled,omitempty"`
}

// Form triage verdicts, as stored on form_submissions.triage. Empty means the
// submission was not triaged.
const (
	FormTriageBuyer     = "buyer"
	FormTriageVendor    = "vendor"
	FormTriageJobSeeker = "job_seeker"
	FormTriageOther     = "other"
	FormTriageJunk      = "junk"
)

// FormSubmission is one public submit, kept verbatim. Data is keyed by field
// id; checkbox groups store a string slice, everything else a string.
// FormSubmissionPosition is the keyset a submissions page continues after; a
// nil ID is a legacy timestamp-only cursor and continues strictly before At.
type FormSubmissionPosition struct {
	At time.Time
	ID uuid.UUID
}

type FormSubmission struct {
	ID             uuid.UUID  `json:"id"`
	FormID         uuid.UUID  `json:"form_id"`
	OrganizationID uuid.UUID  `json:"organization_id"`
	ContactID      *uuid.UUID `json:"contact_id,omitempty"`
	// CampaignID is the campaign whose email carried the personalized link
	// the visitor arrived through, when there was one.
	CampaignID *uuid.UUID     `json:"campaign_id,omitempty"`
	Data       map[string]any `json:"data"`
	SourceURL  string         `json:"source_url"`
	// Triage is the FormTriage* verdict, empty when the form did not triage
	// or the call did not complete; TriageConfidence is the model's own.
	Triage           string    `json:"triage"`
	TriageConfidence float64   `json:"triage_confidence"`
	CreatedAt        time.Time `json:"created_at"`

	// Contact and campaign summaries for the submissions table, populated by
	// reads.
	ContactEmail string `json:"contact_email,omitempty"`
	ContactName  string `json:"contact_name,omitempty"`
	CampaignName string `json:"campaign_name,omitempty"`
}

// Form limits.
const (
	FormMaxNameLen        = 120
	FormsPerOrgMax        = 100
	FormMaxFields         = 60
	FormMaxLabelLen       = 200
	FormMaxTextLen        = 2000
	FormMaxOptions        = 50
	FormMaxOptionLen      = 100
	FormMaxDomains        = 20
	FormMaxAnswerLen      = 5000
	FormMaxCategories     = 20
	FormMinBorderRadius   = 0
	FormMaxBorderRadius   = 24
	FormMinWidth          = 320
	FormMaxWidth          = 960
	FormDefaultWidth      = 560
	FormDefaultSuccessMsg = "Thanks! Your submission has been received."
)

var (
	formFieldIDRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,39}$`)
	formHexRE     = regexp.MustCompile(`^#[a-fA-F0-9]{6}$`)
	formThemeRE   = regexp.MustCompile(`^[a-z0-9-]{0,40}$`)
)

// FormContactColumns is every contact column a field may map to.
var FormContactColumns = map[string]bool{
	"first_name": true,
	"last_name":  true,
	"email":      true,
	"company":    true,
	"phone":      true,
}

// ValidateFormName trims and bounds the internal name.
func ValidateFormName(name string) (string, *errx.Error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errx.New(errx.BadRequest, "form name is required")
	}
	if len(name) > FormMaxNameLen {
		return "", errx.New(errx.BadRequest, fmt.Sprintf("form name must be at most %d characters", FormMaxNameLen))
	}
	return name, nil
}

// ValidateFormFields checks and normalizes the field list in place.
func ValidateFormFields(fields []FormField) *errx.Error {
	if len(fields) > FormMaxFields {
		return errx.New(errx.BadRequest, fmt.Sprintf("at most %d fields per form", FormMaxFields))
	}
	seenID := make(map[string]bool, len(fields))
	seenMap := make(map[string]bool, len(fields))
	for i := range fields {
		f := &fields[i]
		f.ID = strings.TrimSpace(f.ID)
		f.Label = strings.TrimSpace(f.Label)
		f.Placeholder = strings.TrimSpace(f.Placeholder)
		f.HelpText = strings.TrimSpace(f.HelpText)
		f.MapTo = strings.TrimSpace(f.MapTo)
		f.Value = strings.TrimSpace(f.Value)
		at := fmt.Sprintf("field %d", i+1)
		if !formFieldIDRE.MatchString(f.ID) {
			return errx.New(errx.BadRequest, at+": id must be a short lowercase slug")
		}
		if seenID[f.ID] {
			return errx.New(errx.BadRequest, at+": duplicate id "+f.ID)
		}
		seenID[f.ID] = true
		if !f.Type.Valid() {
			return errx.New(errx.BadRequest, fmt.Sprintf("%s: unknown type %q", at, f.Type))
		}
		if len(f.Label) > FormMaxLabelLen {
			return errx.New(errx.BadRequest, fmt.Sprintf("%s: label must be at most %d characters", at, FormMaxLabelLen))
		}
		if len(f.Placeholder) > FormMaxLabelLen || len(f.HelpText) > FormMaxLabelLen {
			return errx.New(errx.BadRequest, fmt.Sprintf("%s: placeholder and help text must be at most %d characters", at, FormMaxLabelLen))
		}
		if len(f.Value) > FormMaxTextLen {
			return errx.New(errx.BadRequest, fmt.Sprintf("%s: text must be at most %d characters", at, FormMaxTextLen))
		}
		if f.Type.IsInput() && f.Type != FormFieldHidden && f.Label == "" {
			return errx.New(errx.BadRequest, at+": a label is required")
		}
		if f.Type.HasOptions() {
			if len(f.Options) == 0 {
				return errx.New(errx.BadRequest, at+": at least one option is required")
			}
			if len(f.Options) > FormMaxOptions {
				return errx.New(errx.BadRequest, fmt.Sprintf("%s: at most %d options", at, FormMaxOptions))
			}
			seenOpt := make(map[string]bool, len(f.Options))
			for j, opt := range f.Options {
				opt = strings.TrimSpace(opt)
				f.Options[j] = opt
				if opt == "" || len(opt) > FormMaxOptionLen {
					return errx.New(errx.BadRequest, fmt.Sprintf("%s: options must be 1 to %d characters", at, FormMaxOptionLen))
				}
				if seenOpt[opt] {
					return errx.New(errx.BadRequest, at+": duplicate option "+opt)
				}
				seenOpt[opt] = true
			}
		} else {
			f.Options = nil
		}
		if f.MapTo != "" {
			if !f.Type.IsInput() {
				return errx.New(errx.BadRequest, at+": only input fields can map to a contact column")
			}
			if !FormContactColumns[f.MapTo] {
				return errx.New(errx.BadRequest, fmt.Sprintf("%s: unknown contact column %q", at, f.MapTo))
			}
			if seenMap[f.MapTo] {
				return errx.New(errx.BadRequest, fmt.Sprintf("%s: another field already maps to %s", at, f.MapTo))
			}
			seenMap[f.MapTo] = true
		}
		if f.Type == FormFieldEmail {
			f.MapTo = "email"
		}
		if f.Width != "" && f.Width != "full" && f.Width != "half" {
			return errx.New(errx.BadRequest, at+": width must be full or half")
		}
		if f.Rows < 0 || f.Rows > 20 {
			return errx.New(errx.BadRequest, at+": rows must be between 0 and 20")
		}
	}
	return nil
}

// FormEmailField returns the field that fills the contact email, if any.
func FormEmailField(fields []FormField) *FormField {
	for i := range fields {
		if fields[i].MapTo == "email" || fields[i].Type == FormFieldEmail {
			return &fields[i]
		}
	}
	return nil
}

// ValidateFormDesign checks colors and enums; NormalizeFormDesign clamps the
// numeric knobs and fills defaults.
func ValidateFormDesign(d *FormDesign) *errx.Error {
	for _, c := range []string{
		d.PageBackground, d.FormBackground, d.TextColor, d.LabelColor, d.InputBackground,
		d.InputBorderColor, d.InputTextColor, d.PlaceholderColor, d.AccentColor,
		d.ButtonBackground, d.ButtonTextColor, d.PageBackgroundEnd, d.HeaderBackground,
	} {
		if c != "" && !formHexRE.MatchString(c) && c != "transparent" {
			return errx.New(errx.BadRequest, "design colors must be #rrggbb values")
		}
	}
	// The list must match FONT_CATALOG in the mirrored designCore.ts.
	switch d.FontFamily {
	case "", "system", "inter", "serif", "mono", "manrope", "sora", "fraunces", "space-grotesk":
	default:
		return errx.New(errx.BadRequest, "font_family is not a supported font")
	}
	switch d.Layout {
	case "", "card", "wide", "split":
	default:
		return errx.New(errx.BadRequest, "layout must be card, wide or split")
	}
	switch d.Mode {
	case "", "classic", "focus":
	default:
		return errx.New(errx.BadRequest, "mode must be classic or focus")
	}
	switch d.Align {
	case "", "left", "center":
	default:
		return errx.New(errx.BadRequest, "align must be left or center")
	}
	switch d.HeaderPlacement {
	case "", "page", "inline":
	default:
		return errx.New(errx.BadRequest, "header_placement must be page or inline")
	}
	switch d.HeaderAlign {
	case "", "left", "center", "between":
	default:
		return errx.New(errx.BadRequest, "header_align must be left, center or between")
	}
	switch d.LogoSize {
	case "", "sm", "md", "lg":
	default:
		return errx.New(errx.BadRequest, "logo_size must be sm, md or lg")
	}
	switch d.LogoPosition {
	case "", "card", "page":
	default:
		return errx.New(errx.BadRequest, "logo_position must be card or page")
	}
	switch d.BackgroundSize {
	case "", "cover", "contain", "tile":
	default:
		return errx.New(errx.BadRequest, "background_size must be cover, contain or tile")
	}
	if d.BackgroundOverlay != nil && (*d.BackgroundOverlay < 0 || *d.BackgroundOverlay > 100) {
		return errx.New(errx.BadRequest, "background_overlay must be between 0 and 100")
	}
	for _, t := range []string{d.HeaderTitle, d.CoverTitle} {
		if len(t) > FormMaxLabelLen {
			return errx.New(errx.BadRequest, "title is too long")
		}
	}
	if len(d.CoverSubtitle) > FormMaxTextLen {
		return errx.New(errx.BadRequest, "cover subtitle is too long")
	}
	if !formThemeRE.MatchString(d.Theme) {
		return errx.New(errx.BadRequest, "theme must be a short lowercase slug")
	}
	switch d.ButtonSize {
	case "", "sm", "md", "lg":
	default:
		return errx.New(errx.BadRequest, "button_size must be sm, md or lg")
	}
	switch d.Spacing {
	case "", "compact", "normal", "relaxed":
	default:
		return errx.New(errx.BadRequest, "spacing must be compact, normal or relaxed")
	}
	if len(d.ButtonText) > FormMaxLabelLen {
		return errx.New(errx.BadRequest, "button text is too long")
	}
	return nil
}

// NormalizeFormDesign clamps numbers into range. String defaults are applied
// at render time so a stored row stays sparse.
func NormalizeFormDesign(d *FormDesign) {
	if d.BorderRadius != nil {
		v := clampInt(*d.BorderRadius, FormMinBorderRadius, FormMaxBorderRadius)
		d.BorderRadius = &v
	}
	if d.MaxWidth != nil {
		v := clampInt(*d.MaxWidth, FormMinWidth, FormMaxWidth)
		d.MaxWidth = &v
	}
	d.ButtonText = strings.TrimSpace(d.ButtonText)
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// ValidateFormRedirectURL accepts an empty value or an absolute http(s) URL.
func ValidateFormRedirectURL(raw string) (string, *errx.Error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return "", errx.New(errx.BadRequest, "redirect URL must be an absolute http(s) URL")
	}
	if len(raw) > 2048 {
		return "", errx.New(errx.BadRequest, "redirect URL is too long")
	}
	return raw, nil
}

// ValidateFormDomains normalizes the embed allowlist to bare lowercase hosts.
// A host admits its subdomains, mirroring website tracking.
func ValidateFormDomains(domains []string) ([]string, *errx.Error) {
	if len(domains) > FormMaxDomains {
		return nil, errx.New(errx.BadRequest, fmt.Sprintf("at most %d allowed domains", FormMaxDomains))
	}
	out := make([]string, 0, len(domains))
	seen := make(map[string]bool, len(domains))
	for _, d := range domains {
		h := strings.ToLower(strings.TrimSpace(d))
		h = strings.TrimPrefix(h, "https://")
		h = strings.TrimPrefix(h, "http://")
		if i := strings.IndexAny(h, "/?#"); i >= 0 {
			h = h[:i]
		}
		h = strings.TrimSuffix(h, ".")
		if h == "" {
			continue
		}
		if len(h) > 253 || strings.ContainsAny(h, " \t@") {
			return nil, errx.New(errx.BadRequest, fmt.Sprintf("%q is not a valid domain", d))
		}
		if !seen[h] {
			seen[h] = true
			out = append(out, h)
		}
	}
	return out, nil
}

// FormDomainAllowed reports whether host is covered by the allowlist. An
// empty list allows everything.
func FormDomainAllowed(allowed []string, host string) bool {
	if len(allowed) == 0 {
		return true
	}
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if h, _, ok := strings.Cut(host, ":"); ok {
		host = h
	}
	for _, a := range allowed {
		if host == a || strings.HasSuffix(host, "."+a) {
			return true
		}
	}
	return false
}
