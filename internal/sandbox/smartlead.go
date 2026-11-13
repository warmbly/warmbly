package sandbox

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// smartleadMock plays Smartlead's read API for "Migrate from another tool": an
// agency account with two clients and a few items no client owns. It answers in
// the shapes internal/pkg/outreachsource/smartlead decodes.
type smartleadMock struct {
	campaigns []slCampaign
	accounts  []slAccount
	leads     map[int][]slLead // by campaign id
	blocks    []slBlock
}

type slCampaign struct {
	ID, Client       int // Client 0 is unassigned
	Name, Status     string
	Created          time.Time
	Start            *time.Time
	TZ, From, To     string
	Days             []int
	MinGap, MaxLeads int
	Track            []string
	PlainText        bool
	Unsub            string
	Senders          []int
	Steps            []slStep
}

type slStep struct {
	Delay    int
	Variants [][2]string // subject, body; more than one is an A/B test
}

type slAccount struct {
	ID, Client, PerDay int
	Name, Email, Type  string
	Signature, Tag     string
	Warmup             bool
	ReplyTo            string
}

type slLead struct {
	ID                                 int
	First, Last, Email, Company, Title string
	Phone, Website, Location, LinkedIn string
	Custom                             map[string]any
	Status                             string
	Category                           int // 0 is none
	Unsub                              bool
	Sends                              []slSend
}

type slSend struct {
	Step                       int
	At                         time.Time
	Replied, Bounced, Unsubbed bool
}

type slBlock struct {
	Value, Source string
	Client        int
}

const (
	slNorthwind  = 7101
	slBrightline = 7102
)

var slClients = []struct {
	ID          int
	Name, Email string
}{
	{slNorthwind, "Northwind Studio", "ops@northwindstudio.test"},
	{slBrightline, "Brightline Legal", "it@brightlinelegal.test"},
}

// Smartlead's reply categories; the adapter maps them by name.
var slCategories = []struct {
	ID              int
	Name, Sentiment string
}{
	{1, "Interested", "positive"},
	{2, "Not Interested", "negative"},
	{3, "Meeting Request", "positive"},
	{4, "Out Of Office", ""},
	{5, "Do Not Contact", "negative"},
	{6, "Information Request", ""},
}

const (
	slCatInterested    = 1
	slCatNotInterested = 2
	slCatMeeting       = 3
)

func newSmartleadMock(now time.Time) *smartleadMock {
	day := now.UTC().Truncate(24 * time.Hour)
	ago := func(d int) time.Time { return day.AddDate(0, 0, -d).Add(14 * time.Hour) }
	start := func(d int) *time.Time { t := ago(d); return &t }
	weekdays := []int{1, 2, 3, 4, 5}
	m := &smartleadMock{
		accounts: []slAccount{
			{ID: 8001, Client: slNorthwind, Name: "Maya Collins", Email: "maya@northwindstudio.test", Type: "GMAIL", PerDay: 40, Signature: "<p>Maya Collins<br>Northwind Studio</p>", Tag: "Primary", Warmup: true},
			{ID: 8002, Client: slNorthwind, Name: "Jonas Berg", Email: "jonas@northwindstudio.test", Type: "GMAIL", PerDay: 35, Signature: "<p>Jonas Berg<br>Northwind Studio</p>", Warmup: true},
			{ID: 8003, Client: slNorthwind, Name: "Maya Collins", Email: "maya@trynorthwind.test", Type: "OUTLOOK", PerDay: 25, Tag: "Secondary domain", Warmup: false, ReplyTo: "maya@northwindstudio.test"},
			{ID: 8004, Client: slBrightline, Name: "Daniel Okafor", Email: "daniel@brightlinelegal.test", Type: "OUTLOOK", PerDay: 30, Signature: "<p>Daniel Okafor<br>Partner, Brightline Legal</p>", Warmup: true},
			{ID: 8005, Client: slBrightline, Name: "Priya Shah", Email: "priya@brightlinelegal.test", Type: "OUTLOOK", PerDay: 30, Signature: "<p>Priya Shah<br>Brightline Legal</p>", Warmup: true},
			{ID: 8006, Client: 0, Name: "Alex Rivera", Email: "alex@harborgrowth.test", Type: "GMAIL", PerDay: 50, Tag: "Agency", Warmup: true},
		},
		campaigns: []slCampaign{
			{
				ID: 9101, Client: slNorthwind, Name: "Q4 brand refresh outreach", Status: "ACTIVE",
				Created: ago(30), Start: start(21), TZ: "America/New_York", From: "9:00", To: "17:00", Days: weekdays,
				MinGap: 12, MaxLeads: 20, Track: []string{"DONT_LINK_CLICK"}, Unsub: "If this is not relevant, just let me know.",
				Senders: []int{8001, 8002, 8003},
				Steps: []slStep{
					{Delay: 0, Variants: [][2]string{
						{"Quick idea for {{company_name}}", "<p>{Hi|Hey} {{first_name|there}},</p><p>I came across {{company_name}} and noticed your site still runs the brand from a few years back. We refreshed identities for two {{Industry}} teams this year and both saw longer time on site.</p><p>Worth a 15 minute look?</p><p>%sender-firstname%</p>%signature%<p>%unsubscribe-text%</p>"},
						{"{{first_name}}, a thought on your brand", "<p>{Hi|Hey} {{first_name|there}},</p><p>Short one: we help {{Industry}} companies like {{company_name}} look as good as their product. Open to seeing a few examples?</p><p>%sender-firstname%</p>%signature%<p>%unsubscribe-text%</p>"},
					}},
					{Delay: 3, Variants: [][2]string{
						{"", "<p>Hi {{first_name|there}}, bumping this in case it got buried. {{#if team_size 'gte' 50}}With a team your size, a consistent brand saves a lot of back and forth.{{else}}Even a small refresh goes a long way.{{/if}}</p><p>%sender-firstname%</p>%signature%"},
					}},
					{Delay: 5, Variants: [][2]string{
						{"Closing the loop", "<p>Hi {{first_name|there}},</p><p>I will stop here. If a refresh lands on the roadmap for {{company_name}} next year, I would be glad to help.</p><p>%sender-name%</p>%signature%"},
					}},
				},
			},
			{
				ID: 9102, Client: slNorthwind, Name: "Agency partner program", Status: "PAUSED",
				Created: ago(45), Start: start(40), TZ: "Europe/London", From: "08:30", To: "16:30", Days: weekdays,
				MinGap: 15, MaxLeads: 10, PlainText: true,
				Senders: []int{8002},
				Steps: []slStep{
					{Delay: 0, Variants: [][2]string{
						{"Partnering with {{company_name}}", "Hi {{first_name|there}},\n\nWe pass on web development work we do not take on ourselves, and {{company_name}} keeps coming up. Would a referral arrangement be interesting?\n\n%sender-firstname%\n%signature%"},
					}},
					{Delay: 4, Variants: [][2]string{
						{"", "Hi {{first_name}}, any thoughts on this? Happy to share how the last partnership worked.\n\n%sender-firstname%"},
					}},
				},
			},
			{
				ID: 9103, Client: slBrightline, Name: "In-house counsel intro", Status: "ACTIVE",
				Created: ago(25), Start: start(18), TZ: "America/Chicago", From: "8:00", To: "16:00", Days: []int{1, 2, 3, 4},
				MinGap: 20, MaxLeads: 15, Track: []string{"DONT_EMAIL_OPEN", "DONT_LINK_CLICK"}, Unsub: "Reply stop and I will not follow up.",
				Senders: []int{8004, 8005},
				Steps: []slStep{
					{Delay: 0, Variants: [][2]string{
						{"Outside counsel for {{company_name}}", "<p>Hello {{first_name|there}},</p><p>Brightline supports in-house teams at {{Industry}} companies with contract overflow and employment matters, on a fixed monthly fee.</p><p>Would a short call next week be useful?</p><p>%sender-name%</p>%signature%<p>%unsubscribe-text%</p>"},
					}},
					{Delay: 4, Variants: [][2]string{
						{"", "<p>Hello {{first_name|there}}, following up on my note about overflow support for {{company_name}}.</p><p>%sender-name%</p>%signature%"},
					}},
					{Delay: 7, Variants: [][2]string{
						{"", "<p>{{first_name|there}}, one last note. If timing is the issue, I am glad to reconnect next quarter.</p><p>%sender-name%</p>%signature%"},
					}},
				},
			},
			{
				ID: 9104, Client: 0, Name: "Founder network intros", Status: "COMPLETED",
				Created: ago(60), Start: start(55), TZ: "UTC", From: "10:00", To: "18:00", Days: []int{1, 2, 3, 4, 5, 6},
				MinGap: 10, MaxLeads: 25,
				Senders: []int{8006},
				Steps: []slStep{
					{Delay: 0, Variants: [][2]string{
						{"{{company_name}} and Harbor Growth", "<p>{Hi|Hey} {{first_name|there}},</p><p>We run a small founder dinner series and thought of {{company_name}}. Interested in the next one?</p><p>%sender-firstname%</p>%signature%"},
					}},
					{Delay: 6, Variants: [][2]string{
						{"", "<p>Hi {{first_name|there}}, seats for the next dinner are filling up. Want me to hold one?</p><p>%sender-firstname%</p>%signature%"},
					}},
				},
			},
		},
		blocks: []slBlock{
			{Value: "competitor-agency.test", Source: "Manual"},
			{Value: "legal@bigclient.test", Source: "Manual"},
			{Value: "rivaldesign.test", Source: "Manual", Client: slNorthwind},
			{Value: "former.client@oldaccount.test", Source: "Smartlead.ai Bounce Detection", Client: slNorthwind},
			{Value: "opposingcounsel.test", Source: "Manual", Client: slBrightline},
			{Value: "*.conflictcheck.test", Source: "Manual", Client: slBrightline},
		},
		leads: map[int][]slLead{},
	}
	seed := []struct {
		campaign, count int
		industry        string
	}{
		{9101, 16, "retail"},
		{9102, 12, "software"},
		{9103, 18, "healthcare"},
		{9104, 12, "fintech"},
	}
	next := 0
	for _, s := range seed {
		var c slCampaign
		for _, cc := range m.campaigns {
			if cc.ID == s.campaign {
				c = cc
			}
		}
		for i := 0; i < s.count; i++ {
			m.leads[c.ID] = append(m.leads[c.ID], slMakeLead(c, i, next, s.industry, c.Start.AddDate(0, 0, 1)))
			next++
		}
	}
	return m
}

// slOutcomes covers every status the adapter maps within any campaign's first 12 leads.
var slOutcomes = []string{"progress", "new", "interested", "completed", "bounced", "replied", "progress", "unsubscribed", "new", "meeting", "completed", "not_interested", "progress", "new", "progress", "completed", "new", "progress"}

var slPeople = [][2]string{
	{"Olivia", "Hart"}, {"Ethan", "Moreno"}, {"Sophie", "Lang"}, {"Lucas", "Fischer"}, {"Grace", "Kim"}, {"Henry", "Adams"},
	{"Isla", "Novak"}, {"Mateo", "Silva"}, {"Chloe", "Dubois"}, {"Ryan", "Walsh"}, {"Amara", "Obi"}, {"Felix", "Wagner"},
	{"Nora", "Lindqvist"}, {"Samuel", "Greene"}, {"Zoe", "Patel"}, {"Daniel", "Cruz"}, {"Hannah", "Weiss"}, {"Owen", "Brooks"},
}

var slCompanies = []string{"Lumen Goods", "Cedar & Co", "Atlas Outfitters", "Pinecrest Labs", "Bluefin Apps", "Granite Health",
	"Harborview Clinic", "Northstar Pay", "Quill Finance", "Meridian Supply", "Oakline Software", "Vela Care",
	"Copperleaf", "Brightwater", "Summit Ledger", "Tidewell", "Ironbark", "Willowmere"}

var slTitles = []string{"Head of Marketing", "Founder", "COO", "General Counsel", "VP Operations", "CEO", "Brand Director", "Head of Legal"}

var slCities = []string{"Austin, TX", "Chicago, IL", "London, UK", "Denver, CO", "Toronto, ON", "Berlin, DE"}

// slMakeLead builds lead i of campaign c; n numbers it across the whole account.
func slMakeLead(c slCampaign, i, n int, industry string, first time.Time) slLead {
	p := slPeople[(i+c.ID)%len(slPeople)]
	company := slCompanies[n%len(slCompanies)]
	domain := strings.ToLower(strings.NewReplacer(" & ", "", " ", "").Replace(company)) + ".test"
	l := slLead{
		ID:       3000000 + n,
		First:    p[0],
		Last:     p[1],
		Email:    strings.ToLower(p[0]+"."+p[1]) + "@" + domain,
		Company:  company,
		Website:  "https://" + domain,
		Location: slCities[n%len(slCities)],
		Custom: map[string]any{
			"Industry":  industry,
			"team_size": 10 + (n*37)%240,
			"Job Title": slTitles[n%len(slTitles)],
		},
	}
	if n%3 == 0 {
		l.Phone = fmt.Sprintf("+1 555 01%02d", n%100)
		l.LinkedIn = "https://www.linkedin.com/in/" + strings.ToLower(p[0]+"-"+p[1])
	}
	if n%4 == 1 {
		l.Custom["Referral Source"] = "Conference list"
	}
	steps := len(c.Steps)
	sentAt := func(k int) time.Time {
		// Each step goes out after the previous one's delay, a few minutes apart per lead.
		at := first.Add(time.Duration(i*7) * time.Minute)
		for s := 1; s < k; s++ {
			at = at.AddDate(0, 0, c.Steps[s].Delay)
		}
		return at
	}
	send := func(k int) { l.Sends = append(l.Sends, slSend{Step: k, At: sentAt(k)}) }
	sendTo := func(k int) {
		for s := 1; s <= k; s++ {
			send(s)
		}
	}
	outcome := slOutcomes[i%len(slOutcomes)]
	// A finished campaign has nobody left waiting.
	if c.Status == "COMPLETED" && (outcome == "new" || outcome == "progress") {
		outcome = "completed"
	}
	switch outcome {
	case "new":
		l.Status = "STARTED"
	case "progress":
		l.Status = "INPROGRESS"
		sendTo(1 + i%max(steps-1, 1))
	case "completed":
		l.Status = "COMPLETED"
		sendTo(steps)
	case "bounced":
		l.Status = "STOPPED"
		sendTo(1)
		l.Sends[0].Bounced = true
	case "unsubscribed":
		l.Status = "STOPPED"
		sendTo(min(2, steps))
		l.Sends[len(l.Sends)-1].Unsubbed = true
		l.Unsub = true
	default: // a reply, with or without a category
		l.Status = "STOPPED"
		sendTo(min(1+i%2, steps))
		l.Sends[len(l.Sends)-1].Replied = true
		l.Category = map[string]int{"interested": slCatInterested, "meeting": slCatMeeting, "not_interested": slCatNotInterested}[outcome]
	}
	return l
}

func (s *smartleadMock) serve(w http.ResponseWriter, r *http.Request, path string) {
	if strings.TrimSpace(r.URL.Query().Get("api_key")) == "" {
		writeMock(w, http.StatusUnauthorized, map[string]any{"message": "Invalid API Key"})
		return
	}
	if r.Method != http.MethodGet {
		writeMock(w, http.StatusMethodNotAllowed, map[string]any{"error": "The sandbox only plays Smartlead's read API"})
		return
	}
	q := r.URL.Query()
	off, limit := slInt(q.Get("offset"), 0), slInt(q.Get("limit"), 100)
	switch {
	case path == "/leads/fetch-categories":
		out := make([]map[string]any, 0, len(slCategories))
		for _, c := range slCategories {
			out = append(out, map[string]any{"id": c.ID, "name": c.Name, "sentiment_type": slNullable(c.Sentiment), "created_at": "2024-01-15T10:30:00.000Z"})
		}
		writeMock(w, http.StatusOK, out)
	case path == "/client/" || path == "/client":
		out := make([]map[string]any, 0, len(slClients))
		for _, c := range slClients {
			out = append(out, map[string]any{"id": c.ID, "name": c.Name, "email": c.Email, "created_at": "2025-03-01T10:00:00.000Z"})
		}
		writeMock(w, http.StatusOK, out)
	case path == "/campaigns/" || path == "/campaigns":
		out := make([]map[string]any, 0, len(s.campaigns))
		for _, c := range s.campaigns {
			out = append(out, s.campaignJSON(c))
		}
		writeMock(w, http.StatusOK, out)
	case path == "/email-accounts/" || path == "/email-accounts":
		var out []map[string]any
		for _, a := range slPage(s.accounts, off, limit) {
			out = append(out, accountJSON(a))
		}
		writeMock(w, http.StatusOK, slList(out))
	case path == "/leads/get-domain-block-list":
		var out []map[string]any
		for i, b := range slPage(s.blocks, off, limit) {
			out = append(out, map[string]any{"id": 228000000 + off + i, "email_or_domain": b.Value, "source": b.Source,
				"client_id": slOptID(b.Client), "created_at": "2025-09-01T12:00:00.000Z"})
		}
		writeMock(w, http.StatusOK, slList(out))
	case strings.HasPrefix(path, "/email-accounts/"):
		id := slInt(strings.Trim(strings.TrimPrefix(path, "/email-accounts/"), "/"), -1)
		for _, a := range s.accounts {
			if a.ID == id {
				writeMock(w, http.StatusOK, accountJSON(a))
				return
			}
		}
		writeMock(w, http.StatusNotFound, map[string]any{"error": "Email account not found"})
	case strings.HasPrefix(path, "/campaigns/"):
		id, rest, _ := strings.Cut(strings.TrimPrefix(path, "/campaigns/"), "/")
		c, ok := s.campaign(slInt(id, -1))
		if !ok {
			writeMock(w, http.StatusNotFound, map[string]any{"error": "Campaign not found"})
			return
		}
		s.campaignPart(w, c, strings.Trim(rest, "/"), off, limit)
	default:
		writeMock(w, http.StatusNotFound, map[string]any{"error": "Resource not found"})
	}
}

func (s *smartleadMock) campaign(id int) (slCampaign, bool) {
	for _, c := range s.campaigns {
		if c.ID == id {
			return c, true
		}
	}
	return slCampaign{}, false
}

func (s *smartleadMock) campaignPart(w http.ResponseWriter, c slCampaign, part string, off, limit int) {
	switch part {
	case "":
		writeMock(w, http.StatusOK, s.campaignJSON(c))
	case "sequences":
		out := make([]map[string]any, 0, len(c.Steps))
		for i, st := range c.Steps {
			seq := map[string]any{
				"id": c.ID*10 + i + 1, "email_campaign_id": c.ID, "seq_number": i + 1,
				"subject": st.Variants[0][0], "email_body": st.Variants[0][1],
				"seq_delay_details": map[string]any{"delay_in_days": st.Delay}, "sequence_variants": []any{},
			}
			if len(st.Variants) > 1 {
				vars := make([]map[string]any, 0, len(st.Variants))
				for j, v := range st.Variants {
					vars = append(vars, map[string]any{"id": c.ID*100 + j, "variant_label": string(rune('A' + j)), "subject": v[0], "email_body": v[1], "is_deleted": false})
				}
				seq["sequence_variants"] = vars
			}
			out = append(out, seq)
		}
		writeMock(w, http.StatusOK, out)
	case "email-accounts":
		var out []map[string]any
		for _, id := range c.Senders {
			for _, a := range s.accounts {
				if a.ID == id {
					out = append(out, map[string]any{"id": a.ID, "from_email": a.Email, "from_name": a.Name, "type": a.Type, "warmup_enabled": a.Warmup})
				}
			}
		}
		writeMock(w, http.StatusOK, map[string]any{"ok": true, "data": out})
	case "leads":
		leads := s.leads[c.ID]
		var out []map[string]any
		for i, l := range slPage(leads, off, limit) {
			out = append(out, map[string]any{
				"campaign_lead_map_id": 5000000 + l.ID, "lead_category_id": slOptID(l.Category), "status": l.Status,
				"created_at": c.Start.Add(time.Duration(off+i) * time.Minute).Format(time.RFC3339),
				"lead": map[string]any{
					"id": l.ID, "first_name": l.First, "last_name": l.Last, "email": l.Email, "phone_number": slNullable(l.Phone),
					"company_name": l.Company, "website": l.Website, "location": l.Location, "linkedin_profile": slNullable(l.LinkedIn),
					"company_url": l.Website, "custom_fields": l.Custom, "is_unsubscribed": l.Unsub, "unsubscribed_client_id_map": nil,
				},
			})
		}
		writeMock(w, http.StatusOK, map[string]any{"total_leads": strconv.Itoa(len(leads)), "offset": off, "limit": limit, "data": slList(out)})
	case "statistics":
		var rows []map[string]any
		for _, l := range s.leads[c.ID] {
			for _, sd := range l.Sends {
				rows = append(rows, map[string]any{
					"lead_name": l.First + " " + l.Last, "lead_email": l.Email, "lead_category": nil,
					"sequence_number": sd.Step, "email_subject": c.Steps[sd.Step-1].Variants[0][0],
					"sent_time": sd.At.Format(time.RFC3339), "open_time": nil, "click_time": nil, "reply_time": nil,
					"is_opened": sd.Replied, "is_clicked": false, "is_replied": sd.Replied, "is_bounced": sd.Bounced,
					"sender_bounce": false, "is_unsubscribed": sd.Unsubbed,
				})
			}
		}
		page := slPage(rows, off, limit)
		writeMock(w, http.StatusOK, map[string]any{"total_stats": strconv.Itoa(len(rows)), "offset": off, "limit": limit, "data": slList(page)})
	default:
		writeMock(w, http.StatusNotFound, map[string]any{"error": "Resource not found"})
	}
}

func (s *smartleadMock) campaignJSON(c slCampaign) map[string]any {
	days := make([]int, len(c.Days))
	copy(days, c.Days)
	track := c.Track
	if track == nil {
		track = []string{}
	}
	return map[string]any{
		"id": c.ID, "user_id": 196026, "name": c.Name, "status": c.Status,
		"created_at": c.Created.Format(time.RFC3339), "updated_at": c.Created.Format(time.RFC3339),
		"track_settings":       track,
		"scheduler_cron_value": map[string]any{"tz": c.TZ, "days": days, "startHour": c.From, "endHour": c.To},
		"min_time_btwn_emails": c.MinGap, "max_leads_per_day": c.MaxLeads,
		"stop_lead_settings": "REPLY_TO_AN_EMAIL", "schedule_start_time": c.Start.Format(time.RFC3339),
		"send_as_plain_text": c.PlainText, "follow_up_percentage": 100, "unsubscribe_text": slNullable(c.Unsub),
		"parent_campaign_id": nil, "client_id": slOptID(c.Client),
	}
}

func accountJSON(a slAccount) map[string]any {
	smtp := map[string]string{"GMAIL": "smtp.gmail.com", "OUTLOOK": "smtp.office365.com"}[a.Type]
	tags := []map[string]any{}
	if a.Tag != "" {
		tags = append(tags, map[string]any{"tag_id": a.ID, "tag_name": a.Tag, "tag_color": "#B1FCCF"})
	}
	warmup := "INACTIVE"
	if a.Warmup {
		warmup = "ACTIVE"
	}
	return map[string]any{
		"id": a.ID, "from_name": a.Name, "from_email": a.Email, "username": a.Email,
		"smtp_host": smtp, "smtp_port": 587, "type": a.Type, "message_per_day": a.PerDay,
		"signature": slNullable(a.Signature), "different_reply_to_address": slNullable(a.ReplyTo),
		"client_id": slOptID(a.Client), "tags": tags,
		"warmup_details": map[string]any{"status": warmup, "warmup_reputation": "100%"},
	}
}

func slPage[T any](all []T, off, limit int) []T {
	if off >= len(all) || limit <= 0 {
		return nil
	}
	return all[off:min(off+limit, len(all))]
}

// slList keeps an empty page a JSON array, as Smartlead sends it.
func slList(v []map[string]any) []map[string]any {
	if v == nil {
		return []map[string]any{}
	}
	return v
}

func slInt(s string, def int) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

func slNullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// slOptID writes an unset client or category id as null.
func slOptID(id int) any {
	if id == 0 {
		return nil
	}
	return id
}
