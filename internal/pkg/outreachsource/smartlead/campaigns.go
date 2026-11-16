package smartlead

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	source "github.com/warmbly/warmbly/internal/pkg/outreachsource"
)

type campaign struct {
	ID            num      `json:"id"`
	Name          string   `json:"name"`
	Status        string   `json:"status"`
	CreatedAt     stamp    `json:"created_at"`
	TrackSettings []string `json:"track_settings"`
	Cron          *struct {
		TZ        string `json:"tz"`
		Days      []num  `json:"days"`
		StartHour string `json:"startHour"`
		EndHour   string `json:"endHour"`
	} `json:"scheduler_cron_value"`
	MinGap      num     `json:"min_time_btwn_emails"`
	MaxLeads    num     `json:"max_leads_per_day"`
	StopLead    string  `json:"stop_lead_settings"`
	StartTime   stamp   `json:"schedule_start_time"`
	PlainText   bool    `json:"send_as_plain_text"`
	FollowUpPct num     `json:"follow_up_percentage"`
	UnsubText   *string `json:"unsubscribe_text"`
	ParentID    num     `json:"parent_campaign_id"`
	ClientID    num     `json:"client_id"`
}

type sequence struct {
	SeqNumber num    `json:"seq_number"`
	SeqType   string `json:"seq_type"`
	Subject   string `json:"subject"`
	Body      string `json:"email_body"`
	Delay     struct {
		Days  *num `json:"delay_in_days"`
		Camel *num `json:"delayInDays"`
	} `json:"seq_delay_details"`
	Variants    []variant `json:"sequence_variants"`
	SeqVariants []variant `json:"seq_variants"`
}

type variant struct {
	Subject   string `json:"subject"`
	Body      string `json:"email_body"`
	IsDeleted bool   `json:"is_deleted"`
}

func (s *session) allCampaigns(ctx context.Context) ([]campaign, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.campOK {
		return s.campaigns, nil
	}
	var raw json.RawMessage
	if err := s.c.Get(ctx, "/campaigns/", nil, &raw); err != nil {
		return nil, err
	}
	var list []campaign
	if err := decodeList(raw, &list, "campaigns"); err != nil {
		return nil, fmt.Errorf("%s: decode campaigns: %w", label, err)
	}
	s.campaigns, s.campOK = list, true
	return list, nil
}

func (s *session) campaignsFor(ctx context.Context, ws source.Workspace) ([]campaign, error) {
	all, err := s.allCampaigns(ctx)
	if err != nil {
		return nil, err
	}
	var out []campaign
	for _, c := range all {
		ok, err := s.owns(ctx, ws, c.ClientID.String())
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, c)
		}
	}
	return out, nil
}

// leadTotal reads a campaign's lead count from a one-lead page.
func (s *session) leadTotal(ctx context.Context, campaignID string) (int, error) {
	s.mu.Lock()
	n, ok := s.leadTotals[campaignID]
	s.mu.Unlock()
	if ok {
		return n, nil
	}
	var page leadsPage
	q := url.Values{"offset": {"0"}, "limit": {"1"}}
	if err := s.c.Get(ctx, "/campaigns/"+url.PathEscape(campaignID)+"/leads", q, &page); err != nil {
		return 0, err
	}
	n = page.Total.Int()
	s.mu.Lock()
	s.leadTotals[campaignID] = n
	s.mu.Unlock()
	return n, nil
}

func (s *session) Campaigns(ctx context.Context, ws source.Workspace) ([]source.Campaign, error) {
	camps, err := s.campaignsFor(ctx, ws)
	if err != nil {
		return nil, err
	}
	names := map[string]string{}
	all, _ := s.allCampaigns(ctx)
	for _, c := range all {
		names[c.ID.String()] = c.Name
	}
	out := make([]source.Campaign, 0, len(camps))
	for _, c := range camps {
		cid := url.PathEscape(c.ID.String())
		var raw json.RawMessage
		if err := s.c.Get(ctx, "/campaigns/"+cid+"/sequences", nil, &raw); err != nil {
			return nil, err
		}
		var seqs []sequence
		if err := decodeList(raw, &seqs, "data"); err != nil {
			return nil, fmt.Errorf("%s: decode sequences: %w", label, err)
		}
		raw = nil
		if err := s.c.Get(ctx, "/campaigns/"+cid+"/email-accounts", nil, &raw); err != nil {
			return nil, err
		}
		var senders []account
		if err := decodeList(raw, &senders, "data"); err != nil {
			return nil, fmt.Errorf("%s: decode campaign email accounts: %w", label, err)
		}
		leads, err := s.leadTotal(ctx, c.ID.String())
		if err != nil {
			return nil, err
		}
		mc := mapCampaign(c, seqs, senders, names[c.ParentID.String()])
		mc.LeadCount = leads
		out = append(out, mc)
	}
	return out, nil
}

func campaignStatus(s string) source.CampaignStatus {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "ACTIVE":
		return source.CampaignActive
	case "PAUSED":
		return source.CampaignPaused
	case "STOPPED", "COMPLETED":
		return source.CampaignCompleted
	case "ARCHIVED":
		return source.CampaignArchived
	}
	return source.CampaignDraft
}

func mapCampaign(c campaign, seqs []sequence, senders []account, parentName string) source.Campaign {
	out := source.Campaign{
		ID:          c.ID.String(),
		Name:        c.Name,
		Status:      campaignStatus(c.Status),
		Schedule:    schedule(c),
		StopOnReply: !strings.EqualFold(c.StopLead, "NEVER"),
		TrackOpens:  true,
		TrackClicks: true,
		PlainText:   c.PlainText,
		Unsubscribe: c.UnsubText != nil && strings.TrimSpace(*c.UnsubText) != "",
		CreatedAt:   c.CreatedAt.ptr(),
		// Smartlead's max_leads_per_day caps new leads, not emails.
		MaxNewLeadsPerDay: max(c.MaxLeads.Int(), 0),
	}
	for _, t := range c.TrackSettings {
		switch strings.ToUpper(t) {
		case "DONT_EMAIL_OPEN":
			out.TrackOpens = false
		case "DONT_LINK_CLICK":
			out.TrackClicks = false
		}
	}
	for _, a := range senders {
		if e := strings.TrimSpace(a.FromEmail); e != "" {
			out.Senders = append(out.Senders, strings.ToLower(e))
		}
	}
	var notes []string
	if c.ParentID != "" {
		parent := parentName
		if parent == "" {
			parent = "campaign " + c.ParentID.String()
		}
		notes = append(notes, fmt.Sprintf("This is a subsequence of %q; the reply categories that moved leads into it are not brought over.", parent))
	}
	switch strings.ToUpper(c.StopLead) {
	case "OPENED_EMAIL":
		notes = append(notes, "Smartlead stopped a lead once they opened an email; Warmbly stops on reply.")
	case "CLICKED_LINK":
		notes = append(notes, "Smartlead stopped a lead once they clicked a link; Warmbly stops on reply.")
	}
	if p := c.FollowUpPct.Int(); c.FollowUpPct != "" && p < 100 {
		notes = append(notes, fmt.Sprintf("Smartlead sent follow-ups to %d%% of leads; in Warmbly every lead gets them.", p))
	}

	sort.SliceStable(seqs, func(i, j int) bool { return seqs[i].SeqNumber.Int() < seqs[j].SeqNumber.Int() })
	var dropped, kept []string
	seenDrop := map[string]bool{}
	for _, sq := range seqs {
		if t := strings.ToUpper(strings.TrimSpace(sq.SeqType)); t != "" && t != "EMAIL" {
			notes = append(notes, fmt.Sprintf("Step %s is a %s step and is not brought over.", sq.SeqNumber, strings.ToLower(t)))
			continue
		}
		step := source.Step{WaitDays: delayDays(sq)}
		vars := sq.Variants
		if len(vars) == 0 {
			vars = sq.SeqVariants
		}
		if len(vars) == 0 {
			vars = []variant{{Subject: sq.Subject, Body: sq.Body}}
		}
		for _, v := range vars {
			if v.IsDeleted {
				continue
			}
			subj, body := translate(v.Subject), translate(v.Body)
			for _, d := range append(subj.Dropped, body.Dropped...) {
				if !seenDrop[d] {
					seenDrop[d] = true
					dropped = append(dropped, d)
				}
			}
			kept = append(kept, subj.Kept...)
			kept = append(kept, body.Kept...)
			if strings.Contains(strings.ToLower(v.Body), "%unsubscribe-text%") {
				out.Unsubscribe = true
			}
			step.Variants = append(step.Variants, source.Variant{
				Subject: strings.TrimSpace(subj.Text),
				Body:    body.Text,
				HTML:    htmlRE.MatchString(v.Body),
			})
		}
		if len(step.Variants) == 0 {
			continue
		}
		// A follow-up with its own subject starts a new thread in Smartlead.
		step.NewThread = len(out.Steps) > 0 && step.Variants[0].Subject != ""
		out.Steps = append(out.Steps, step)
	}
	if len(dropped) > 0 {
		notes = append(notes, "Removed placeholders Warmbly fills itself or has no match for: "+strings.Join(dropped, ", ")+".")
	}
	if len(kept) > 0 {
		notes = append(notes, "Some conditions could not be converted and are left as written; edit them before launching: "+strings.Join(kept, ", ")+".")
	}
	out.UnmappedNotes = notes
	return out
}

var htmlRE = regexp.MustCompile(`<(?:[a-zA-Z][a-zA-Z0-9]*)(?:\s[^>]*)?/?>`)

func delayDays(sq sequence) int {
	if sq.Delay.Days != nil {
		return sq.Delay.Days.Int()
	}
	if sq.Delay.Camel != nil {
		return sq.Delay.Camel.Int()
	}
	return 0
}

func schedule(c campaign) *source.Schedule {
	if c.Cron == nil {
		return nil
	}
	sc := &source.Schedule{
		Timezone:      strings.TrimSpace(c.Cron.TZ),
		From:          clock(c.Cron.StartHour),
		To:            clock(c.Cron.EndHour),
		MinGapMinutes: c.MinGap.Int(),
		Start:         c.StartTime.ptr(),
	}
	seen := map[int]bool{}
	for _, d := range c.Cron.Days {
		// Smartlead counts 0 Sunday through 6 Saturday.
		n := d.Int()
		if d == "" || n < 0 || n > 7 {
			continue
		}
		if n == 0 {
			n = 7
		}
		if !seen[n] {
			seen[n] = true
			sc.Days = append(sc.Days, n)
		}
	}
	sort.Ints(sc.Days)
	return sc
}

// clock normalises "9:00" or "09:00:00" to "HH:MM".
func clock(s string) string {
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) < 2 {
		return ""
	}
	h, err1 := strconv.Atoi(parts[0])
	m, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || h < 0 || h > 24 || m < 0 || m > 59 {
		return ""
	}
	return fmt.Sprintf("%02d:%02d", h, m)
}
