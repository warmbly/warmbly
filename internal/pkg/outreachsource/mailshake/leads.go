package mailshake

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/warmbly/warmbly/internal/pkg/outreachsource"
)

type recipient struct {
	ID           int64          `json:"id"`
	EmailAddress string         `json:"emailAddress"`
	FullName     string         `json:"fullName"`
	First        string         `json:"first"`
	Last         string         `json:"last"`
	IsPaused     bool           `json:"isPaused"`
	Fields       map[string]any `json:"fields"`
}

type withRecipient struct {
	Recipient recipient `json:"recipient"`
	Status    string    `json:"status"`
}

// progress is where a campaign's recipients stand. The recipient list carries
// only isPaused, so the rest comes from filtered lists read once per campaign.
type progress struct {
	steps   int
	sent    map[int64]int // highest sequence step sent, 1-based
	replied map[int64]bool
	bounced map[int64]bool
	unsub   map[int64]bool
	lead    map[int64]string // Lead Catcher status: open, closed (won), lost, ignored
}

func (s *session) progressFor(ctx context.Context, campaignID string) (*progress, error) {
	s.mu.Lock()
	p := s.progress[campaignID]
	s.mu.Unlock()
	if p != nil {
		return p, nil
	}
	id, err := strconv.ParseInt(campaignID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("%s: bad campaign id", label)
	}
	camp, err := s.campaign(ctx, campaignID)
	if err != nil {
		return nil, err
	}
	seq := camp.sequence()
	p = &progress{steps: len(seq), sent: map[int64]int{}, unsub: map[int64]bool{}, lead: map[int64]string{}}
	for i, m := range seq {
		ids, err := s.filtered(ctx, id, map[string]any{"action": "wasSent", "campaignMessageID": m.ID})
		if err != nil {
			return nil, err
		}
		for rid := range ids {
			if p.sent[rid] < i+1 {
				p.sent[rid] = i + 1
			}
		}
	}
	if p.replied, err = s.filtered(ctx, id, map[string]any{"action": "replied"}); err != nil {
		return nil, err
	}
	if p.bounced, err = s.filtered(ctx, id, map[string]any{"action": "bounced"}); err != nil {
		return nil, err
	}
	// Recipients unsubscribed by their reply; the team's unsubscribe list is not readable.
	unsub, err := listAll[withRecipient](ctx, s.c, "/activity/replies", url.Values{"campaignID": {campaignID}, "replyType": {"unsubscribe"}, "perPage": {"25"}})
	if err != nil {
		return nil, err
	}
	for _, r := range unsub {
		p.unsub[r.Recipient.ID] = true
	}
	leads, err := listAll[withRecipient](ctx, s.c, "/leads/list", url.Values{"campaignID": {campaignID}})
	if err != nil {
		return nil, err
	}
	for _, l := range leads {
		p.lead[l.Recipient.ID] = strings.ToLower(l.Status)
	}
	s.mu.Lock()
	s.progress[campaignID] = p
	s.mu.Unlock()
	return p, nil
}

// filtered collects the ids of a campaign's recipients matching a documented
// filter, which recipients/list takes as a JSON POST.
func (s *session) filtered(ctx context.Context, campaignID int64, filter map[string]any) (map[int64]bool, error) {
	out := map[int64]bool{}
	token := ""
	for {
		body := map[string]any{"campaignID": campaignID, "filter": filter, "perPage": pageSize}
		if token != "" {
			body["nextToken"] = token
		}
		var pg page[recipient]
		if err := s.c.Post(ctx, "/recipients/list", nil, body, &pg); err != nil {
			return nil, err
		}
		for _, r := range pg.Results {
			out[r.ID] = true
		}
		if pg.NextToken == nil || *pg.NextToken == "" || len(pg.Results) == 0 {
			return out, nil
		}
		token = *pg.NextToken
	}
}

// Leads pages through a campaign's recipients. Every Mailshake recipient sits
// in a campaign, so there are none outside one.
func (s *session) Leads(ctx context.Context, _ outreachsource.Workspace, campaignID, cursor string) ([]outreachsource.Lead, string, error) {
	if campaignID == "" {
		return nil, "", nil
	}
	p, err := s.progressFor(ctx, campaignID)
	if err != nil {
		return nil, "", err
	}
	recips, next, err := list[recipient](ctx, s.c, "/recipients/list", url.Values{"campaignID": {campaignID}}, cursor)
	if err != nil {
		return nil, "", err
	}
	out := make([]outreachsource.Lead, 0, len(recips))
	for _, r := range recips {
		out = append(out, toLead(r, p))
	}
	return out, next, nil
}

func toLead(r recipient, p *progress) outreachsource.Lead {
	l := outreachsource.Lead{
		ID:        strconv.FormatInt(r.ID, 10),
		Email:     strings.TrimSpace(r.EmailAddress),
		FirstName: r.First,
		LastName:  r.Last,
		Custom:    map[string]string{},
		StepsSent: p.sent[r.ID],
		// Read per message from the wasSent filter, not estimated.
		StepsExact: true,
	}
	if r.FullName != "" {
		l.Custom["name"] = r.FullName // Mailshake's {{name}} text replacement
	}
	for k, v := range r.Fields {
		key := outreachsource.FieldKey(k)
		val := strings.TrimSpace(fmt.Sprint(v))
		if key == "" || v == nil || val == "" {
			continue
		}
		switch key {
		case "account", "company", "company_name":
			l.Company = val
		case "phone", "phone_number":
			l.Phone = val
		case "linked_in_url", "linkedin_url", "linkedin":
			l.LinkedIn = val
		case "title", "job_title":
			l.Title = val
		case "website":
			l.Website = val
		case "location":
			l.Location = val
		case "timezone":
			l.Timezone = val
		default:
			l.Custom[key] = val
		}
	}
	if len(l.Custom) == 0 {
		l.Custom = nil
	}
	l.Status = recipientStatus(r, p)
	return l
}

func recipientStatus(r recipient, p *progress) outreachsource.LeadStatus {
	switch {
	case p.bounced[r.ID]:
		return outreachsource.LeadBounced
	case p.unsub[r.ID]:
		return outreachsource.LeadUnsubscribed
	}
	switch p.lead[r.ID] {
	case "open", "closed":
		return outreachsource.LeadInterested
	case "lost":
		return outreachsource.LeadNotInterest
	}
	switch {
	case p.replied[r.ID]:
		return outreachsource.LeadReplied
	case r.IsPaused:
		return outreachsource.LeadPaused
	case p.steps > 0 && p.sent[r.ID] >= p.steps:
		return outreachsource.LeadCompleted
	case p.sent[r.ID] > 0:
		return outreachsource.LeadInProgress
	}
	return outreachsource.LeadNotStarted
}
