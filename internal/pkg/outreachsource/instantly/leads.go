package instantly

import (
	"context"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/warmbly/warmbly/internal/pkg/outreachsource"
)

type account struct {
	Email        string   `json:"email"`
	FirstName    *string  `json:"first_name"`
	LastName     *string  `json:"last_name"`
	DailyLimit   *float64 `json:"daily_limit"`
	WarmupStatus *float64 `json:"warmup_status"`
	ProviderCode float64  `json:"provider_code"`
	Signature    *string  `json:"signature"`
	ReplyTo      *string  `json:"reply_to"`
	Tags         []struct {
		Label string `json:"label"`
	} `json:"tags"`
}

func (s *session) listAccounts(ctx context.Context, ws outreachsource.Workspace, extra url.Values) ([]account, error) {
	api := s.api(ws)
	var out []account
	cursor := ""
	for {
		q := url.Values{"limit": {strconv.Itoa(pageSize)}}
		for k, v := range extra {
			q[k] = v
		}
		if cursor != "" {
			q.Set("starting_after", cursor)
		}
		var page struct {
			Items []account `json:"items"`
			Next  string    `json:"next_starting_after"`
		}
		if err := api.Get(ctx, "/accounts", q, &page); err != nil {
			return nil, err
		}
		out = append(out, page.Items...)
		if cursor = nextCursor(len(page.Items), page.Next); cursor == "" {
			return out, nil
		}
	}
}

// Mailboxes never carry secrets: Instantly's account object has no SMTP or IMAP fields.
func (s *session) Mailboxes(ctx context.Context, ws outreachsource.Workspace) ([]outreachsource.Mailbox, error) {
	accts, err := s.listAccounts(ctx, ws, url.Values{"include_tags": {"true"}})
	if err != nil {
		return nil, err
	}
	out := make([]outreachsource.Mailbox, 0, len(accts))
	for _, a := range accts {
		m := outreachsource.Mailbox{
			ID:        a.Email,
			Email:     strings.ToLower(strings.TrimSpace(a.Email)),
			FromName:  strings.TrimSpace(str(a.FirstName) + " " + str(a.LastName)),
			Kind:      mailboxKind(a.ProviderCode),
			Signature: str(a.Signature),
			ReplyTo:   str(a.ReplyTo),
		}
		if a.DailyLimit != nil {
			m.DailyCap = int(*a.DailyLimit)
		}
		if a.WarmupStatus != nil {
			on := int(*a.WarmupStatus) == 1
			m.Warmup = &on
		}
		for _, t := range a.Tags {
			if t.Label != "" {
				m.Tags = append(m.Tags, t.Label)
			}
		}
		out = append(out, m)
	}
	return out, nil
}

func mailboxKind(code float64) outreachsource.MailboxKind {
	switch int(code) {
	case 2:
		return outreachsource.MailboxGoogle
	case 3:
		return outreachsource.MailboxMicrosoft
	default: // custom IMAP/SMTP, AWS, AirMail
		return outreachsource.MailboxSMTP
	}
}

type lead struct {
	ID            string         `json:"id"`
	Email         *string        `json:"email"`
	FirstName     *string        `json:"first_name"`
	LastName      *string        `json:"last_name"`
	CompanyName   *string        `json:"company_name"`
	CompanyDomain *string        `json:"company_domain"`
	JobTitle      *string        `json:"job_title"`
	Phone         *string        `json:"phone"`
	Website       *string        `json:"website"`
	Personal      *string        `json:"personalization"`
	Payload       map[string]any `json:"payload"`
	Status        float64        `json:"status"`
	Interest      *float64       `json:"lt_interest_status"`
	Replies       float64        `json:"email_reply_count"`
	LastStepID    *string        `json:"last_step_id"`
	LastContact   *string        `json:"timestamp_last_contact"`
}

func (s *session) Leads(ctx context.Context, ws outreachsource.Workspace, campaignID, cursor string) ([]outreachsource.Lead, string, error) {
	body := map[string]any{"limit": pageSize}
	if campaignID != "" {
		body["campaign"] = campaignID
	} else {
		body["in_campaign"] = false
	}
	if cursor != "" {
		body["starting_after"] = cursor
	}
	var page struct {
		Items []lead `json:"items"`
		Next  string `json:"next_starting_after"`
	}
	if err := s.api(ws).Post(ctx, "/leads/list", nil, body, &page); err != nil {
		return nil, "", err
	}
	steps := 0
	if campaignID != "" {
		n, err := s.stepCount(ctx, ws, campaignID)
		if err != nil {
			return nil, "", err
		}
		steps = n
	}
	out := make([]outreachsource.Lead, 0, len(page.Items))
	for _, l := range page.Items {
		out = append(out, mapLead(l, steps))
	}
	return out, nextCursor(len(page.Items), page.Next), nil
}

// stepCount is a campaign's email steps, read once per session.
func (s *session) stepCount(ctx context.Context, ws outreachsource.Workspace, id string) (int, error) {
	s.mu.Lock()
	n, ok := s.steps[id]
	s.mu.Unlock()
	if ok {
		return n, nil
	}
	var c campaign
	if err := s.api(ws).Get(ctx, "/campaigns/"+url.PathEscape(id), nil, &c); err != nil {
		return 0, err
	}
	n = len(mapCampaign(c).Steps)
	s.mu.Lock()
	s.steps[id] = n
	s.mu.Unlock()
	return n, nil
}

func mapLead(l lead, steps int) outreachsource.Lead {
	out := outreachsource.Lead{
		ID:        l.ID,
		Email:     strings.ToLower(str(l.Email)),
		FirstName: str(l.FirstName),
		LastName:  str(l.LastName),
		Company:   str(l.CompanyName),
		Title:     str(l.JobTitle),
		Phone:     str(l.Phone),
		Website:   str(l.Website),
		LastSent:  parseTime(l.LastContact),
		Custom:    map[string]string{},
	}
	keys := make([]string, 0, len(l.Payload))
	for k := range l.Payload {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if v, ok := scalar(l.Payload[k]); ok && v != "" {
			out.Custom[outreachsource.FieldKey(k)] = v
		}
	}
	// Instantly's own variables Warmbly has no template field for stay reachable by name.
	for k, v := range map[string]string{
		"personalization": str(l.Personal), "company_domain": str(l.CompanyDomain),
	} {
		if v != "" {
			out.Custom[k] = v
		}
	}
	if len(out.Custom) == 0 {
		out.Custom = nil
	}
	out.Status = leadStatus(l)
	contacted := out.LastSent != nil || str(l.LastStepID) != ""
	switch {
	case int(l.Status) == 3:
		out.StepsSent = steps
	case contacted:
		// Instantly names the last step by an id its campaign object does not carry.
		out.StepsSent = 1
	}
	return out
}

func leadStatus(l lead) outreachsource.LeadStatus {
	switch int(l.Status) {
	case -1:
		return outreachsource.LeadBounced
	case -2:
		return outreachsource.LeadUnsubscribed
	}
	if l.Interest != nil {
		switch int(*l.Interest) {
		case 1:
			return outreachsource.LeadInterested
		case 2, 3, 4, -4: // booked, completed, won, no show
			return outreachsource.LeadMeeting
		case -1, -2, -3: // not interested, wrong person, lost
			return outreachsource.LeadNotInterest
		}
	}
	if l.Replies > 0 {
		return outreachsource.LeadReplied
	}
	switch int(l.Status) {
	case 3:
		return outreachsource.LeadCompleted
	case 2, -3: // paused, skipped by Instantly
		return outreachsource.LeadPaused
	}
	if l.LastContact != nil || str(l.LastStepID) != "" {
		return outreachsource.LeadInProgress
	}
	return outreachsource.LeadNotStarted
}

func scalar(v any) (string, bool) {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t), true
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64), true
	case bool:
		return strconv.FormatBool(t), true
	}
	return "", false
}

type blockEntry struct {
	Value    string `json:"bl_value"`
	IsDomain bool   `json:"is_domain"`
}

func (s *session) Blocklist(ctx context.Context, ws outreachsource.Workspace, cursor string) ([]outreachsource.BlockEntry, string, error) {
	q := url.Values{"limit": {strconv.Itoa(pageSize)}}
	if cursor != "" {
		q.Set("starting_after", cursor)
	}
	var page struct {
		Items []blockEntry `json:"items"`
		Next  string       `json:"next_starting_after"`
	}
	if err := s.api(ws).Get(ctx, "/block-lists-entries", q, &page); err != nil {
		return nil, "", err
	}
	out := make([]outreachsource.BlockEntry, 0, len(page.Items))
	for _, e := range page.Items {
		v := strings.ToLower(strings.TrimSpace(e.Value))
		if v == "" {
			continue
		}
		out = append(out, outreachsource.BlockEntry{Value: v, Domain: e.IsDomain})
	}
	return out, nextCursor(len(page.Items), page.Next), nil
}
