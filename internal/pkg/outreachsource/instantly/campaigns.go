package instantly

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/warmbly/warmbly/internal/pkg/outreachsource"
)

type campaign struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Status   float64 `json:"status"`
	Schedule struct {
		StartDate *string `json:"start_date"`
		EndDate   *string `json:"end_date"`
		Schedules []struct {
			Name   string `json:"name"`
			Timing struct {
				From string `json:"from"`
				To   string `json:"to"`
			} `json:"timing"`
			Days     map[string]bool `json:"days"`
			Timezone string          `json:"timezone"`
		} `json:"schedules"`
	} `json:"campaign_schedule"`
	Sequences []struct {
		Steps []step `json:"steps"`
	} `json:"sequences"`
	Created            *string  `json:"timestamp_created"`
	EmailGap           *float64 `json:"email_gap"`
	TextOnly           *bool    `json:"text_only"`
	FirstEmailTextOnly *bool    `json:"first_email_text_only"`
	EmailList          []string `json:"email_list"`
	EmailTagList       []string `json:"email_tag_list"`
	DailyLimit         *float64 `json:"daily_limit"`
	StopOnReply        *bool    `json:"stop_on_reply"`
	LinkTracking       *bool    `json:"link_tracking"`
	OpenTracking       *bool    `json:"open_tracking"`
	StopForCompany     *bool    `json:"stop_for_company"`
	DailyMaxLeads      *float64 `json:"daily_max_leads"`
	Unsubscribe        *bool    `json:"insert_unsubscribe_header"`
	CC                 []string `json:"cc_list"`
	BCC                []string `json:"bcc_list"`
}

type step struct {
	Type      string  `json:"type"`
	Delay     float64 `json:"delay"`
	DelayUnit string  `json:"delay_unit"`
	Variants  []struct {
		Subject  string `json:"subject"`
		Body     string `json:"body"`
		Disabled bool   `json:"v_disabled"`
	} `json:"variants"`
}

// emailSteps is the campaign's one sequence; Instantly reads only the first.
func (c campaign) emailSteps() []step {
	if len(c.Sequences) == 0 {
		return nil
	}
	var out []step
	for _, s := range c.Sequences[0].Steps {
		if s.Type == "" || s.Type == "email" {
			out = append(out, s)
		}
	}
	return out
}

// delayMinutes is a step's wait before the next one; delay_unit defaults to days.
func (s step) delayMinutes() int {
	switch s.DelayUnit {
	case "minutes":
		return int(s.Delay)
	case "hours":
		return int(s.Delay * 60)
	default:
		return int(s.Delay * 1440)
	}
}

func campaignStatus(code float64) outreachsource.CampaignStatus {
	switch int(code) {
	case 0:
		return outreachsource.CampaignDraft
	case 1, 4: // 4 is running subsequences
		return outreachsource.CampaignActive
	case 3:
		return outreachsource.CampaignCompleted
	default: // paused, or stopped by Instantly: suspended, unhealthy accounts, bounce protect
		return outreachsource.CampaignPaused
	}
}

func dialect() outreachsource.Dialect {
	f := outreachsource.CommonFields()
	f["sending_account_name"] = "Sender.Name"
	f["sending_account_first_name"] = "Sender.FirstName"
	f["sending_account_email"] = "Sender.Email"
	f["account_signature"] = ""
	return outreachsource.Dialect{Fields: f, Spin: []string{"RANDOM"}}
}

var (
	liquidRE   = regexp.MustCompile(`\{%|\{#`)
	leftoverRE = regexp.MustCompile(`\{\{\s*[^.\s}]`)
)

func (s *session) listCampaigns(ctx context.Context, ws outreachsource.Workspace) ([]campaign, error) {
	api := s.api(ws)
	var out []campaign
	cursor := ""
	for {
		q := url.Values{"limit": {strconv.Itoa(pageSize)}}
		if cursor != "" {
			q.Set("starting_after", cursor)
		}
		var page struct {
			Items []campaign `json:"items"`
			Next  string     `json:"next_starting_after"`
		}
		if err := api.Get(ctx, "/campaigns", q, &page); err != nil {
			return nil, err
		}
		out = append(out, page.Items...)
		if cursor = nextCursor(len(page.Items), page.Next); cursor == "" {
			return out, nil
		}
	}
}

// leadCounts is every campaign's total leads from one analytics read.
func (s *session) leadCounts(ctx context.Context, ws outreachsource.Workspace) (map[string]int, error) {
	var rows []struct {
		CampaignID string  `json:"campaign_id"`
		Leads      float64 `json:"leads_count"`
	}
	if err := s.api(ws).Get(ctx, "/campaigns/analytics", nil, &rows); err != nil {
		return nil, err
	}
	out := make(map[string]int, len(rows))
	for _, r := range rows {
		out[r.CampaignID] = int(r.Leads)
	}
	return out, nil
}

func (s *session) Campaigns(ctx context.Context, ws outreachsource.Workspace) ([]outreachsource.Campaign, error) {
	raw, err := s.listCampaigns(ctx, ws)
	if err != nil {
		return nil, err
	}
	leads, err := s.leadCounts(ctx, ws)
	if err != nil {
		return nil, err
	}
	out := make([]outreachsource.Campaign, 0, len(raw))
	for _, c := range raw {
		senders, err := s.senders(ctx, ws, c)
		if err != nil {
			return nil, err
		}
		mc := mapCampaign(c)
		mc.Senders = senders
		mc.LeadCount = leads[c.ID]
		s.mu.Lock()
		s.steps[c.ID] = len(mc.Steps)
		s.mu.Unlock()
		out = append(out, mc)
	}
	return out, nil
}

// senders joins the campaign's listed mailboxes with those picked by sender tag.
func (s *session) senders(ctx context.Context, ws outreachsource.Workspace, c campaign) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	add := func(e string) {
		e = strings.ToLower(strings.TrimSpace(e))
		if e != "" && !seen[e] {
			seen[e] = true
			out = append(out, e)
		}
	}
	for _, e := range c.EmailList {
		add(e)
	}
	if len(c.EmailTagList) > 0 {
		tags := append([]string(nil), c.EmailTagList...)
		sort.Strings(tags)
		key := ws.ID + "|" + strings.Join(tags, ",")
		s.mu.Lock()
		emails, ok := s.tagged[key]
		s.mu.Unlock()
		if !ok {
			accts, err := s.listAccounts(ctx, ws, url.Values{"tag_ids": {strings.Join(tags, ",")}})
			if err != nil {
				return nil, err
			}
			for _, a := range accts {
				emails = append(emails, a.Email)
			}
			s.mu.Lock()
			s.tagged[key] = emails
			s.mu.Unlock()
		}
		for _, e := range emails {
			add(e)
		}
	}
	return out, nil
}

func mapCampaign(c campaign) outreachsource.Campaign {
	out := outreachsource.Campaign{
		ID:          c.ID,
		Name:        c.Name,
		Status:      campaignStatus(c.Status),
		StopOnReply: c.StopOnReply == nil || *c.StopOnReply,
		TrackOpens:  c.OpenTracking != nil && *c.OpenTracking,
		TrackClicks: c.LinkTracking != nil && *c.LinkTracking,
		PlainText:   c.TextOnly != nil && *c.TextOnly,
		Unsubscribe: c.Unsubscribe != nil && *c.Unsubscribe,
		CreatedAt:   parseTime(c.Created),
	}
	if c.DailyLimit != nil {
		out.DailyLimit = int(*c.DailyLimit)
	}
	d := dialect()
	dropped := map[string]bool{}
	var liquid, leftover bool
	translate := func(text string) string {
		t := outreachsource.Translate(text, d)
		for _, name := range t.Dropped {
			dropped[name] = true
		}
		liquid = liquid || liquidRE.MatchString(t.Text)
		leftover = leftover || leftoverRE.MatchString(strings.ReplaceAll(t.Text, "{{unsubscribe_link}}", ""))
		return t.Text
	}
	if len(c.Sequences) > 0 {
		for _, s := range c.Sequences[0].Steps {
			if s.Type != "" && s.Type != "email" {
				out.UnmappedNotes = append(out.UnmappedNotes, fmt.Sprintf("A %q step was left out because Warmbly sends email steps only.", s.Type))
			}
		}
	}
	prevDelay := 0
	for i, s := range c.emailSteps() {
		st := outreachsource.Step{WaitDays: prevDelay / 1440, WaitMinutes: prevDelay % 1440}
		prevDelay = s.delayMinutes()
		for _, v := range s.Variants {
			st.Variants = append(st.Variants, outreachsource.Variant{
				Subject:  translate(v.Subject),
				Body:     translate(v.Body),
				HTML:     true,
				Disabled: v.Disabled,
			})
		}
		if len(st.Variants) == 0 {
			continue
		}
		st.NewThread = i > 0 && strings.TrimSpace(st.Variants[0].Subject) != ""
		out.Steps = append(out.Steps, st)
	}
	out.Schedule = mapSchedule(c, &out)
	if len(dropped) > 0 {
		out.UnmappedNotes = append(out.UnmappedNotes, "The {{accountSignature}} token was removed because Warmbly adds each mailbox's signature itself.")
	}
	if liquid {
		out.UnmappedNotes = append(out.UnmappedNotes, "Liquid conditions ({% %}) were copied as written and need rewriting in Warmbly's template syntax.")
	}
	if leftover {
		out.UnmappedNotes = append(out.UnmappedNotes, "Some nested spintax or chained fallbacks could not be translated and need checking.")
	}
	if c.FirstEmailTextOnly != nil && *c.FirstEmailTextOnly && !out.PlainText {
		out.UnmappedNotes = append(out.UnmappedNotes, "The first email was sent as plain text in Instantly; Warmbly sends it like the other steps.")
	}
	if c.StopForCompany != nil && *c.StopForCompany {
		out.UnmappedNotes = append(out.UnmappedNotes, "Stopping the whole company when one lead replies was not brought over.")
	}
	if c.DailyMaxLeads != nil && *c.DailyMaxLeads > 0 {
		out.MaxNewLeadsPerDay = int(*c.DailyMaxLeads)
	}
	if len(c.CC) > 0 || len(c.BCC) > 0 {
		out.UnmappedNotes = append(out.UnmappedNotes, "CC and BCC addresses were not brought over.")
	}
	return out
}

// mapSchedule reads the first schedule; Instantly keys days 0 (Sunday) to 6.
func mapSchedule(c campaign, out *outreachsource.Campaign) *outreachsource.Schedule {
	cs := c.Schedule
	if len(cs.Schedules) == 0 {
		return nil
	}
	if len(cs.Schedules) > 1 {
		out.UnmappedNotes = append(out.UnmappedNotes, fmt.Sprintf("Only the first of %d sending schedules was brought over.", len(cs.Schedules)))
	}
	first := cs.Schedules[0]
	sch := &outreachsource.Schedule{
		Timezone: first.Timezone,
		From:     first.Timing.From,
		To:       first.Timing.To,
	}
	for k, on := range first.Days {
		n, err := strconv.Atoi(k)
		if !on || err != nil || n < 0 || n > 6 {
			continue
		}
		if n == 0 {
			n = 7
		}
		sch.Days = append(sch.Days, n)
	}
	sort.Ints(sch.Days)
	if c.EmailGap != nil {
		sch.MinGapMinutes = int(*c.EmailGap)
	}
	loc, err := time.LoadLocation(first.Timezone)
	if err != nil {
		loc = time.UTC
	}
	sch.Start = parseDate(cs.StartDate, loc)
	sch.End = parseDate(cs.EndDate, loc)
	return sch
}

func parseDate(s *string, loc *time.Location) *time.Time {
	if s == nil || *s == "" {
		return nil
	}
	t, err := time.ParseInLocation("2006-01-02", *s, loc)
	if err != nil {
		return nil
	}
	return &t
}
