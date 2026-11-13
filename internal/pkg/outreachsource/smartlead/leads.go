package smartlead

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	source "github.com/warmbly/warmbly/internal/pkg/outreachsource"
)

type category struct {
	ID        num     `json:"id"`
	Name      string  `json:"name"`
	Sentiment *string `json:"sentiment_type"`
}

func (s *session) loadCategories(ctx context.Context) error {
	var raw json.RawMessage
	if err := s.c.Get(ctx, "/leads/fetch-categories", nil, &raw); err != nil {
		return err
	}
	var list []category
	if err := decodeList(raw, &list, "data"); err != nil {
		return fmt.Errorf("%s: decode lead categories: %w", label, err)
	}
	s.categories = make(map[string]category, len(list))
	for _, c := range list {
		s.categories[c.ID.String()] = c
	}
	return nil
}

type leadsPage struct {
	Total num       `json:"total_leads"`
	Data  []leadRow `json:"data"`
}

type leadRow struct {
	CategoryID num    `json:"lead_category_id"`
	Status     string `json:"status"`
	Lead       struct {
		ID         num                        `json:"id"`
		Email      string                     `json:"email"`
		FirstName  string                     `json:"first_name"`
		LastName   string                     `json:"last_name"`
		Phone      string                     `json:"phone_number"`
		Company    string                     `json:"company_name"`
		Website    string                     `json:"website"`
		Location   string                     `json:"location"`
		LinkedIn   string                     `json:"linkedin_profile"`
		CompanyURL string                     `json:"company_url"`
		Custom     map[string]json.RawMessage `json:"custom_fields"`
		Unsub      bool                       `json:"is_unsubscribed"`
		UnsubMap   map[string]json.RawMessage `json:"unsubscribed_client_id_map"`
	} `json:"lead"`
}

// progress is what a campaign sent one lead, from its email-level statistics.
type progress struct {
	steps    int
	last     time.Time
	replied  bool
	bounced  bool
	unsubbed bool
}

// sent is a campaign's statistics; perLead is false when they did not come
// back as one row per sent email, so step counts are not exact.
type sent struct {
	leads   map[string]progress
	perLead bool
}

type statRow struct {
	Email        string `json:"lead_email"`
	Seq          num    `json:"sequence_number"`
	SentTime     stamp  `json:"sent_time"`
	Replied      bool   `json:"is_replied"`
	Bounced      bool   `json:"is_bounced"`
	SenderBounce bool   `json:"sender_bounce"`
	Unsubscribed bool   `json:"is_unsubscribed"`
}

const (
	leadPage = 100
	statPage = 1000
)

func (s *session) Leads(ctx context.Context, ws source.Workspace, campaignID, cursor string) ([]source.Lead, string, error) {
	if campaignID == "" {
		return nil, "", fmt.Errorf("%s: leads outside a campaign: %w", label, source.ErrUnsupported)
	}
	off, err := offset(cursor)
	if err != nil {
		return nil, "", err
	}
	var page leadsPage
	q := url.Values{"offset": {strconv.Itoa(off)}, "limit": {strconv.Itoa(leadPage)}}
	if err := s.c.Get(ctx, "/campaigns/"+url.PathEscape(campaignID)+"/leads", q, &page); err != nil {
		return nil, "", err
	}
	prog, err := s.campaignProgress(ctx, campaignID)
	if err != nil {
		return nil, "", err
	}
	out := make([]source.Lead, 0, len(page.Data))
	for _, r := range page.Data {
		if l, ok := s.mapLead(r, ws, prog); ok {
			out = append(out, l)
		}
	}
	next := ""
	end := off + len(page.Data)
	if total := page.Total.Int(); len(page.Data) > 0 && (end < total || (page.Total == "" && len(page.Data) == leadPage)) {
		next = strconv.Itoa(end)
	}
	if next == "" {
		s.mu.Lock()
		delete(s.progress, campaignID)
		s.mu.Unlock()
	}
	return out, next, nil
}

// campaignProgress pages a campaign's sent-email statistics once, so every
// lead carries how far it got and how it answered.
func (s *session) campaignProgress(ctx context.Context, campaignID string) (sent, error) {
	s.mu.Lock()
	cached, ok := s.progress[campaignID]
	s.mu.Unlock()
	if ok {
		return cached, nil
	}
	p := map[string]progress{}
	perLead := true
	for off := 0; ; off += statPage {
		var body struct {
			Data json.RawMessage `json:"data"`
		}
		q := url.Values{"offset": {strconv.Itoa(off)}, "limit": {strconv.Itoa(statPage)}}
		if err := s.c.Get(ctx, "/campaigns/"+url.PathEscape(campaignID)+"/statistics", q, &body); err != nil {
			return sent{}, err
		}
		var rows []statRow
		if d := bytes.TrimSpace(body.Data); len(d) > 0 && d[0] == '[' {
			if err := json.Unmarshal(d, &rows); err != nil {
				return sent{}, fmt.Errorf("%s: decode campaign statistics: %w", label, err)
			}
		} else {
			perLead = false
		}
		for _, r := range rows {
			key := strings.ToLower(strings.TrimSpace(r.Email))
			if key == "" || r.Seq.Int() < 1 {
				perLead = false
				continue
			}
			cur := p[key]
			if n := r.Seq.Int(); n > cur.steps {
				cur.steps = n
			}
			if r.SentTime.After(cur.last) {
				cur.last = r.SentTime.Time
			}
			cur.replied = cur.replied || r.Replied
			cur.bounced = cur.bounced || (r.Bounced && !r.SenderBounce)
			cur.unsubbed = cur.unsubbed || r.Unsubscribed
			p[key] = cur
		}
		if len(rows) < statPage {
			break
		}
	}
	out := sent{leads: p, perLead: perLead}
	s.mu.Lock()
	s.progress[campaignID] = out
	s.mu.Unlock()
	return out, nil
}

func (s *session) mapLead(r leadRow, ws source.Workspace, prog sent) (source.Lead, bool) {
	l := r.Lead
	email := strings.ToLower(strings.TrimSpace(l.Email))
	if email == "" {
		return source.Lead{}, false
	}
	out := source.Lead{
		ID:        l.ID.String(),
		Email:     email,
		FirstName: strings.TrimSpace(l.FirstName),
		LastName:  strings.TrimSpace(l.LastName),
		Company:   strings.TrimSpace(l.Company),
		Phone:     strings.TrimSpace(l.Phone),
		Website:   firstOf(l.Website, l.CompanyURL),
		LinkedIn:  strings.TrimSpace(l.LinkedIn),
		Location:  strings.TrimSpace(l.Location),
	}
	// A custom field whose token means a lead field fills that field, so
	// templates and stored values agree on one key.
	leadFields := map[string]*string{"website": &out.Website, "linkedin": &out.LinkedIn, "location": &out.Location, "title": &out.Title, "timezone": &out.Timezone}
	custom := map[string]string{}
	for _, k := range sortedRawKeys(l.Custom) {
		key := source.FieldKey(k)
		v := rawString(l.Custom[k])
		if key == "" || v == "" {
			continue
		}
		if dst, ok := leadFields[dialect.Fields[key]]; ok {
			if *dst == "" {
				*dst = v
			}
			continue
		}
		custom[key] = v
	}
	if len(custom) > 0 {
		out.Custom = custom
	}
	p := prog.leads[email]
	out.StepsSent = p.steps
	out.StepsExact = prog.perLead && stepsAgree(r.Status, p.steps)
	if !p.last.IsZero() {
		t := p.last
		out.LastSent = &t
	}
	unsub := l.Unsub || p.unsubbed || (ws.ID != "" && truthy(l.UnsubMap[ws.ID]))
	out.Status = s.leadStatus(r, p, unsub)
	return out, true
}

func (s *session) leadStatus(r leadRow, p progress, unsub bool) source.LeadStatus {
	status := strings.ToUpper(strings.TrimSpace(r.Status))
	switch {
	case unsub, status == "BLOCKED":
		return source.LeadUnsubscribed
	case p.bounced:
		return source.LeadBounced
	}
	if c, ok := s.categories[r.CategoryID.String()]; ok && r.CategoryID != "" {
		if st, ok := categoryStatus(c); ok {
			return st
		}
	}
	if p.replied {
		return source.LeadReplied
	}
	switch status {
	case "STARTED":
		return source.LeadNotStarted
	case "INPROGRESS":
		return source.LeadInProgress
	case "PAUSED":
		return source.LeadPaused
	case "COMPLETED", "STOPPED":
		// Stopped is final: a stop condition or a person ended the sequence.
		return source.LeadCompleted
	}
	if p.steps > 0 {
		return source.LeadInProgress
	}
	return source.LeadNotStarted
}

// categoryStatus maps a reply category; ok is false for one that says
// nothing about the lead, such as an out-of-office.
func categoryStatus(c category) (source.LeadStatus, bool) {
	switch source.FieldKey(c.Name) {
	case "interested":
		return source.LeadInterested, true
	case "meeting_request", "meeting_booked":
		return source.LeadMeeting, true
	case "not_interested":
		return source.LeadNotInterest, true
	case "do_not_contact":
		return source.LeadUnsubscribed, true
	case "information_request", "wrong_person":
		return source.LeadReplied, true
	case "out_of_office", "sender_originated_bounce":
		return "", false
	}
	if c.Sentiment != nil {
		switch strings.ToLower(*c.Sentiment) {
		case "positive":
			return source.LeadInterested, true
		case "negative":
			return source.LeadNotInterest, true
		}
	}
	return source.LeadReplied, true
}

func firstOf(vals ...string) string {
	for _, v := range vals {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}

// sortedRawKeys orders custom fields so a duplicate resolves the same way every time.
func sortedRawKeys(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func rawString(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s)
	}
	return string(raw)
}

func truthy(raw json.RawMessage) bool {
	v := rawString(raw)
	return v != "" && v != "false" && v != "0"
}

// stepsAgree reports whether the step count fits the lead's own status, so a
// lead the statistics missed is never continued as if it were untouched.
func stepsAgree(status string, steps int) bool {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "STARTED":
		return steps == 0
	case "INPROGRESS", "PAUSED", "COMPLETED", "STOPPED":
		return steps > 0
	}
	return false
}
