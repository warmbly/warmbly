package snovio

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	source "github.com/warmbly/warmbly/internal/pkg/outreachsource"
)

type contentSlot struct {
	ID        int64  `json:"id"`
	PlainText bool   `json:"plain_text"`
	Usage     string `json:"usage"`
}

type seqStep struct {
	Ref         ref           `json:"_ref"`
	Type        string        `json:"type"`
	Next        ref           `json:"next"`
	Yes         ref           `json:"yes"`
	No          ref           `json:"no"`
	WaitingType string        `json:"waiting_type"`
	WaitingVal  int           `json:"waiting_val"`
	Action      string        `json:"action"`
	Content     []contentSlot `json:"content"`
}

// minutes is the wait a delay or condition holds a lead for.
func (st seqStep) minutes() int {
	switch strings.ToLower(st.WaitingType) {
	case "days":
		return st.WaitingVal * 24 * 60
	case "hours":
		return st.WaitingVal * 60
	case "minutes":
		return st.WaitingVal
	}
	return 0
}

type campaignInfo struct {
	ID            int64   `json:"id"`
	Status        string  `json:"status"`
	Title         string  `json:"title"`
	EmailAccounts []int64 `json:"email_accounts"`
	Tracking      struct {
		Open      bool `json:"open"`
		LinkClick bool `json:"link_click"`
	} `json:"tracking"`
	SendingSettings struct {
		DailySendingAll           *int `json:"daily_sending_all"`
		DailySendingNewRecipients *int `json:"daily_sending_new_recipients"`
		SkipWhoReplied            bool `json:"skip_who_replied"`
		OneClickUnsubscribe       bool `json:"one_click_unsubscribe"`
	} `json:"sending_settings"`
	ScheduleID         *int64 `json:"schedule_id"`
	StartCampaignAt    string `json:"start_campaign_at"`
	CompleteCampaignAt string `json:"complete_campaign_at"`
	Timezone           string `json:"timezone"`
	Sequence           struct {
		Entry ref       `json:"entry"`
		Steps []seqStep `json:"steps"`
	} `json:"sequence"`
}

func (s *session) campaignInfo(ctx context.Context, id int64) (campaignInfo, error) {
	var resp struct {
		Data campaignInfo `json:"data"`
	}
	err := s.get(ctx, "/v2/campaigns/"+strconv.FormatInt(id, 10), nil, &resp)
	return resp.Data, err
}

type schedule struct {
	ID       int64  `json:"id"`
	Timezone string `json:"timezone"`
	Days     []struct {
		Day       string `json:"day"`
		Enabled   bool   `json:"enabled"`
		StartTime string `json:"start_time"`
		EndTime   string `json:"end_time"`
	} `json:"days"`
}

func (s *session) schedule(ctx context.Context, id int64) (schedule, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.schedules == nil {
		var resp struct {
			Data []schedule `json:"data"`
		}
		if err := s.get(ctx, "/v2/campaigns/schedules", nil, &resp); err != nil {
			return schedule{}, false, err
		}
		s.schedules = map[int64]schedule{}
		for _, sc := range resp.Data {
			s.schedules[sc.ID] = sc
		}
	}
	sc, ok := s.schedules[id]
	return sc, ok, nil
}

var isoDay = map[string]int{"monday": 1, "tuesday": 2, "wednesday": 3, "thursday": 4, "friday": 5, "saturday": 6, "sunday": 7}

// zone turns Snov.io's "America/New York" spelling into the IANA name.
func zone(tz string) string { return strings.ReplaceAll(strings.TrimSpace(tz), " ", "_") }

// toSchedule keeps the widest daily window, since Warmbly has one window for every day.
func toSchedule(sc schedule, tz string) (*source.Schedule, string) {
	if sc.Timezone != "" {
		tz = sc.Timezone
	}
	out := &source.Schedule{Timezone: zone(tz)}
	uneven := false
	for _, d := range sc.Days {
		n, ok := isoDay[strings.ToLower(d.Day)]
		if !ok || !d.Enabled {
			continue
		}
		out.Days = append(out.Days, n)
		if out.From != "" && (d.StartTime != out.From || d.EndTime != out.To) {
			uneven = true
		}
		if out.From == "" || d.StartTime < out.From {
			out.From = d.StartTime
		}
		if d.EndTime > out.To {
			out.To = d.EndTime
		}
	}
	sort.Ints(out.Days)
	if uneven {
		return out, "Snov.io sends at different hours on different days; the widest window was kept."
	}
	return out, ""
}

func (s *session) Campaigns(ctx context.Context, ws source.Workspace) ([]source.Campaign, error) {
	camps, err := s.userCampaigns(ctx)
	if err != nil {
		return nil, err
	}
	lists, err := s.userLists(ctx)
	if err != nil {
		return nil, err
	}
	contacts := map[int64]int{}
	for _, l := range lists {
		contacts[l.ID] = l.Contacts
	}
	out := make([]source.Campaign, 0, len(camps))
	for _, uc := range camps {
		c, err := s.campaign(ctx, uc)
		if err != nil {
			return nil, err
		}
		c.LeadCount = contacts[uc.ListID]
		out = append(out, c)
	}
	return out, nil
}

func campaignStatus(s string) source.CampaignStatus {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "active", "started", "running", "scheduled":
		return source.CampaignActive
	case "paused":
		return source.CampaignPaused
	case "completed", "finished":
		return source.CampaignCompleted
	case "archived":
		return source.CampaignArchived
	}
	return source.CampaignDraft
}

func (s *session) campaign(ctx context.Context, uc userCampaign) (source.Campaign, error) {
	c := source.Campaign{ID: strconv.FormatInt(uc.ID, 10), Name: uc.Campaign, Status: campaignStatus(uc.Status)}
	if uc.CreatedAt > 0 {
		t := time.Unix(uc.CreatedAt, 0).UTC()
		c.CreatedAt = &t
	}
	info, err := s.campaignInfo(ctx, uc.ID)
	if isAPIRefusal(err) {
		c.UnmappedNotes = append(c.UnmappedNotes, "Snov.io did not return this campaign's sequence, so its steps must be recreated.")
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if info.Title != "" {
		c.Name = info.Title
	}
	if info.Status != "" {
		c.Status = campaignStatus(info.Status)
	}
	c.TrackOpens, c.TrackClicks = info.Tracking.Open, info.Tracking.LinkClick
	c.StopOnReply = info.SendingSettings.SkipWhoReplied
	c.Unsubscribe = info.SendingSettings.OneClickUnsubscribe
	if v := info.SendingSettings.DailySendingAll; v != nil {
		c.DailyLimit = *v
	}
	if v := info.SendingSettings.DailySendingNewRecipients; v != nil {
		c.MaxNewLeadsPerDay = *v
	}

	emails, err := s.senderEmails(ctx)
	if err != nil {
		return c, err
	}
	for _, id := range info.EmailAccounts {
		if e := emails[id]; e != "" {
			c.Senders = append(c.Senders, e)
		}
	}

	if info.ScheduleID != nil {
		sc, ok, err := s.schedule(ctx, *info.ScheduleID)
		if err != nil {
			return c, err
		}
		if ok {
			var note string
			c.Schedule, note = toSchedule(sc, info.Timezone)
			if note != "" {
				c.UnmappedNotes = append(c.UnmappedNotes, note)
			}
		}
	}
	if start, end := campaignTime(info.StartCampaignAt, info.Timezone), campaignTime(info.CompleteCampaignAt, info.Timezone); start != nil || end != nil {
		if c.Schedule == nil {
			c.Schedule = &source.Schedule{Timezone: zone(info.Timezone)}
		}
		c.Schedule.Start, c.Schedule.End = start, end
	}

	steps, notes, err := s.walk(ctx, c.ID, info)
	if err != nil {
		return c, err
	}
	c.Steps = steps
	c.UnmappedNotes = append(c.UnmappedNotes, notes...)
	c.PlainText = len(steps) > 0
	for _, st := range steps {
		for _, v := range st.Variants {
			c.PlainText = c.PlainText && !v.HTML
		}
	}
	return c, nil
}

// campaignTime reads "2006-01-02 15:04:05" in the campaign's timezone.
func campaignTime(v, tz string) *time.Time {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	loc, err := time.LoadLocation(zone(tz))
	if err != nil || tz == "" {
		loc = time.UTC
	}
	t, err := time.ParseInLocation("2006-01-02 15:04:05", strings.TrimSpace(v), loc)
	if err != nil {
		return nil
	}
	return &t
}

// walk follows the flowchart from its entry. A condition is followed down its
// "not met" branch, and its wait is added to the next email's delay.
func (s *session) walk(ctx context.Context, campaignID string, info campaignInfo) ([]source.Step, []string, error) {
	byRef := map[ref]seqStep{}
	for _, st := range info.Sequence.Steps {
		byRef[st.Ref] = st
	}
	var out []source.Step
	var notes []string
	visited := map[ref]bool{}
	pending, dropped := 0, false
	cur := info.Sequence.Entry
	for cur != "" && !visited[cur] {
		visited[cur] = true
		st, ok := byRef[cur]
		if !ok {
			break
		}
		cur = st.Next
		switch strings.ToLower(st.Type) {
		case "email":
			step, d, err := s.emailStep(ctx, campaignID, st)
			if err != nil {
				return nil, nil, err
			}
			dropped = dropped || d
			if len(step.Variants) == 0 {
				notes = append(notes, fmt.Sprintf("Email %d has no content in Snov.io and was not brought over.", len(out)+1))
				continue
			}
			step.WaitDays, step.WaitMinutes = pending/(24*60), pending%(24*60)
			pending = 0
			out = append(out, step)
		case "delay":
			pending += st.minutes()
		case "trigger":
			pending += st.minutes()
			if st.No != "" {
				cur = st.No
				notes = append(notes, fmt.Sprintf("A condition (%s) branches the sequence; only the path for leads who did not meet it was kept.", conditionName(st.Action)))
			} else {
				cur = st.Yes
				notes = append(notes, fmt.Sprintf("A condition (%s) branches the sequence; only the path for leads who met it was kept.", conditionName(st.Action)))
			}
		case "linkedin":
			notes = append(notes, fmt.Sprintf("A LinkedIn step (%s) was not brought over.", strings.ReplaceAll(st.Action, "_", " ")))
		case "goal":
			cur = ""
		default:
			notes = append(notes, fmt.Sprintf("A %s step was not brought over.", st.Type))
		}
	}
	for _, st := range info.Sequence.Steps {
		if strings.EqualFold(st.Type, "email") && !visited[st.Ref] {
			notes = append(notes, "Emails on the other branch of a condition were not brought over.")
			break
		}
	}
	if dropped {
		notes = append(notes, "Signature tags were removed; Warmbly adds the mailbox signature itself.")
	}
	return out, notes, nil
}

func conditionName(action string) string {
	switch action {
	case "open":
		return "opened an email"
	case "click":
		return "clicked a link"
	case "calendly":
		return "booked through Calendly"
	}
	return strings.ReplaceAll(action, "_", " ")
}

func (s *session) emailStep(ctx context.Context, campaignID string, st seqStep) (source.Step, bool, error) {
	var step source.Step
	dropped := false
	for _, slot := range st.Content {
		var resp struct {
			Data struct {
				Subject   string `json:"subject"`
				Body      string `json:"body"`
				PlainText bool   `json:"plain_text"`
				Usage     string `json:"usage"`
			} `json:"data"`
		}
		path := "/v2/campaigns/" + url.PathEscape(campaignID) + "/steps/" + url.PathEscape(string(st.Ref)) + "/content/" + strconv.FormatInt(slot.ID, 10)
		if err := s.get(ctx, path, nil, &resp); err != nil {
			return step, false, err
		}
		subj, body := source.Translate(resp.Data.Subject, dialect), source.Translate(resp.Data.Body, dialect)
		dropped = dropped || len(subj.Dropped)+len(body.Dropped) > 0
		usage := resp.Data.Usage
		if usage == "" {
			usage = slot.Usage
		}
		step.Variants = append(step.Variants, source.Variant{
			Subject: subj.Text, Body: body.Text, HTML: !resp.Data.PlainText,
			Disabled: strings.EqualFold(usage, "pause"),
		})
	}
	return step, dropped, nil
}

var dialect = func() source.Dialect {
	f := source.CommonFields()
	f["sender_account_name"] = "Sender.Name"
	f["position"] = "title"
	return source.Dialect{Fields: f, Spin: []string{"spintax"}}
}()
