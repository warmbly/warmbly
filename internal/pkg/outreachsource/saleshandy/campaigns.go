package saleshandy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	source "github.com/warmbly/warmbly/internal/pkg/outreachsource"
)

type sequence struct {
	ID        string     `json:"id"`
	Title     string     `json:"title"`
	Name      string     `json:"name"`
	Active    bool       `json:"active"`
	CreatedAt string     `json:"createdAt"`
	Client    *clientRef `json:"client"`
	Steps     []struct {
		ID   flexString `json:"id"`
		Name string     `json:"name"`
	} `json:"steps"`
}

func (q sequence) title() string {
	if q.Title != "" {
		return q.Title
	}
	return q.Name
}

func (q sequence) status() source.CampaignStatus {
	if q.Active {
		return source.CampaignActive
	}
	return source.CampaignPaused
}

func (s *session) sequences(ctx context.Context, ws source.Workspace) ([]sequence, error) {
	var out []sequence
	for page := 1; ; page++ {
		q := url.Values{
			"page": {strconv.Itoa(page)}, "pageSize": {strconv.Itoa(pageSize)},
			"sort": {"ASC"}, "sortBy": {"sequence.createdAt"},
		}
		if ws.ID != "" {
			q.Set("clientIds", ws.ID)
		}
		var raw json.RawMessage
		if err := s.c.Get(ctx, "/v1/sequences", q, &raw); err != nil {
			return nil, err
		}
		var seqs []sequence
		if err := decodeList(raw, &seqs, "sequences"); err != nil {
			return nil, fmt.Errorf("%s: decode sequences: %w", label, err)
		}
		for _, sq := range seqs {
			if in(ws, sq.Client) {
				out = append(out, sq)
			}
		}
		if len(seqs) < pageSize {
			return out, nil
		}
	}
}

// prospectCount is a sequence's prospect total from its stats, -1 when unreadable.
func (s *session) prospectCount(ctx context.Context, sequenceID string) int {
	var raw json.RawMessage
	if err := s.c.Post(ctx, "/v1/analytics/stats", nil, map[string]string{"sequenceId": sequenceID}, &raw); err != nil {
		return -1
	}
	var resp struct {
		Prospects []struct {
			Total flexInt `json:"total"`
		} `json:"prospects"`
	}
	if json.Unmarshal(unwrap(raw), &resp) != nil || len(resp.Prospects) == 0 {
		return -1
	}
	return int(resp.Prospects[0].Total)
}

// channel is a step's channel, sent as a number (1) or a name ("Email").
type channel string

func (c *channel) UnmarshalJSON(b []byte) error {
	var f flexString
	if err := f.UnmarshalJSON(b); err != nil {
		return err
	}
	*c = channel(f)
	return nil
}

var channelNames = map[string]string{
	"1": "email", "2": "LinkedIn connection request", "3": "LinkedIn message", "4": "LinkedIn InMail",
	"5": "LinkedIn profile visit", "6": "LinkedIn post interaction", "9": "manual task",
	"11": "call", "12": "call", "13": "call", "14": "call", "15": "call",
	"16": "WhatsApp message", "17": "WhatsApp voice message", "18": "WhatsApp call",
}

func (c channel) email() bool { return c == "1" || strings.EqualFold(string(c), "email") }

func (c channel) name() string {
	if n, ok := channelNames[string(c)]; ok {
		return n
	}
	if c == "" {
		return "non-email"
	}
	return string(c)
}

type variant struct {
	Payload struct {
		Subject   string `json:"subject"`
		Content   string `json:"content"`
		Preheader string `json:"preheader"`
	} `json:"payload"`
	Status       *flexInt `json:"status"`
	Type         channel  `json:"type"`
	AbsoluteDays flexInt  `json:"absoluteDays"`
	WaitN        flexInt  `json:"waitN"`
}

type step struct {
	ID           flexString `json:"id"`
	Name         string     `json:"name"`
	Number       flexInt    `json:"number"`
	Type         channel    `json:"type"`
	AbsoluteDays flexInt    `json:"absoluteDays"`
	WaitN        flexInt    `json:"waitN"`
	Variants     []variant  `json:"variants"`
}

func (st step) kind() channel {
	if st.Type != "" {
		return st.Type
	}
	if len(st.Variants) > 0 {
		return st.Variants[0].Type
	}
	return ""
}

// timing is the step's day (1 is the day a prospect enters) and minutes into it.
func (st step) timing() (int, int) {
	day, wait := int(st.AbsoluteDays), int(st.WaitN)
	if day == 0 && len(st.Variants) > 0 {
		day, wait = int(st.Variants[0].AbsoluteDays), int(st.Variants[0].WaitN)
	}
	return day, wait
}

func (s *session) steps(ctx context.Context, sq sequence) ([]step, error) {
	base := "/v1/sequences/" + url.PathEscape(sq.ID) + "/steps"
	var raw json.RawMessage
	if err := s.c.Get(ctx, base, nil, &raw); err != nil {
		return nil, err
	}
	var steps []step
	if err := decodeList(raw, &steps, "steps"); err != nil {
		return nil, fmt.Errorf("%s: decode steps: %w", label, err)
	}
	if len(steps) == 0 {
		for _, ref := range sq.Steps {
			steps = append(steps, step{ID: ref.ID, Name: ref.Name})
		}
	}
	for i := range steps {
		if len(steps[i].Variants) > 0 || steps[i].ID == "" {
			continue
		}
		var vraw json.RawMessage
		if err := s.c.Get(ctx, base+"/"+url.PathEscape(string(steps[i].ID)), nil, &vraw); err != nil {
			return nil, err
		}
		if err := decodeList(vraw, &steps[i].Variants, "variants"); err != nil {
			return nil, fmt.Errorf("%s: decode step variants: %w", label, err)
		}
	}
	numbered := true
	for _, st := range steps {
		numbered = numbered && st.Number > 0
	}
	if numbered {
		sort.SliceStable(steps, func(a, b int) bool { return steps[a].Number < steps[b].Number })
	}
	return steps, nil
}

type settingsReply struct {
	Settings []struct {
		Code  flexInt    `json:"code"`
		Value flexString `json:"value"`
	} `json:"settings"`
	Schedule *struct {
		ID       flexString `json:"id"`
		Timezone string     `json:"timezone"`
	} `json:"schedule"`
}

func (s *session) settings(ctx context.Context, id string) (settingsReply, error) {
	var raw json.RawMessage
	var out settingsReply
	if err := s.c.Get(ctx, "/v1/sequences/"+url.PathEscape(id)+"/settings", nil, &raw); err != nil {
		return out, err
	}
	raw = unwrap(raw)
	if len(raw) > 0 && raw[0] == '[' {
		return out, json.Unmarshal(raw, &out.Settings)
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, fmt.Errorf("%s: decode sequence settings: %w", label, err)
	}
	return out, nil
}

type hm struct {
	Hour   int `json:"hour"`
	Minute int `json:"minute"`
}

func (t hm) minutes() int   { return t.Hour*60 + t.Minute }
func (t hm) String() string { return fmt.Sprintf("%02d:%02d", t.Hour, t.Minute) }

type schedule struct {
	ID        flexString `json:"id"`
	Timezone  string     `json:"timezone"`
	TimeSlots []struct {
		Day   flexInt `json:"day"`
		Slots []struct {
			Start hm `json:"start"`
			End   hm `json:"end"`
		} `json:"slots"`
	} `json:"timeSlots"`
}

func (s *session) schedule(ctx context.Context, id string) (schedule, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.schedules == nil {
		var raw json.RawMessage
		if err := s.c.Get(ctx, "/v1/schedules", nil, &raw); err != nil {
			return schedule{}, false, err
		}
		var all []schedule
		if err := decodeList(raw, &all, "schedules"); err != nil {
			return schedule{}, false, fmt.Errorf("%s: decode schedules: %w", label, err)
		}
		s.schedules = map[string]schedule{}
		for _, sc := range all {
			s.schedules[string(sc.ID)] = sc
		}
	}
	sc, ok := s.schedules[id]
	return sc, ok, nil
}

// toSchedule keeps the widest daily window, since Warmbly has one window for every day.
func toSchedule(sc schedule, tz string) (*source.Schedule, string) {
	if sc.Timezone != "" {
		tz = sc.Timezone
	}
	out := &source.Schedule{Timezone: tz}
	from, to := -1, -1
	uneven := false
	var first string
	for _, d := range sc.TimeSlots {
		if len(d.Slots) == 0 {
			continue
		}
		day := int(d.Day)
		if day == 0 {
			day = 7
		}
		out.Days = append(out.Days, day)
		sig := ""
		for _, sl := range d.Slots {
			if from < 0 || sl.Start.minutes() < from {
				from = sl.Start.minutes()
				out.From = sl.Start.String()
			}
			if sl.End.minutes() > to {
				to = sl.End.minutes()
				out.To = sl.End.String()
			}
			sig += sl.Start.String() + "-" + sl.End.String() + ";"
		}
		if first == "" {
			first = sig
		} else if sig != first {
			uneven = true
		}
		if len(d.Slots) > 1 {
			uneven = true
		}
	}
	sort.Ints(out.Days)
	if len(out.Days) == 0 {
		return out, ""
	}
	if uneven {
		return out, "Saleshandy sends at different hours on different days; the widest window was kept."
	}
	return out, ""
}

func (s *session) senders(ctx context.Context, id string) ([]string, error) {
	var raw json.RawMessage
	if err := s.c.Get(ctx, "/v1/sequences/"+url.PathEscape(id)+"/email-accounts", nil, &raw); err != nil {
		return nil, err
	}
	type link struct {
		EmailAccount []emailAccount `json:"emailAccount"`
		FromEmail    string         `json:"fromEmail"`
	}
	raw = unwrap(raw)
	var links []link
	if len(raw) > 0 && raw[0] == '[' {
		if err := json.Unmarshal(raw, &links); err != nil {
			return nil, fmt.Errorf("%s: decode sequence email accounts: %w", label, err)
		}
	} else {
		var one link
		if err := json.Unmarshal(raw, &one); err != nil {
			return nil, fmt.Errorf("%s: decode sequence email accounts: %w", label, err)
		}
		links = []link{one}
	}
	var out []string
	seen := map[string]bool{}
	add := func(e string) {
		e = strings.ToLower(strings.TrimSpace(e))
		if e != "" && !seen[e] {
			seen[e] = true
			out = append(out, e)
		}
	}
	for _, l := range links {
		add(l.FromEmail)
		for _, a := range l.EmailAccount {
			add(a.FromEmail)
		}
	}
	return out, nil
}

func (s *session) Campaigns(ctx context.Context, ws source.Workspace) ([]source.Campaign, error) {
	seqs, err := s.sequences(ctx, ws)
	if err != nil {
		return nil, err
	}
	out := make([]source.Campaign, 0, len(seqs))
	for _, sq := range seqs {
		c, err := s.campaign(ctx, sq)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

func (s *session) campaign(ctx context.Context, sq sequence) (source.Campaign, error) {
	c := source.Campaign{ID: sq.ID, Name: sq.title(), Status: sq.status(), StopOnReply: true}
	if t, err := time.Parse(time.RFC3339, sq.CreatedAt); err == nil {
		c.CreatedAt = &t
	}
	steps, err := s.steps(ctx, sq)
	if err != nil {
		return c, err
	}
	c.Steps, c.UnmappedNotes = mapSteps(steps)

	set, err := s.settings(ctx, sq.ID)
	if err != nil {
		return c, err
	}
	for _, st := range set.Settings {
		v := strings.TrimSpace(string(st.Value))
		switch st.Code {
		case 1, 2:
			c.Unsubscribe = c.Unsubscribe || v != ""
		case 3:
			c.StopOnReply = v == "1"
		case 4:
			c.TrackClicks = v == "1"
		case 5:
			c.TrackOpens = v == "1"
		case 9:
			c.PlainText = v == "2"
		case 13:
			c.Unsubscribe = c.Unsubscribe || v == "1"
		}
	}
	if set.Schedule != nil && set.Schedule.ID != "" {
		sc, ok, err := s.schedule(ctx, string(set.Schedule.ID))
		if err != nil {
			return c, err
		}
		if ok {
			var note string
			c.Schedule, note = toSchedule(sc, set.Schedule.Timezone)
			if note != "" {
				c.UnmappedNotes = append(c.UnmappedNotes, note)
			}
		}
	}
	if c.Senders, err = s.senders(ctx, sq.ID); err != nil {
		return c, err
	}
	if n := s.prospectCount(ctx, sq.ID); n > 0 {
		c.LeadCount = n
	}
	return c, nil
}

func mapSteps(steps []step) ([]source.Step, []string) {
	var out []source.Step
	var notes []string
	prev := 0
	preheader, dropped := false, false
	for i, st := range steps {
		if !st.kind().email() {
			notes = append(notes, fmt.Sprintf("Step %d is a %s step and was not brought over.", i+1, st.kind().name()))
			continue
		}
		day, wait := st.timing()
		if day < 1 {
			day = 1
		}
		at := (day-1)*24*60 + wait
		delay := at - prev
		if delay < 0 {
			delay = 0
		}
		prev = at
		ms := source.Step{WaitDays: delay / (24 * 60), WaitMinutes: delay % (24 * 60)}
		for _, v := range st.Variants {
			subj, body := translate(v.Payload.Subject), translate(v.Payload.Content)
			dropped = dropped || len(subj.Dropped)+len(body.Dropped) > 0
			preheader = preheader || strings.TrimSpace(v.Payload.Preheader) != ""
			ms.Variants = append(ms.Variants, source.Variant{
				Subject: subj.Text, Body: body.Text, HTML: true,
				Disabled: v.Status != nil && *v.Status == 0,
			})
		}
		if len(ms.Variants) == 0 {
			notes = append(notes, fmt.Sprintf("Step %d has no email content in Saleshandy and was not brought over.", i+1))
			continue
		}
		out = append(out, ms)
	}
	if dropped {
		notes = append(notes, "Signature tags were removed; Warmbly adds the mailbox signature itself.")
	}
	if preheader {
		notes = append(notes, "Email preheaders are not brought over.")
	}
	return out, notes
}

var dialect = func() source.Dialect {
	f := source.CommonFields()
	f["sender_full_name"] = "Sender.Name"
	// Saleshandy's own spellings of fields the migration keeps as custom keys.
	f["linked_in"] = "linkedin"
	f["company_domain"] = "website"
	return source.Dialect{Fields: f}
}()

var (
	spinRE     = regexp.MustCompile(`(?is)\{spin\}(.*?)\{endspin\}`)
	fallbackRE = regexp.MustCompile(`\|\s*'([^'{}]*)'\s*\}\}`)
)

// translate turns {spin}a|b{endspin} into {a|b} and {{First Name|'there'}} into a
// plain fallback before the shared token rewrite.
func translate(text string) source.Translated {
	text = spinRE.ReplaceAllStringFunc(text, func(m string) string {
		parts := strings.Split(spinRE.FindStringSubmatch(m)[1], "|")
		for i, p := range parts {
			parts[i] = strings.TrimSpace(p)
		}
		return "{" + strings.Join(parts, "|") + "}"
	})
	text = fallbackRE.ReplaceAllString(text, "|$1}}")
	return source.Translate(text, dialect)
}
