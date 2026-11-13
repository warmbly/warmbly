package lemlist

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	source "github.com/warmbly/warmbly/internal/pkg/outreachsource"
)

type campaign struct {
	ID         string     `json:"_id"`
	Name       string     `json:"name"`
	Status     string     `json:"status"`
	SequenceID string     `json:"sequenceId"`
	Timezone   string     `json:"timezone"`
	CreatedAt  *time.Time `json:"createdAt"`
}

// listCampaigns pages GET /campaigns, which answers a bare array or, with
// version=v2, an object holding the array.
func (s *session) listCampaigns(ctx context.Context) ([]campaign, error) {
	var all []campaign
	for offset := 0; ; offset += pageSize {
		q := url.Values{"version": {"v2"}, "offset": {strconv.Itoa(offset)}, "limit": {strconv.Itoa(pageSize)}}
		var raw json.RawMessage
		if err := s.c.Get(ctx, "/campaigns", q, &raw); err != nil {
			return nil, err
		}
		page, err := decodeCampaigns(raw)
		if err != nil {
			return nil, err
		}
		all = append(all, page...)
		if len(page) < pageSize {
			return all, nil
		}
	}
}

func decodeCampaigns(raw json.RawMessage) ([]campaign, error) {
	var page []campaign
	if err := json.Unmarshal(raw, &page); err == nil {
		return page, nil
	}
	var wrapped struct {
		Campaigns []campaign `json:"campaigns"`
	}
	if err := json.Unmarshal(raw, &wrapped); err != nil {
		return nil, fmt.Errorf("%s: decode campaigns: %w", label, err)
	}
	return wrapped.Campaigns, nil
}

func campaignStatus(s string) source.CampaignStatus {
	switch s {
	case "running":
		return source.CampaignActive
	case "paused", "errors":
		return source.CampaignPaused
	case "ended":
		return source.CampaignCompleted
	case "archived":
		return source.CampaignArchived
	}
	return source.CampaignDraft
}

type sequence struct {
	ID       string `json:"_id"`
	Level    int    `json:"level"`
	ParentID string `json:"parentId"`
	Steps    []step `json:"steps"`
}

type step struct {
	ID         string          `json:"_id"`
	Delay      int             `json:"delay"`
	Type       string          `json:"type"`
	Index      int             `json:"index"`
	SequenceID string          `json:"sequenceId"`
	Subject    string          `json:"subject"`
	Message    string          `json:"message"`
	ABTest     json.RawMessage `json:"abTest"`
	Manual     bool            `json:"manual"`
	CustomName string          `json:"customName"`
	Conditions []struct {
		SequenceID string `json:"sequenceId"`
	} `json:"conditions"`
}

type abVariant struct {
	Subject   string `json:"subject"`
	Message   string `json:"message"`
	PlainText bool   `json:"plainText"`
}

type schedule struct {
	SecondsToWait int    `json:"secondsToWait"`
	Timezone      string `json:"timezone"`
	Start         string `json:"start"`
	End           string `json:"end"`
	Weekdays      []int  `json:"weekdays"`
}

type settings struct {
	Timezone string `json:"timezone"`
	General  struct {
		AddUnsubscribeLink bool `json:"addUnsubscribeLink"`
	} `json:"general"`
	Tracking struct {
		TrackOpens  bool `json:"trackOpens"`
		TrackClicks bool `json:"trackClicks"`
	} `json:"tracking"`
	OnReplied struct {
		CampaignProgress string `json:"campaignProgress"`
	} `json:"onReplied"`
}

func (s *session) Campaigns(ctx context.Context, _ source.Workspace) ([]source.Campaign, error) {
	camps, err := s.listCampaigns(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]source.Campaign, 0, len(camps))
	for _, c := range camps {
		mc, err := s.campaign(ctx, c)
		if err != nil {
			return nil, err
		}
		out = append(out, mc)
	}
	return out, nil
}

func (s *session) campaign(ctx context.Context, c campaign) (source.Campaign, error) {
	out := source.Campaign{
		ID: c.ID, Name: c.Name, Status: campaignStatus(c.Status), CreatedAt: c.CreatedAt,
		StopOnReply: true, LeadCount: -1,
	}
	var set settings
	switch err := s.c.Get(ctx, "/campaigns/"+url.PathEscape(c.ID)+"/settings", nil, &set); {
	case err == nil:
		out.TrackOpens = set.Tracking.TrackOpens
		out.TrackClicks = set.Tracking.TrackClicks
		out.Unsubscribe = set.General.AddUnsubscribeLink
		out.StopOnReply = set.OnReplied.CampaignProgress != "continue"
	case isRefusal(err):
		out.UnmappedNotes = append(out.UnmappedNotes, "Tracking and reply settings need the lemlist Email Pro plan to read, so the defaults were used.")
	default:
		return out, err
	}

	var seqs map[string]sequence
	switch err := s.c.Get(ctx, "/campaigns/"+url.PathEscape(c.ID)+"/sequences", nil, &seqs); {
	case err == nil:
	case isRefusal(err):
		out.UnmappedNotes = append(out.UnmappedNotes, "lemlist did not return a sequence for this campaign.")
	default:
		return out, err
	}
	if err := s.mapSteps(ctx, &out, c, seqs); err != nil {
		return out, err
	}

	var scheds []schedule
	switch err := s.c.Get(ctx, "/campaigns/"+url.PathEscape(c.ID)+"/schedules/", nil, &scheds); {
	case err == nil:
		out.Schedule = mapSchedule(scheds, c.Timezone, set.Timezone)
		if len(scheds) > 1 {
			out.UnmappedNotes = append(out.UnmappedNotes, fmt.Sprintf("The campaign has %d schedules in lemlist; only the first was brought over.", len(scheds)))
		}
	case isRefusal(err):
	default:
		return out, err
	}

	senders, err := s.senders(ctx, c.ID)
	if err != nil && !isRefusal(err) {
		return out, err
	}
	for _, sn := range senders {
		for _, ch := range sn.Channels {
			if ch.Type == "email" && sn.InPool && ch.InPool {
				out.Senders = append(out.Senders, strings.ToLower(ch.Label))
			}
		}
	}
	return out, nil
}

// mainSequence is the top-level sequence; condition branches hang off it.
func mainSequence(c campaign, seqs map[string]sequence) (sequence, bool) {
	if sq, ok := seqs[c.SequenceID]; ok && c.SequenceID != "" {
		return sq, true
	}
	ids := make([]string, 0, len(seqs))
	for id := range seqs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if seqs[id].Level == 0 && seqs[id].ParentID == "" {
			return seqs[id], true
		}
	}
	return sequence{}, false
}

func (s *session) mapSteps(ctx context.Context, out *source.Campaign, c campaign, seqs map[string]sequence) error {
	main, ok := mainSequence(c, seqs)
	if !ok {
		return nil
	}
	steps := append([]step(nil), main.Steps...)
	sort.SliceStable(steps, func(i, j int) bool { return steps[i].Index < steps[j].Index })
	carry := 0
	var dropped []string
	for n, st := range steps {
		pos := n + 1
		if st.Type == "conditional" {
			out.UnmappedNotes = append(out.UnmappedNotes, fmt.Sprintf("Step %d is a condition that splits the sequence into %d branches; the steps inside the branches were not brought over.", pos, len(st.Conditions)))
			break
		}
		if st.Type != "email" {
			carry += st.Delay
			out.UnmappedNotes = append(out.UnmappedNotes, fmt.Sprintf("Step %d is a %s step and was not brought over.", pos, stepName(st)))
			continue
		}
		mapped := source.Step{WaitDays: carry + st.Delay}
		carry = 0
		a := source.Variant{Subject: st.Subject, Body: st.Message, HTML: true}
		mapped.Variants = []source.Variant{a}
		switch winner := abState(st.ABTest); winner {
		case "running", "B":
			var b abVariant
			path := "/sequences/" + url.PathEscape(main.ID) + "/steps/" + url.PathEscape(st.ID) + "/ab-test"
			err := s.c.Get(ctx, path, nil, &b)
			switch {
			case err == nil && winner == "B":
				a.Disabled = true
				mapped.Variants = []source.Variant{{Subject: b.Subject, Body: b.Message, HTML: !b.PlainText}, a}
			case err == nil:
				mapped.Variants = append(mapped.Variants, source.Variant{Subject: b.Subject, Body: b.Message, HTML: !b.PlainText})
			case isRefusal(err):
				out.UnmappedNotes = append(out.UnmappedNotes, fmt.Sprintf("Step %d has an A/B test whose variant B lemlist did not return.", pos))
			default:
				return err
			}
		}
		if st.Manual {
			out.UnmappedNotes = append(out.UnmappedNotes, fmt.Sprintf("Step %d waited for a manual task in lemlist and will send on its own in Warmbly.", pos))
		}
		for i := range mapped.Variants {
			v := &mapped.Variants[i]
			subj, sd, sl := translate(v.Subject)
			body, bd, bl := translate(v.Body)
			v.Subject, v.Body = subj, body
			dropped = append(append(dropped, sd...), bd...)
			if sl || bl {
				out.UnmappedNotes = append(out.UnmappedNotes, fmt.Sprintf("Step %d uses Liquid conditions, which were kept as written and need rewriting.", pos))
			}
		}
		out.Steps = append(out.Steps, mapped)
	}
	if len(dropped) > 0 {
		out.UnmappedNotes = append(out.UnmappedNotes, "Removed "+strings.Join(uniq(dropped), ", ")+", which Warmbly adds from the sending mailbox.")
	}
	return nil
}

// abState reads a step's abTest: true while running, "A" or "B" once decided.
func abState(raw json.RawMessage) string {
	var b bool
	if json.Unmarshal(raw, &b) == nil {
		if b {
			return "running"
		}
		return ""
	}
	var w string
	if json.Unmarshal(raw, &w) == nil {
		return w
	}
	return ""
}

func stepName(st step) string {
	names := map[string]string{
		"linkedinSend": "LinkedIn message", "linkedinInvite": "LinkedIn invite", "linkedinVisit": "LinkedIn profile visit",
		"linkedinInMail": "LinkedIn InMail", "linkedinVoiceNote": "LinkedIn voice note", "phone": "call",
		"manual": "manual task", "whatsapp": "WhatsApp", "api": "API call", "enrichment": "enrichment",
	}
	if n, ok := names[st.Type]; ok {
		return n
	}
	if st.Type == "" {
		return "non-email"
	}
	return st.Type
}

func mapSchedule(scheds []schedule, tzs ...string) *source.Schedule {
	if len(scheds) == 0 {
		return nil
	}
	sc := scheds[0]
	tz := sc.Timezone
	for _, t := range tzs {
		if tz == "" {
			tz = t
		}
	}
	return &source.Schedule{
		Timezone: tz, Days: sc.Weekdays, From: sc.Start, To: sc.End,
		MinGapMinutes: (sc.SecondsToWait + 59) / 60,
	}
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
