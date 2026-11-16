package mailshake

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/warmbly/warmbly/internal/pkg/outreachsource"
)

type message struct {
	ID        int64   `json:"id"`
	Type      string  `json:"type"`
	Subject   *string `json:"subject"`
	ReplyToID *int64  `json:"replyToID"`
	IsPaused  bool    `json:"isPaused"`
}

func (m message) subject() string {
	if m.Subject == nil || *m.Subject == "null" {
		return "" // the docs show follow-ups with both null and "null"
	}
	return *m.Subject
}

type campaign struct {
	ID         int64     `json:"id"`
	Title      string    `json:"title"`
	Created    string    `json:"created"`
	IsArchived bool      `json:"isArchived"`
	IsPaused   bool      `json:"isPaused"`
	Messages   []message `json:"messages"`
	Sender     *sender   `json:"sender"`
}

func (c campaign) status() outreachsource.CampaignStatus {
	switch {
	case c.IsArchived:
		return outreachsource.CampaignArchived
	case c.IsPaused:
		return outreachsource.CampaignPaused
	}
	return outreachsource.CampaignActive
}

// sequence is the reply chain: the initial email, then its follow-ups in order.
func (c campaign) sequence() []message {
	var out []message
	for _, m := range c.Messages {
		if m.Type == "initial" {
			out = append(out, m)
		}
	}
	for _, m := range c.Messages {
		if m.Type == "follow-up" {
			out = append(out, m)
		}
	}
	return out
}

// campaigns lists every campaign with its messages, reading one by one only
// when the list leaves the messages out.
func (s *session) campaigns(ctx context.Context) ([]campaign, error) {
	camps, err := listAll[campaign](ctx, s.c, "/campaigns/list", nil)
	if err != nil {
		return nil, err
	}
	for i, c := range camps {
		if len(c.Messages) > 0 {
			continue
		}
		full, err := s.campaign(ctx, strconv.FormatInt(c.ID, 10))
		if err != nil {
			return nil, err
		}
		camps[i] = full
	}
	return camps, nil
}

func (s *session) campaign(ctx context.Context, id string) (campaign, error) {
	var c campaign
	err := s.c.Get(ctx, "/campaigns/get", url.Values{"campaignID": {id}}, &c)
	return c, err
}

var dialect = func() outreachsource.Dialect {
	f := outreachsource.CommonFields()
	f["first"] = "FirstName"
	f["last"] = "LastName"
	f["account"] = "Company"
	return outreachsource.Dialect{Fields: f}
}()

func (s *session) Campaigns(ctx context.Context, _ outreachsource.Workspace) ([]outreachsource.Campaign, error) {
	camps, err := s.campaigns(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]outreachsource.Campaign, 0, len(camps))
	for _, c := range camps {
		camp := outreachsource.Campaign{
			ID:          strconv.FormatInt(c.ID, 10),
			Name:        c.Title,
			Status:      c.status(),
			StopOnReply: true, // follow-ups go only to recipients who have not replied
			LeadCount:   -1,
		}
		if t, err := time.Parse(time.RFC3339, c.Created); err == nil {
			camp.CreatedAt = &t
		}
		if c.Sender != nil && c.Sender.EmailAddress != "" {
			camp.Senders = []string{c.Sender.EmailAddress}
		}
		for _, m := range c.sequence() {
			camp.Steps = append(camp.Steps, outreachsource.Step{Variants: []outreachsource.Variant{{
				Subject:  outreachsource.Translate(m.subject(), dialect).Text,
				Disabled: m.IsPaused,
			}}})
		}
		if len(camp.Steps) > 0 {
			camp.UnmappedNotes = append(camp.UnmappedNotes, "Mailshake's API does not return email bodies, follow-up delays or sending hours; write them before starting.")
		}
		for _, m := range c.Messages {
			switch m.Type {
			case "initial", "follow-up":
			case "drip":
				camp.UnmappedNotes = append(camp.UnmappedNotes, fmt.Sprintf("Drip email %q is not brought over.", m.subject()))
			case "on-click":
				camp.UnmappedNotes = append(camp.UnmappedNotes, fmt.Sprintf("On-click email %q is not brought over.", m.subject()))
			default:
				camp.UnmappedNotes = append(camp.UnmappedNotes, fmt.Sprintf("A %s message, %q, is not brought over.", strings.TrimSpace(m.Type), m.subject()))
			}
		}
		out = append(out, camp)
	}
	return out, nil
}
