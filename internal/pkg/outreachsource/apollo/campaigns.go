package apollo

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/warmbly/warmbly/internal/pkg/outreachsource"
)

type sequence struct {
	ID                  string `json:"id"`
	Name                string `json:"name"`
	Archived            bool   `json:"archived"`
	Active              bool   `json:"active"`
	CreatedAt           text   `json:"created_at"`
	ScheduleID          text   `json:"emailer_schedule_id"`
	MaxEmailsPerDay     *int   `json:"max_emails_per_day"`
	MarkFinishedIfReply bool   `json:"mark_finished_if_reply"`
	LastUsedAt          text   `json:"last_used_at"`
	NumSteps            int    `json:"num_steps"`
}

func (q sequence) status() outreachsource.CampaignStatus {
	switch {
	case q.Archived:
		return outreachsource.CampaignArchived
	case q.Active:
		return outreachsource.CampaignActive
	case q.LastUsedAt == "":
		return outreachsource.CampaignDraft
	}
	return outreachsource.CampaignPaused
}

type pagination struct {
	Page         int `json:"page"`
	TotalEntries int `json:"total_entries"`
	TotalPages   int `json:"total_pages"`
}

// sequences pages through every sequence the key can see.
func (s *session) sequences(ctx context.Context) ([]sequence, error) {
	var out []sequence
	for page := 1; page <= maxPages; page++ {
		q := url.Values{"page": {strconv.Itoa(page)}, "per_page": {strconv.Itoa(pageSize)}}
		var resp struct {
			Sequences  []sequence `json:"emailer_campaigns"`
			Pagination pagination `json:"pagination"`
		}
		if err := s.c.Post(ctx, "/emailer_campaigns/search", q, map[string]any{}, &resp); err != nil {
			return nil, err
		}
		out = append(out, resp.Sequences...)
		if len(resp.Sequences) == 0 || page >= resp.Pagination.TotalPages {
			break
		}
	}
	return out, nil
}

type emailerSchedule struct {
	ID              string                 `json:"id"`
	TimeZone        string                 `json:"time_zone"`
	Hash            map[string][][]float64 `json:"schedule_hash"`
	UseContactsZone bool                   `json:"use_contacts_time_zone"`
}

func (s *session) schedules(ctx context.Context) (map[string]emailerSchedule, error) {
	var resp struct {
		Schedules []emailerSchedule `json:"emailer_schedules"`
	}
	if err := s.c.Get(ctx, "/emailer_schedules", nil, &resp); err != nil {
		return nil, optional(err)
	}
	out := make(map[string]emailerSchedule, len(resp.Schedules))
	for _, sc := range resp.Schedules {
		out[sc.ID] = sc
	}
	return out, nil
}

var isoDays = map[string]int{
	"monday": 1, "tuesday": 2, "wednesday": 3, "thursday": 4,
	"friday": 5, "saturday": 6, "sunday": 7,
}

func hhmm(hour float64) string {
	mins := int(math.Round(hour * 60))
	if mins >= 24*60 {
		return "23:59"
	}
	if mins < 0 {
		mins = 0
	}
	return fmt.Sprintf("%02d:%02d", mins/60, mins%60)
}

// schedule flattens Apollo's per-day windows into one daily window, saying
// so when the days differed.
func schedule(sc emailerSchedule) (*outreachsource.Schedule, []string) {
	out := &outreachsource.Schedule{Timezone: sc.TimeZone}
	start, end := math.Inf(1), math.Inf(-1)
	shapes := map[string]bool{}
	gaps := false
	for day, ranges := range sc.Hash {
		iso, ok := isoDays[strings.ToLower(day)]
		if !ok || len(ranges) == 0 {
			continue
		}
		out.Days = append(out.Days, iso)
		shapes[fmt.Sprint(ranges)] = true
		gaps = gaps || len(ranges) > 1
		for _, r := range ranges {
			if len(r) != 2 {
				continue
			}
			start, end = math.Min(start, r[0]), math.Max(end, r[1])
		}
	}
	if len(out.Days) == 0 || math.IsInf(start, 0) {
		return nil, nil
	}
	sort.Ints(out.Days)
	out.From, out.To = hhmm(start), hhmm(end)
	var notes []string
	if len(shapes) > 1 || gaps {
		notes = append(notes, fmt.Sprintf("Apollo's sending hours vary across the week; Warmbly sends from %s to %s on every sending day.", out.From, out.To))
	}
	if sc.UseContactsZone {
		notes = append(notes, fmt.Sprintf("Apollo sent this sequence in each contact's own time zone; Warmbly uses %s.", sc.TimeZone))
	}
	return out, notes
}

// outreach reads a page of the sequence's sent mail, for its senders and
// whether tracking was on. Best effort: a scoped key may not reach it.
func (s *session) outreach(ctx context.Context, id string) (senders []string, tracking bool, err error) {
	q := url.Values{"emailer_campaign_ids[]": {id}, "page": {"1"}, "per_page": {strconv.Itoa(pageSize)}}
	var resp struct {
		Messages []struct {
			FromEmail      text   `json:"from_email"`
			EnableTracking truthy `json:"enable_tracking"`
		} `json:"emailer_messages"`
	}
	if err := s.c.Get(ctx, "/emailer_messages/search", q, &resp); err != nil {
		return nil, false, optional(err)
	}
	seen := map[string]bool{}
	for _, m := range resp.Messages {
		addr := strings.ToLower(string(m.FromEmail))
		if addr != "" && !seen[addr] {
			seen[addr] = true
			senders = append(senders, addr)
		}
		tracking = tracking || bool(m.EnableTracking)
	}
	sort.Strings(senders)
	return senders, tracking, nil
}

func (s *session) Campaigns(ctx context.Context, _ outreachsource.Workspace) ([]outreachsource.Campaign, error) {
	seqs, err := s.sequences(ctx)
	if err != nil {
		return nil, err
	}
	scheds, err := s.schedules(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]outreachsource.Campaign, 0, len(seqs))
	for _, q := range seqs {
		c := outreachsource.Campaign{
			ID:          q.ID,
			Name:        strings.TrimSpace(q.Name),
			Status:      q.status(),
			Steps:       []outreachsource.Step{},
			StopOnReply: q.MarkFinishedIfReply,
			CreatedAt:   parseTime(string(q.CreatedAt)),
		}
		if q.MaxEmailsPerDay != nil && *q.MaxEmailsPerDay > 0 {
			c.DailyLimit = *q.MaxEmailsPerDay
		}
		if q.NumSteps > 0 {
			c.UnmappedNotes = append(c.UnmappedNotes, fmt.Sprintf("Apollo's API does not return this sequence's %d steps, so its emails need to be written again.", q.NumSteps))
		}
		if sc, ok := scheds[string(q.ScheduleID)]; ok {
			sched, notes := schedule(sc)
			c.Schedule = sched
			c.UnmappedNotes = append(c.UnmappedNotes, notes...)
		}
		senders, tracking, err := s.outreach(ctx, q.ID)
		if err != nil {
			return nil, err
		}
		c.Senders, c.TrackOpens, c.TrackClicks = senders, tracking, tracking
		out = append(out, c)
	}
	return out, nil
}
