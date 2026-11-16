package woodpecker

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/warmbly/warmbly/internal/pkg/outreachsource"
)

type listedCampaign struct {
	ID         int64    `json:"id"`
	Name       string   `json:"name"`
	Status     string   `json:"status"`
	Created    string   `json:"created"`
	FromEmail  string   `json:"from_email"`
	FromEmails []string `json:"from_emails"`
}

func campaignList(ctx context.Context, c *outreachsource.Client) ([]listedCampaign, error) {
	var list []listedCampaign
	// 204 with no body when there are none, which leaves list empty.
	if err := c.Get(ctx, "/v1/campaign_list", nil, &list); err != nil {
		return nil, err
	}
	return list, nil
}

type stats struct{ prospects, emails int }

// campaignStats reads the enrolled prospect count and email step count.
func campaignStats(ctx context.Context, c *outreachsource.Client, id int64) (stats, error) {
	var resp []struct {
		Stats struct {
			Prospects int   `json:"prospects"`
			Emails    []any `json:"emails"`
		} `json:"stats"`
	}
	if err := c.Get(ctx, "/v1/campaign_list", url.Values{"id": {strconv.FormatInt(id, 10)}}, &resp); err != nil {
		return stats{}, err
	}
	if len(resp) == 0 {
		return stats{}, nil
	}
	return stats{prospects: resp[0].Stats.Prospects, emails: len(resp[0].Stats.Emails)}, nil
}

func campaignStatus(s string) outreachsource.CampaignStatus {
	switch strings.ToUpper(s) {
	case "RUNNING":
		return outreachsource.CampaignActive
	case "DRAFT":
		return outreachsource.CampaignDraft
	case "COMPLETED":
		return outreachsource.CampaignCompleted
	default: // PAUSED, STOPPED, EDITED
		return outreachsource.CampaignPaused
	}
}

type window struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type wpStep struct {
	Type         string              `json:"type"`
	DeliveryTime map[string][]window `json:"delivery_time"`
	Body         struct {
		ActionType string `json:"action_type"`
		Versions   []struct {
			Version    string  `json:"version"`
			Subject    *string `json:"subject"`
			Message    string  `json:"message"`
			Signature  string  `json:"signature"`
			TrackOpens bool    `json:"track_opens"`
		} `json:"versions"`
	} `json:"body"`
	FollowupAfter *struct {
		Range string `json:"range"`
		Value int    `json:"value"`
	} `json:"followup_after"`
	Followup *wpStep `json:"followup"`
}

type wpCampaign struct {
	EmailAccountIDs []int64 `json:"email_account_ids"`
	Settings        struct {
		Timezone            string   `json:"timezone"`
		ProspectTimezone    bool     `json:"prospect_timezone"`
		DailyEnroll         int      `json:"daily_enroll"`
		GDPRUnsubscribe     bool     `json:"gdpr_unsubscribe"`
		ListUnsubscribe     bool     `json:"list_unsubscribe"`
		OpenDisabledList    []string `json:"open_disabled_list"`
		WorkingDayFollowups bool     `json:"count_followup_delay_in_working_days"`
	} `json:"settings"`
	Steps *wpStep `json:"steps"`
}

func (s *session) Campaigns(ctx context.Context, ws outreachsource.Workspace) ([]outreachsource.Campaign, error) {
	c := s.client(ws)
	list, err := campaignList(ctx, c)
	if err != nil {
		return nil, err
	}
	out := make([]outreachsource.Campaign, 0, len(list))
	for _, lc := range list {
		camp := outreachsource.Campaign{
			ID:          strconv.FormatInt(lc.ID, 10),
			Name:        lc.Name,
			Status:      campaignStatus(lc.Status),
			Senders:     lc.FromEmails,
			StopOnReply: true,
			CreatedAt:   parseTime(lc.Created),
		}
		if len(camp.Senders) == 0 && lc.FromEmail != "" {
			camp.Senders = []string{lc.FromEmail}
		}
		st, err := campaignStats(ctx, c, lc.ID)
		if err != nil {
			return nil, err
		}
		camp.LeadCount = st.prospects
		var full wpCampaign
		err = c.Get(ctx, "/v2/campaigns/"+camp.ID, nil, &full)
		var ae *outreachsource.APIError
		switch {
		case errors.As(err, &ae) && ae.Status == http.StatusConflict:
			camp.UnmappedNotes = append(camp.UnmappedNotes, "Woodpecker's API cannot read this campaign's sequence because it uses IF conditions, a scheduled start or manual task steps, so its emails are not brought over.")
		case err != nil:
			return nil, err
		default:
			mapSequence(&camp, full)
		}
		out = append(out, camp)
	}
	return out, nil
}

var dialect = func() outreachsource.Dialect {
	f := outreachsource.CommonFields()
	f["click"] = ""    // a bare click-tracking marker; Warmbly tracks links itself
	f["city"] = "city" // Woodpecker keeps city apart from state and country
	return outreachsource.Dialect{Fields: f, Spin: []string{"SPINTAX"}}
}()

var (
	tokenRE = regexp.MustCompile(`\{\{([^{}]*)\}\}`)
	clickRE = regexp.MustCompile(`(?i)^\s*click\s*=\s*(.*?)\s*$`)
)

// prepare unwraps {{CLICK=url}} to the url and unquotes `"fallback"` parts so
// Translate reads Woodpecker's `{{FIRST_NAME | "there"}}`.
func prepare(text string) (out string, clicks bool) {
	out = tokenRE.ReplaceAllStringFunc(text, func(tok string) string {
		inner := tokenRE.FindStringSubmatch(tok)[1]
		if m := clickRE.FindStringSubmatch(inner); m != nil {
			clicks = true
			return m[1]
		}
		parts := strings.Split(html.UnescapeString(inner), "|")
		for i := 1; i < len(parts); i++ {
			p := strings.TrimSpace(parts[i])
			p = strings.TrimPrefix(strings.TrimSuffix(p, `"`), `"`)
			parts[i] = strings.TrimSpace(p)
		}
		return "{{" + strings.Join(parts, "|") + "}}"
	})
	return out, clicks
}

var dayNumber = map[string]int{"MONDAY": 1, "TUESDAY": 2, "WEDNESDAY": 3, "THURSDAY": 4, "FRIDAY": 5, "SATURDAY": 6, "SUNDAY": 7}

func delayMinutes(st *wpStep) int {
	if st.FollowupAfter == nil {
		return 24 * 60 // documented default: 1 DAY
	}
	v := st.FollowupAfter.Value
	switch strings.ToUpper(st.FollowupAfter.Range) {
	case "MINUTE":
		return v
	case "HOUR":
		return v * 60
	default:
		return v * 24 * 60
	}
}

func mapSequence(camp *outreachsource.Campaign, full wpCampaign) {
	set := full.Settings
	camp.Unsubscribe = set.ListUnsubscribe || set.GDPRUnsubscribe
	dropped := map[string]bool{}
	var firstHours string
	hoursDiffer := false
	pending, n := 0, 0
	for st := full.Steps; st != nil; st = st.Followup {
		if strings.EqualFold(st.Type, "START") {
			continue
		}
		n++
		if !strings.EqualFold(st.Type, "EMAIL") {
			action := strings.ToLower(strings.ReplaceAll(st.Body.ActionType, "_", " "))
			kind := strings.ToLower(st.Type)
			if kind == "linkedin" {
				kind = "LinkedIn"
			}
			camp.UnmappedNotes = append(camp.UnmappedNotes, strings.TrimSpace(fmt.Sprintf("Step %d, a %s %s step, is not brought over.", n, kind, action)))
			pending += delayMinutes(st)
			continue
		}
		step := outreachsource.Step{WaitDays: pending / (24 * 60), WaitMinutes: pending % (24 * 60)}
		pending = delayMinutes(st)
		for _, v := range st.Body.Versions {
			subject := ""
			if v.Subject != nil {
				subject = *v.Subject
			}
			subj, c1 := prepare(subject)
			body, c2 := prepare(v.Message)
			ts, tb := outreachsource.Translate(subj, dialect), outreachsource.Translate(body, dialect)
			for _, d := range append(ts.Dropped, tb.Dropped...) {
				dropped[d] = true
			}
			camp.TrackClicks = camp.TrackClicks || c1 || c2
			camp.TrackOpens = camp.TrackOpens || v.TrackOpens
			if strings.Contains(tb.Text, "{{unsubscribe_link}}") {
				camp.Unsubscribe = true
			}
			step.Variants = append(step.Variants, outreachsource.Variant{Subject: ts.Text, Body: tb.Text, HTML: true})
		}
		if len(camp.Steps) > 0 && len(step.Variants) > 0 && step.Variants[0].Subject != "" {
			step.NewThread = true
		}
		hours := hoursKey(st.DeliveryTime)
		if camp.Schedule == nil {
			camp.Schedule = schedule(set.Timezone, st.DeliveryTime, camp)
			firstHours = hours
		} else if hours != firstHours {
			hoursDiffer = true
		}
		camp.Steps = append(camp.Steps, step)
	}
	if hoursDiffer {
		camp.UnmappedNotes = append(camp.UnmappedNotes, "Later emails had their own sending hours in Woodpecker; the first email's hours apply to the whole campaign.")
	}
	if set.ProspectTimezone {
		camp.UnmappedNotes = append(camp.UnmappedNotes, "Woodpecker sent in each prospect's own timezone; the campaign's timezone is used instead.")
	}
	if set.WorkingDayFollowups {
		camp.UnmappedNotes = append(camp.UnmappedNotes, "Woodpecker counted follow-up delays in working days only; the delays are kept as calendar days.")
	}
	// daily_enroll applies per mailbox.
	camp.MaxNewLeadsPerDay = set.DailyEnroll * max(1, len(full.EmailAccountIDs))
	if len(set.OpenDisabledList) > 0 {
		camp.UnmappedNotes = append(camp.UnmappedNotes, "Open tracking was off for some recipient providers in Woodpecker; it is set for the whole campaign.")
	}
	if len(dropped) > 0 {
		names := make([]string, 0, len(dropped))
		for d := range dropped {
			names = append(names, d)
		}
		sort.Strings(names)
		camp.UnmappedNotes = append(camp.UnmappedNotes, "Removed merge tokens Warmbly fills itself: "+strings.Join(names, ", ")+".")
	}
}

func hoursKey(dt map[string][]window) string {
	days := make([]string, 0, len(dt))
	for d, ws := range dt {
		var b strings.Builder
		b.WriteString(strings.ToUpper(d))
		for _, w := range ws {
			b.WriteString(" " + w.From + "-" + w.To)
		}
		days = append(days, b.String())
	}
	sort.Strings(days)
	return strings.Join(days, ";")
}

// schedule folds Woodpecker's per-day windows into one daily window.
func schedule(tz string, dt map[string][]window, camp *outreachsource.Campaign) *outreachsource.Schedule {
	sch := &outreachsource.Schedule{Timezone: tz}
	var shapes = map[string]bool{}
	for day, ws := range dt {
		n, ok := dayNumber[strings.ToUpper(day)]
		if !ok || len(ws) == 0 {
			continue
		}
		sch.Days = append(sch.Days, n)
		var shape []string
		for _, w := range ws {
			from, to := w.From, w.To
			if to == "00:00" || to == "24:00" {
				to = "23:59" // "00:00" to "00:00" is the whole day
			}
			if sch.From == "" || from < sch.From {
				sch.From = from
			}
			if to > sch.To {
				sch.To = to
			}
			shape = append(shape, from+"-"+to)
		}
		shapes[strings.Join(shape, ",")] = true
	}
	sort.Ints(sch.Days)
	if len(shapes) > 1 || anyMulti(dt) {
		camp.UnmappedNotes = append(camp.UnmappedNotes, fmt.Sprintf("Woodpecker sent in different or several windows a day; the campaign sends from %s to %s.", sch.From, sch.To))
	}
	return sch
}

func anyMulti(dt map[string][]window) bool {
	for _, ws := range dt {
		if len(ws) > 1 {
			return true
		}
	}
	return false
}
