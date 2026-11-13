package replyio

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	source "github.com/warmbly/warmbly/internal/pkg/outreachsource"
)

type sequenceSummary struct {
	ID         int        `json:"id"`
	Name       string     `json:"name"`
	Status     string     `json:"status"`
	IsArchived bool       `json:"isArchived"`
	Created    *time.Time `json:"created"`
}

func (s sequenceSummary) status() source.CampaignStatus {
	switch {
	case s.IsArchived:
		return source.CampaignArchived
	case s.Status == "active":
		return source.CampaignActive
	case s.Status == "paused":
		return source.CampaignPaused
	}
	return source.CampaignDraft
}

type sequence struct {
	sequenceSummary
	EmailAccounts []struct {
		ID    int64  `json:"id"`
		Email string `json:"email"`
	} `json:"emailAccounts"`
	ScheduleID int       `json:"scheduleId"`
	Settings   settings  `json:"settings"`
	Steps      []seqStep `json:"steps"`
}

type settings struct {
	DailyThrottling           int    `json:"dailyThrottling"`
	UseDailyThrottling        bool   `json:"useDailyThrottling"`
	DisableOpensTracking      bool   `json:"disableOpensTracking"`
	EnableLinksTracking       bool   `json:"enableLinksTracking"`
	EmailSendingDelaySeconds  int    `json:"emailSendingDelaySeconds"`
	EmailsCountPerDay         int    `json:"emailsCountPerDay"`
	IsListUnsubscribeHeader   bool   `json:"isListUnsubscribeHeaderEnabled"`
	IsSendingPlainTextEnabled bool   `json:"isSendingPlainTextEnabled"`
	RepliesHandlingType       string `json:"repliesHandlingType"`
}

type seqStep struct {
	ID             *int      `json:"id"`
	Type           string    `json:"type"`
	ActionType     string    `json:"actionType"`
	DelayInMinutes int       `json:"delayInMinutes"`
	ExecutionMode  string    `json:"executionMode"`
	ParentID       *int      `json:"parentId"`
	Variants       []variant `json:"variants"`
}

type variant struct {
	ID        int     `json:"id"`
	Subject   *string `json:"subject"`
	Message   *string `json:"message"`
	IsEnabled *bool   `json:"isEnabled"`
}

type schedule struct {
	TimezoneID          string   `json:"timezoneId"`
	UseProspectTimezone bool     `json:"useProspectTimezone"`
	UseFollowUpSchedule bool     `json:"useFollowUpSchedule"`
	MainTimings         []timing `json:"mainTimings"`
}

type timing struct {
	WeekDay    string `json:"weekDay"`
	IsActive   bool   `json:"isActive"`
	TimeRanges []struct {
		FromTime clock `json:"fromTime"`
		ToTime   clock `json:"toTime"`
	} `json:"timeRanges"`
}

type clock struct {
	Hour   int `json:"hour"`
	Minute int `json:"minute"`
}

func (c clock) String() string { return fmt.Sprintf("%02d:%02d", c.Hour, c.Minute) }

func (c clock) minutes() int { return c.Hour*60 + c.Minute }

func (s *session) listSequences(ctx context.Context) ([]sequenceSummary, error) {
	return all[sequenceSummary](ctx, s.c, "/v3/sequences", nil)
}

// sequence reads one sequence in full, once per session.
func (s *session) sequence(ctx context.Context, id int) (*sequence, error) {
	s.mu.Lock()
	sq, ok := s.seqs[id]
	s.mu.Unlock()
	if ok {
		return sq, nil
	}
	sq = &sequence{}
	if err := s.c.Get(ctx, "/v3/sequences/"+strconv.Itoa(id), nil, sq); err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.seqs[id] = sq
	s.mu.Unlock()
	return sq, nil
}

func (s *session) schedule(ctx context.Context, id int) (*schedule, error) {
	s.mu.Lock()
	sc, ok := s.schedules[id]
	s.mu.Unlock()
	if ok {
		return sc, nil
	}
	sc = &schedule{}
	if err := s.c.Get(ctx, "/v3/schedules/"+strconv.Itoa(id), nil, sc); err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.schedules[id] = sc
	s.mu.Unlock()
	return sc, nil
}

func (s *session) Campaigns(ctx context.Context, _ source.Workspace) ([]source.Campaign, error) {
	list, err := s.listSequences(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]source.Campaign, 0, len(list))
	for _, sum := range list {
		c, err := s.campaign(ctx, sum.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

func (s *session) campaign(ctx context.Context, id int) (source.Campaign, error) {
	sq, err := s.sequence(ctx, id)
	if err != nil {
		return source.Campaign{}, err
	}
	set := sq.Settings
	out := source.Campaign{
		ID: strconv.Itoa(sq.ID), Name: sq.Name, Status: sq.status(), CreatedAt: sq.Created,
		DailyLimit:  set.EmailsCountPerDay,
		StopOnReply: set.RepliesHandlingType != "continueSending",
		TrackOpens:  !set.DisableOpensTracking,
		TrackClicks: set.EnableLinksTracking,
		PlainText:   set.IsSendingPlainTextEnabled,
		Unsubscribe: set.IsListUnsubscribeHeader,
	}
	if set.UseDailyThrottling {
		out.MaxNewLeadsPerDay = set.DailyThrottling
	}
	for _, a := range sq.EmailAccounts {
		out.Senders = append(out.Senders, strings.ToLower(a.Email))
	}
	var count struct {
		Count int `json:"count"`
	}
	switch err := s.c.Get(ctx, "/v3/sequences/"+strconv.Itoa(id)+"/contacts/count", nil, &count); {
	case err == nil:
		out.LeadCount = count.Count
	case refused(err):
		out.LeadCount = -1
	default:
		return out, err
	}
	if err := s.mapSteps(ctx, &out, sq); err != nil {
		return out, err
	}
	if sq.ScheduleID != 0 {
		sc, err := s.schedule(ctx, sq.ScheduleID)
		switch {
		case err == nil:
			out.Schedule = mapSchedule(sc, &out)
		case !refused(err):
			return out, err
		}
	}
	if out.Schedule != nil && set.EmailSendingDelaySeconds > 0 {
		out.Schedule.MinGapMinutes = (set.EmailSendingDelaySeconds + 59) / 60
	}
	return out, nil
}

// topLevel lists the steps on the main path; a step with a parent sits in a
// condition's branch.
func topLevel(steps []seqStep) []seqStep {
	var out []seqStep
	for _, st := range steps {
		if st.ParentID == nil {
			out = append(out, st)
		}
	}
	return out
}

func (s *session) mapSteps(ctx context.Context, out *source.Campaign, sq *sequence) error {
	branchEmails := 0
	for _, st := range sq.Steps {
		if st.ParentID != nil && st.Type == "email" {
			branchEmails++
		}
	}
	carry := 0
	var dropped []string
	for n, st := range topLevel(sq.Steps) {
		pos := n + 1
		if st.Type != "email" {
			carry += st.DelayInMinutes
			if st.Type == "condition" {
				out.UnmappedNotes = append(out.UnmappedNotes, fmt.Sprintf("Step %d is a condition; only the steps on the main path were brought over.", pos))
			} else {
				out.UnmappedNotes = append(out.UnmappedNotes, fmt.Sprintf("Step %d is a %s step and was not brought over.", pos, stepName(st)))
			}
			continue
		}
		wait := carry + st.DelayInMinutes
		carry = 0
		mapped := source.Step{WaitDays: wait / (24 * 60), WaitMinutes: wait % (24 * 60)}
		vs := st.Variants
		if len(vs) > 1 && st.ID != nil {
			var full []variant
			path := "/v3/sequences/" + strconv.Itoa(sq.ID) + "/steps/" + strconv.Itoa(*st.ID) + "/variants"
			switch err := s.c.Get(ctx, path, nil, &full); {
			case err == nil:
				vs = full
			case !refused(err):
				return err
			}
		}
		for _, v := range orderVariants(vs) {
			subj, sd := translate(deref(v.Subject))
			body, bd := translate(deref(v.Message))
			dropped = append(append(dropped, sd...), bd...)
			mapped.Variants = append(mapped.Variants, source.Variant{
				Subject: subj, Body: body, HTML: !out.PlainText,
				Disabled: v.IsEnabled != nil && !*v.IsEnabled,
			})
		}
		if st.ExecutionMode == "manual" {
			out.UnmappedNotes = append(out.UnmappedNotes, fmt.Sprintf("Step %d waited for manual sending in Reply.io and will send on its own in Warmbly.", pos))
		}
		out.Steps = append(out.Steps, mapped)
	}
	if branchEmails > 0 {
		out.UnmappedNotes = append(out.UnmappedNotes, fmt.Sprintf("Email steps inside condition branches were not brought over (%d).", branchEmails))
	}
	if len(dropped) > 0 {
		out.UnmappedNotes = append(out.UnmappedNotes, "Removed "+strings.Join(uniq(dropped), ", ")+", which Warmbly adds from the sending mailbox.")
	}
	return nil
}

// orderVariants puts enabled variants first, keeping Reply's order otherwise.
func orderVariants(vs []variant) []variant {
	out := append([]variant(nil), vs...)
	sort.SliceStable(out, func(i, j int) bool {
		return enabled(out[i]) && !enabled(out[j])
	})
	return out
}

func enabled(v variant) bool { return v.IsEnabled == nil || *v.IsEnabled }

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func stepName(st seqStep) string {
	switch st.Type {
	case "linkedIn":
		if st.ActionType != "" {
			return "LinkedIn " + st.ActionType
		}
		return "LinkedIn"
	case "call":
		return "call"
	case "sms":
		return "SMS"
	case "whatsApp":
		return "WhatsApp"
	case "zapier":
		return "Zapier"
	case "task":
		return "manual task"
	case "":
		return "non-email"
	}
	return st.Type
}

var weekdays = map[string]int{"Monday": 1, "Tuesday": 2, "Wednesday": 3, "Thursday": 4, "Friday": 5, "Saturday": 6, "Sunday": 7}

// mapSchedule flattens Reply's per-day windows into one window spanning the
// earliest start and latest end, noting when the days differed.
func mapSchedule(sc *schedule, c *source.Campaign) *source.Schedule {
	out := &source.Schedule{Timezone: ianaZone(sc.TimezoneID)}
	from, to := -1, -1
	windows := map[string]bool{}
	for _, t := range sc.MainTimings {
		d, ok := weekdays[t.WeekDay]
		if !ok || !t.IsActive || len(t.TimeRanges) == 0 {
			continue
		}
		out.Days = append(out.Days, d)
		var w []string
		for _, r := range t.TimeRanges {
			w = append(w, r.FromTime.String()+"-"+r.ToTime.String())
			if from < 0 || r.FromTime.minutes() < from {
				from = r.FromTime.minutes()
			}
			if r.ToTime.minutes() > to {
				to = r.ToTime.minutes()
			}
		}
		windows[strings.Join(w, ",")] = true
	}
	if from < 0 {
		return nil
	}
	sort.Ints(out.Days)
	out.From = clock{Hour: from / 60, Minute: from % 60}.String()
	out.To = clock{Hour: to / 60, Minute: to % 60}.String()
	if len(windows) > 1 {
		c.UnmappedNotes = append(c.UnmappedNotes, fmt.Sprintf("The Reply.io schedule has different hours per day; Warmbly sends %s to %s on every active day.", out.From, out.To))
	}
	if sc.TimezoneID != "" && out.Timezone == "" {
		c.UnmappedNotes = append(c.UnmappedNotes, fmt.Sprintf("The schedule's time zone %q has no standard name, so set it again.", sc.TimezoneID))
	}
	if sc.UseProspectTimezone {
		c.UnmappedNotes = append(c.UnmappedNotes, "Reply.io sent in each contact's own time zone; Warmbly uses the schedule's time zone.")
	}
	if sc.UseFollowUpSchedule {
		c.UnmappedNotes = append(c.UnmappedNotes, "The separate follow-up schedule was not brought over.")
	}
	return out
}

// ianaZone turns a Windows zone id into an IANA name; an IANA name passes through.
func ianaZone(id string) string {
	if z, ok := windowsZones[id]; ok {
		return z
	}
	if strings.Contains(id, "/") {
		return id
	}
	return ""
}

func uniq(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
