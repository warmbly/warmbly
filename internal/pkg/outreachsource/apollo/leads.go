package apollo

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/warmbly/warmbly/internal/pkg/outreachsource"
)

type phoneNumber struct {
	RawNumber       text `json:"raw_number"`
	SanitizedNumber text `json:"sanitized_number"`
}

type org struct {
	Name          text `json:"name"`
	WebsiteURL    text `json:"website_url"`
	PrimaryDomain text `json:"primary_domain"`
}

// campaignStatus is a contact's place in one sequence.
type campaignStatus struct {
	SequenceID     string `json:"emailer_campaign_id"`
	Status         string `json:"status"`
	InactiveReason text   `json:"inactive_reason"`
	StepPosition   *int   `json:"current_step_position"`
}

type contact struct {
	ID                string           `json:"id"`
	FirstName         text             `json:"first_name"`
	LastName          text             `json:"last_name"`
	Email             text             `json:"email"`
	Title             text             `json:"title"`
	Headline          text             `json:"headline"`
	OrganizationName  text             `json:"organization_name"`
	LinkedinURL       text             `json:"linkedin_url"`
	SanitizedPhone    text             `json:"sanitized_phone"`
	PhoneNumbers      []phoneNumber    `json:"phone_numbers"`
	City              text             `json:"city"`
	State             text             `json:"state"`
	Country           text             `json:"country"`
	PresentRawAddress text             `json:"present_raw_address"`
	TimeZone          text             `json:"time_zone"`
	EmailUnsubscribed truthy           `json:"email_unsubscribed"`
	SequenceIDs       []string         `json:"emailer_campaign_ids"`
	Statuses          []campaignStatus `json:"contact_campaign_statuses"`
	CustomFields      map[string]any   `json:"typed_custom_fields"`
	Account           *org             `json:"account"`
	Organization      *org             `json:"organization"`
}

type contactPage struct {
	Contacts   []contact  `json:"contacts"`
	Pagination pagination `json:"pagination"`
}

// in returns the contact's status in a sequence, or nil when it is not in it.
func (c contact) in(sequenceID string) (*campaignStatus, bool) {
	for i := range c.Statuses {
		if c.Statuses[i].SequenceID == sequenceID {
			return &c.Statuses[i], true
		}
	}
	for _, id := range c.SequenceIDs {
		if id == sequenceID {
			return nil, true
		}
	}
	return nil, false
}

func (c contact) inAny() bool { return len(c.SequenceIDs) > 0 || len(c.Statuses) > 0 }

type customField struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Modality string `json:"modality"`
	Picklist []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"picklist_values"`
}

// customFields names the team's contact custom fields; contacts key them by id.
func (s *session) customFields(ctx context.Context) (map[string]customField, error) {
	if s.fields != nil {
		return s.fields, nil
	}
	var resp struct {
		Fields []customField `json:"typed_custom_fields"`
	}
	s.fields = map[string]customField{}
	if err := s.c.Get(ctx, "/typed_custom_fields", nil, &resp); err != nil {
		return s.fields, optional(err)
	}
	for _, f := range resp.Fields {
		if f.Modality == "" || f.Modality == "contact" {
			s.fields[f.ID] = f
		}
	}
	return s.fields, nil
}

// Leads scans the team's contacts, since Apollo's contact search cannot filter
// by sequence. A page can come back empty with a next cursor.
func (s *session) Leads(ctx context.Context, _ outreachsource.Workspace, campaignID, cursor string) ([]outreachsource.Lead, string, error) {
	page := 1
	if cursor != "" {
		n, err := strconv.Atoi(cursor)
		if err != nil || n < 1 {
			return nil, "", fmt.Errorf("%s: bad lead cursor", label)
		}
		page = n
	}
	fields, err := s.customFields(ctx)
	if err != nil {
		return nil, "", err
	}
	body := map[string]any{
		"page":           page,
		"per_page":       pageSize,
		"sort_by_field":  "contact_created_at",
		"sort_ascending": true,
	}
	var resp contactPage
	if err := s.c.Post(ctx, "/contacts/search", nil, body, &resp); err != nil {
		return nil, "", err
	}
	out := make([]outreachsource.Lead, 0, len(resp.Contacts))
	for _, c := range resp.Contacts {
		if c.Email == "" {
			continue
		}
		var st *campaignStatus
		if campaignID != "" {
			cs, ok := c.in(campaignID)
			if !ok {
				continue
			}
			st = cs
		} else if c.inAny() {
			continue
		}
		out = append(out, lead(c, st, fields))
	}
	next := ""
	if len(resp.Contacts) > 0 && page < resp.Pagination.TotalPages && page < maxPages {
		next = strconv.Itoa(page + 1)
	}
	return out, next, nil
}

func lead(c contact, st *campaignStatus, fields map[string]customField) outreachsource.Lead {
	l := outreachsource.Lead{
		ID:        c.ID,
		Email:     strings.ToLower(string(c.Email)),
		FirstName: string(c.FirstName),
		LastName:  string(c.LastName),
		Title:     string(c.Title),
		LinkedIn:  string(c.LinkedinURL),
		Timezone:  string(c.TimeZone),
		Phone:     phone(c),
		Location:  location(c),
		Custom:    map[string]string{},
		Status:    outreachsource.LeadNotStarted,
	}
	o := c.Organization
	if o == nil {
		o = c.Account
	}
	l.Company = string(c.OrganizationName)
	if o != nil {
		if l.Company == "" {
			l.Company = string(o.Name)
		}
		l.Website = string(o.WebsiteURL)
		if o.PrimaryDomain != "" {
			l.Custom["company_domain"] = string(o.PrimaryDomain)
		}
	}
	if c.Headline != "" {
		l.Custom["headline"] = string(c.Headline)
	}
	for id, v := range c.CustomFields {
		f := fields[id]
		name := f.Name
		if name == "" {
			name = id
		}
		if val := customValue(v, f); val != "" {
			l.Custom[outreachsource.FieldKey(name)] = val
		}
	}
	if len(l.Custom) == 0 {
		l.Custom = nil
	}
	if st != nil {
		l.Status, l.StepsSent = leadStatus(*st)
	}
	if c.EmailUnsubscribed {
		l.Status = outreachsource.LeadUnsubscribed
	}
	return l
}

// leadStatus reads Apollo's enrolment state. current_step_position counts every
// step type, so StepsSent includes calls and tasks the contact passed.
func leadStatus(st campaignStatus) (outreachsource.LeadStatus, int) {
	pos := 0
	if st.StepPosition != nil {
		pos = *st.StepPosition
	}
	done := pos - 1
	if done < 0 {
		done = 0
	}
	reason := strings.ToLower(string(st.InactiveReason))
	switch {
	case st.Status == "active":
		if done == 0 {
			return outreachsource.LeadNotStarted, 0
		}
		return outreachsource.LeadInProgress, done
	case strings.Contains(reason, "bounce"):
		return outreachsource.LeadBounced, pos
	case strings.Contains(reason, "unsubscri"), strings.Contains(reason, "opted out"), strings.Contains(reason, "opt out"), strings.Contains(reason, "opt-out"):
		return outreachsource.LeadUnsubscribed, pos
	case strings.Contains(reason, "meeting"), strings.Contains(reason, "demo"):
		return outreachsource.LeadMeeting, pos
	case strings.Contains(reason, "interest"):
		return outreachsource.LeadInterested, pos
	case strings.Contains(reason, "repl"):
		return outreachsource.LeadReplied, pos
	case st.Status == "finished":
		return outreachsource.LeadCompleted, pos
	}
	// paused and failed keep their place.
	return outreachsource.LeadPaused, done
}

func phone(c contact) string {
	if c.SanitizedPhone != "" {
		return string(c.SanitizedPhone)
	}
	for _, n := range c.PhoneNumbers {
		if n.SanitizedNumber != "" {
			return string(n.SanitizedNumber)
		}
		if n.RawNumber != "" {
			return string(n.RawNumber)
		}
	}
	return ""
}

func location(c contact) string {
	var parts []string
	for _, p := range []text{c.City, c.State, c.Country} {
		if p != "" {
			parts = append(parts, string(p))
		}
	}
	if len(parts) > 0 {
		return strings.Join(parts, ", ")
	}
	return string(c.PresentRawAddress)
}

// customValue renders a typed custom field value, naming picklist options.
func customValue(v any, f customField) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		for _, p := range f.Picklist {
			if p.ID == x {
				return p.Name
			}
		}
		return strings.TrimSpace(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	case []any:
		var parts []string
		for _, e := range x {
			if s := customValue(e, f); s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, ", ")
	}
	return ""
}
