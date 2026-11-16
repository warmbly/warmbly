package woodpecker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/warmbly/warmbly/internal/pkg/outreachsource"
)

var leadPageSize = 100 // a var so tests can page through small fixtures

type prospect struct {
	ID            int64  `json:"id"`
	Email         string `json:"email"`
	FirstName     string `json:"first_name"`
	LastName      string `json:"last_name"`
	Company       string `json:"company"`
	Industry      string `json:"industry"`
	Website       string `json:"website"`
	LinkedIn      string `json:"linkedin_url"`
	Tags          string `json:"tags"`
	Title         string `json:"title"`
	Phone         string `json:"phone"`
	Address       string `json:"address"`
	City          string `json:"city"`
	State         string `json:"state"`
	Country       string `json:"country"`
	LastContacted string `json:"last_contacted"`
	Status        string `json:"status"`
	Interested    string `json:"interested"`
	InterestLevel *struct {
		Level string `json:"level"`
	} `json:"interest_level"`
	CampaignStatus string            `json:"campaign_status"`
	SentMails      *int              `json:"sent_mails"`
	SnippetLabels  map[string]string `json:"snippet_labels"`
}

// Leads reads /v1/prospects: a campaign's enrollments with their campaign
// status, or with campaignID empty every prospect in the account.
func (s *session) Leads(ctx context.Context, ws outreachsource.Workspace, campaignID, cursor string) ([]outreachsource.Lead, string, error) {
	page := 1
	if cursor != "" {
		p, err := strconv.Atoi(cursor)
		if err != nil || p < 1 {
			return nil, "", fmt.Errorf("%s: bad leads cursor", label)
		}
		page = p
	}
	q := url.Values{"page": {strconv.Itoa(page)}, "per_page": {strconv.Itoa(leadPageSize)}}
	if campaignID != "" {
		q.Set("campaigns_id", campaignID)
	}
	var raw json.RawMessage
	if err := s.client(ws).Get(ctx, "/v1/prospects", q, &raw); err != nil {
		return nil, "", err
	}
	// An empty result is an object with a message, not an empty array.
	if !bytes.HasPrefix(bytes.TrimSpace(raw), []byte("[")) {
		return nil, "", nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, "", fmt.Errorf("%s: decode prospects: %w", label, err)
	}
	out := make([]outreachsource.Lead, 0, len(items))
	for _, it := range items {
		var p prospect
		var all map[string]any
		if err := json.Unmarshal(it, &p); err != nil {
			return nil, "", fmt.Errorf("%s: decode prospect: %w", label, err)
		}
		_ = json.Unmarshal(it, &all)
		out = append(out, toLead(p, all, campaignID != ""))
	}
	next := ""
	if len(items) == leadPageSize {
		next = strconv.Itoa(page + 1)
	}
	return out, next, nil
}

func toLead(p prospect, all map[string]any, inCampaign bool) outreachsource.Lead {
	l := outreachsource.Lead{
		ID:        strconv.FormatInt(p.ID, 10),
		Email:     strings.TrimSpace(p.Email),
		FirstName: p.FirstName,
		LastName:  p.LastName,
		Company:   p.Company,
		Title:     p.Title,
		Phone:     p.Phone,
		Website:   p.Website,
		LinkedIn:  p.LinkedIn,
		Custom:    map[string]string{},
	}
	var loc []string
	for _, part := range []string{p.City, p.State, p.Country} {
		if part = strings.TrimSpace(part); part != "" {
			loc = append(loc, part)
		}
	}
	l.Location = strings.Join(loc, ", ")
	for k, v := range map[string]string{"industry": p.Industry, "tags": p.Tags, "address": p.Address, "city": p.City, "state": p.State, "country": p.Country} {
		if v != "" {
			l.Custom[k] = v
		}
	}
	// Templates name snippets SNIPPET_1..15; labels are kept under their own name too.
	for i := 1; i <= 15; i++ {
		if v, ok := all["snippet"+strconv.Itoa(i)].(string); ok && v != "" {
			l.Custom[outreachsource.FieldKey("SNIPPET_"+strconv.Itoa(i))] = v
		}
	}
	for k, v := range p.SnippetLabels {
		if key := outreachsource.FieldKey(k); key != "" && v != "" {
			l.Custom[key] = v
		}
	}
	if len(l.Custom) == 0 {
		l.Custom = nil
	}
	sent := 0
	if p.SentMails != nil {
		sent = *p.SentMails
	}
	if inCampaign {
		// sent_mails counts this campaign's emails, one per email step.
		l.StepsSent, l.StepsExact = sent, true
	}
	// last_contacted spans every campaign the prospect is in.
	if (sent > 0 || !inCampaign) && p.LastContacted != "" {
		l.LastSent = parseTime(p.LastContacted)
	}
	l.Status = leadStatus(p, sent)
	return l
}

func leadStatus(p prospect, sent int) outreachsource.LeadStatus {
	status := strings.ToUpper(p.Status)
	switch status {
	case "BOUNCED", "INVALID":
		return outreachsource.LeadBounced
	case "BLACKLIST":
		return outreachsource.LeadUnsubscribed
	}
	level := p.Interested
	if p.InterestLevel != nil {
		level = p.InterestLevel.Level
	}
	switch strings.ToUpper(strings.ReplaceAll(level, "-", "_")) {
	case "INTERESTED":
		return outreachsource.LeadInterested
	case "NOT_INTERESTED":
		return outreachsource.LeadNotInterest
	case "MAYBE_LATER":
		return outreachsource.LeadReplied
	}
	switch status {
	case "REPLIED":
		return outreachsource.LeadReplied
	case "PAUSED", "TO-CHECK", "TO-REVIEW":
		return outreachsource.LeadPaused
	}
	if sent > 0 || (p.LastContacted != "" && p.CampaignStatus == "") {
		if strings.EqualFold(p.CampaignStatus, "COMPLETED") {
			return outreachsource.LeadCompleted
		}
		return outreachsource.LeadInProgress
	}
	return outreachsource.LeadNotStarted
}
