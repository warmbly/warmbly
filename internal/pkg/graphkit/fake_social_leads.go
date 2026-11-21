package graphkit

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ---- search catalog -------------------------------------------------------

type fakeFilter struct {
	Key      string       `json:"key"`
	Label    string       `json:"label"`
	Kind     string       `json:"kind"`
	Suggest  bool         `json:"suggest,omitempty"`
	FreeText bool         `json:"free_text,omitempty"`
	Options  []fakeOption `json:"options,omitempty"`
	Exclude  bool         `json:"exclude"`
	Licence  *string      `json:"licence"`
}

type fakeOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type fakeGroup struct {
	Key     string       `json:"key"`
	Label   string       `json:"label"`
	Filters []fakeFilter `json:"filters"`
}

type fakeSource struct {
	Source   string      `json:"source"`
	Label    string      `json:"label"`
	Category string      `json:"category"`
	Licence  *string     `json:"licence"`
	Cap      int         `json:"cap"`
	PageSize int         `json:"page_size"`
	Groups   []fakeGroup `json:"groups"`
}

func opts(labels ...string) []fakeOption {
	out := make([]fakeOption, len(labels))
	for i, l := range labels {
		out[i] = fakeOption{ID: fakeSlug(l), Label: l}
	}
	return out
}

var (
	fakeSeniorities = opts("Owner / Partner", "CXO", "Vice President", "Director", "Experienced Manager", "Entry Level Manager", "Senior", "Entry Level")
	fakeFunctions   = opts("Business Development", "Engineering", "Finance", "Human Resources", "Marketing", "Operations", "Product Management", "Sales")
	fakeHeadcounts  = opts("1-10", "11-50", "51-200", "201-500", "501-1000", "1001-5000", "5001-10000", "10001+")
	fakeGeographies = []string{"London, United Kingdom", "United Kingdom", "New York, United States", "San Francisco Bay Area", "United States",
		"Berlin, Germany", "Germany", "Paris, France", "Amsterdam, Netherlands", "Toronto, Canada", "Sydney, Australia", "Singapore"}
	fakeIndustries = []string{"Software Development", "Financial Services", "IT Services and IT Consulting", "Marketing Services",
		"Hospitals and Health Care", "Staffing and Recruiting", "Advertising Services", "Retail", "Manufacturing", "Venture Capital and Private Equity"}
	fakeTitles = []string{"Head of Growth", "Founder", "Co-Founder", "Chief Executive Officer", "Chief Technology Officer", "VP of Sales",
		"Head of Marketing", "Sales Director", "Account Executive", "Talent Acquisition Manager", "Head of People", "Product Manager"}
	fakeCompanies = []string{"Northwind", "Contoso Labs", "Fabrikam", "Globex", "Initech", "Umbrella Health", "Hooli", "Stark Analytics",
		"Wayne Logistics", "Acme Robotics", "Soylent Foods", "Tyrell Systems"}
)

func fakeCatalog() []fakeSource {
	sn, rec := LicenceSalesNavigator, LicenceRecruiter
	suggest := func(key, label string, free bool) fakeFilter {
		return fakeFilter{Key: key, Label: label, Kind: "typeahead", Suggest: true, FreeText: free, Exclude: true}
	}
	options := func(key, label string, o []fakeOption) fakeFilter {
		return fakeFilter{Key: key, Label: label, Kind: "options", Options: o, Exclude: true}
	}
	toggle := func(key, label string) fakeFilter { return fakeFilter{Key: key, Label: label, Kind: "toggle"} }
	people := []fakeGroup{
		{Key: "role", Label: "Role", Filters: []fakeFilter{suggest("current_title", "Current job title", true), options("seniority", "Seniority level", fakeSeniorities), options("function", "Function", fakeFunctions)}},
		{Key: "company", Label: "Company", Filters: []fakeFilter{suggest("current_company", "Current company", true), options("company_headcount", "Company headcount", fakeHeadcounts), suggest("industry", "Industry", false)}},
		{Key: "personal", Label: "Personal", Filters: []fakeFilter{suggest("geography", "Geography", false)}},
		{Key: "spotlights", Label: "Spotlights", Filters: []fakeFilter{toggle("changed_jobs", "Changed jobs in the last 90 days"), toggle("posted_recently", "Posted on LinkedIn in the last 30 days")}},
	}
	classic := []fakeGroup{
		{Key: "people", Label: "People", Filters: []fakeFilter{suggest("current_title", "Title", true), suggest("current_company", "Current company", true), suggest("geography", "Location", false), suggest("industry", "Industry", false)}},
	}
	accounts := []fakeGroup{
		{Key: "company", Label: "Company attributes", Filters: []fakeFilter{options("company_headcount", "Company headcount", fakeHeadcounts), suggest("industry", "Industry", false), suggest("geography", "Headquarters location", false)}},
	}
	return []fakeSource{
		{Source: SourceSalesNavigatorLeads, Label: "Sales Navigator leads", Category: "people", Licence: &sn, Cap: 2500, PageSize: 25, Groups: people},
		{Source: SourceSalesNavigatorAccounts, Label: "Sales Navigator accounts", Category: "companies", Licence: &sn, Cap: 1000, PageSize: 25, Groups: accounts},
		{Source: SourceClassicPeople, Label: "LinkedIn people", Category: "people", Cap: 1000, PageSize: 10, Groups: classic},
		{Source: SourceRecruiterPeople, Label: "Recruiter people", Category: "people", Licence: &rec, Cap: 1000, PageSize: 25, Groups: people},
	}
}

func fakeSourceOf(source string) *fakeSource {
	for _, s := range fakeCatalog() {
		if s.Source == source {
			return &s
		}
	}
	return nil
}

func (f *Fake) SearchCatalog(_ context.Context, externalID string) (json.RawMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.socialAccountLocked(externalID); err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(map[string]any{"sources": fakeCatalog()})
	return raw, nil
}

// ---- search ---------------------------------------------------------------

func readQuery(raw json.RawMessage) (Query, *fakeSource, error) {
	var q Query
	if len(raw) == 0 || json.Unmarshal(raw, &q) != nil {
		return q, nil, fakeErrDetails(http.StatusBadRequest, CodeInvalidRequest, "query is required", map[string]any{"fields": []string{"query"}})
	}
	src := fakeSourceOf(q.Source)
	if src == nil {
		return q, nil, fakeErrDetails(http.StatusBadRequest, CodeInvalidRequest, "unknown source", map[string]any{"fields": []string{"query.source"}})
	}
	return q, src, nil
}

// searchAccountLocked picks the account a search runs on: the one named, or one holding the licence with budget left.
func (f *Fake) searchAccountLocked(externalID string, accountID *string, licence *string, units int) (*fakeSocialAccount, error) {
	day := f.now().UTC().Format("2006-01-02")
	holds := func(a *fakeSocialAccount) bool {
		return a.a.Channel == ChannelLinkedIn && (licence == nil || slices.Contains(a.a.Licences, *licence))
	}
	left := func(a *fakeSocialAccount) int {
		if a.searchDay != day {
			a.searchDay, a.searchUsed = day, 0
		}
		return a.a.Limits["search"] - a.searchUsed
	}
	if accountID != nil && *accountID != "" {
		a, err := f.liveAccountLocked(externalID, *accountID)
		if err != nil {
			return nil, err
		}
		if a.a.Status != SocialActive {
			return nil, fakeErr(http.StatusConflict, CodeAccountUnavailable, "this account is not active")
		}
		if !holds(a) {
			return nil, fakeErr(http.StatusConflict, CodeLicenceRequired, "this account does not hold the licence the source needs")
		}
		if left(a) < units {
			return nil, fakeErr(http.StatusConflict, CodeSearchLimitReached, "this account's search budget is used up for today")
		}
		return a, nil
	}
	var licensed []*fakeSocialAccount
	for _, a := range f.social.accounts {
		if a.external == externalID && a.a.Status == SocialActive && holds(a) {
			licensed = append(licensed, a)
		}
	}
	if len(licensed) == 0 {
		return nil, fakeErr(http.StatusConflict, CodeLicenceRequired, "no connected account holds the licence this source needs")
	}
	sort.Slice(licensed, func(i, j int) bool { return licensed[i].a.ID < licensed[j].a.ID })
	for _, a := range licensed {
		if left(a) >= units {
			return a, nil
		}
	}
	return nil, fakeErr(http.StatusConflict, CodeSearchLimitReached, "every licensed account is out of search budget for today")
}

func queryDigest(q Query) string {
	q.URL = nil
	b, _ := json.Marshal(q)
	return strconv.FormatUint(fakeHash(string(b))%1_000_000_007, 36)
}

func fakePerson(seed string) Person {
	h := fakeHash(seed)
	first, last := fakeFirstNames[h%uint64(len(fakeFirstNames))], fakeLastNames[(h/17)%uint64(len(fakeLastNames))]
	public := strings.ToLower(first + "-" + last + "-" + strconv.FormatUint(h%900+100, 10))
	switch h % 9 {
	case 0:
		public += "-accept"
	case 4:
		public += "-reply"
	}
	dist := []string{"1st", "2nd", "2nd", "3rd", "out_of_network"}[(h/3)%5]
	return Person{ProviderID: "ACwAAA" + strings.ToUpper(strconv.FormatUint(h%0xffffffff, 16)), PublicID: public,
		FullName: first + " " + last, FirstName: first, LastName: last,
		Headline:   fakeTitles[(h/5)%uint64(len(fakeTitles))] + " at " + fakeCompanies[(h/11)%uint64(len(fakeCompanies))],
		Location:   fakeGeographies[(h/13)%uint64(len(fakeGeographies))],
		ProfileURL: "https://www.linkedin.com/in/" + public, NetworkDistance: dist,
		Premium: h%4 == 0, OpenProfile: h%6 == 0, CanInmail: h%3 != 2}
}

func fakeCompany(name string) Company {
	h := fakeHash(name)
	slug := fakeSlug(name)
	head, _ := json.Marshal(fakeHeadcounts[h%uint64(len(fakeHeadcounts))].Label)
	return Company{ProviderID: strconv.FormatUint(h%90_000_000+10_000_000, 10), Name: name,
		Industry: fakeIndustries[h%uint64(len(fakeIndustries))], Headcount: head,
		Location: fakeGeographies[(h/7)%uint64(len(fakeGeographies))], Website: "https://" + slug + ".example",
		LinkedInURL: "https://www.linkedin.com/company/" + slug}
}

func fakeHit(source, seed string) Hit {
	if source == SourceSalesNavigatorAccounts {
		c := fakeCompany(fakeCompanies[fakeHash(seed)%uint64(len(fakeCompanies))] + " " + strconv.FormatUint(fakeHash(seed)%97, 10))
		return Hit{Key: "co:" + c.ProviderID, Company: &c}
	}
	p := fakePerson(seed)
	h := fakeHash(seed)
	company := fakeCompanies[(h/11)%uint64(len(fakeCompanies))]
	c := fakeCompany(company)
	title := strings.SplitN(p.Headline, " at ", 2)[0]
	reasons := []ReachReason{}
	score := 30
	if p.OpenProfile {
		reasons = append(reasons, ReachReason{Code: "open_profile", Label: "Open Profile: free InMail", Weight: 18})
		score += 18
	}
	if p.NetworkDistance == "1st" || p.NetworkDistance == "2nd" {
		reasons = append(reasons, ReachReason{Code: "close_network", Label: "In your network (" + p.NetworkDistance + ")", Weight: 20})
		score += 20
	}
	if h%5 == 0 {
		reasons = append(reasons, ReachReason{Code: "posted_recently", Label: "Posted in the last 30 days", Weight: 12})
		score += 12
	}
	level := "low"
	if score >= 60 {
		level = "high"
	} else if score >= 45 {
		level = "medium"
	}
	return Hit{Key: "li:" + p.ProviderID, Person: &p, Company: &c,
		Insights: &Insights{CurrentTitle: title, CurrentCompany: company, CurrentCompanyID: c.ProviderID, Industry: c.Industry,
			SharedConnections: int(h % 14), RecentlyHired: h%7 == 0, PostedRecently: h%5 == 0, OpenToWork: h%13 == 0},
		Reach: &Reach{Score: score, Level: level, Reasons: reasons}}
}

func fakeSearchURL(q Query) string {
	v := url.Values{}
	if q.Keywords != "" {
		v.Set("keywords", q.Keywords)
	}
	path := "/sales/search/people"
	switch q.Source {
	case SourceSalesNavigatorAccounts:
		path = "/sales/search/company"
	case SourceClassicPeople:
		path = "/search/results/people/"
	case SourceRecruiterPeople:
		path = "/talent/search"
	}
	u := "https://www.linkedin.com" + path
	if len(v) > 0 {
		u += "?" + v.Encode()
	}
	return u
}

func (f *Fake) SearchPeople(_ context.Context, externalID string, in SearchInput, key string) (*SearchPage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.socialAccountLocked(externalID); err != nil {
		return nil, err
	}
	q, src, err := readQuery(in.Query)
	if err != nil {
		return nil, err
	}
	limit := in.Limit
	if limit <= 0 {
		limit = src.PageSize
	}
	if limit > 100 {
		return nil, fakeErr(http.StatusBadRequest, CodeInvalidRequest, "limit is at most 100")
	}
	digest, offset, accountID := queryDigest(q), 0, in.AccountID
	if in.Cursor != nil && *in.Cursor != "" {
		parts := strings.Split(*in.Cursor, ".")
		if len(parts) != 3 {
			return nil, fakeErr(http.StatusBadRequest, CodeInvalidRequest, "invalid cursor")
		}
		if parts[1] != digest {
			return nil, fakeErr(http.StatusConflict, CodeStaleCursor, "the query changed since this cursor; start again")
		}
		n, err := strconv.Atoi(parts[2])
		if err != nil || n < 0 {
			return nil, fakeErr(http.StatusBadRequest, CodeInvalidRequest, "invalid cursor")
		}
		offset, accountID = n, &parts[0]
	}
	total := min(int(fakeHash(digest)%1800)+60, src.Cap*2)
	reach := min(total, src.Cap)
	n := max(0, min(limit, reach-offset))
	a, err := f.searchAccountLocked(externalID, accountID, src.Licence, max(n, 1))
	if err != nil {
		return nil, err
	}
	a.searchUsed += n
	page := &SearchPage{Data: make([]Hit, 0, n), Total: &total, Cap: src.Cap, Account: &AccountRef{ID: a.a.ID, DisplayName: a.a.DisplayName}}
	for i := offset; i < offset+n; i++ {
		page.Data = append(page.Data, fakeHit(q.Source, digest+":"+strconv.Itoa(i)))
	}
	if offset+n < reach {
		c := a.a.ID + "." + digest + "." + strconv.Itoa(offset+n)
		page.NextCursor = &c
	} else {
		r := "end"
		if total > src.Cap {
			r = "cap"
		}
		page.EndReason = &r
	}
	tomorrow := f.now().UTC().Truncate(24*time.Hour).AddDate(0, 0, 1)
	page.Usage = SearchUsage{Used: a.searchUsed, Limit: a.a.Limits["search"], Remaining: a.a.Limits["search"] - a.searchUsed, ResetsAt: &tomorrow}
	u := fakeSearchURL(q)
	page.LinkedInURL = &u
	return page, nil
}

func (f *Fake) Typeahead(_ context.Context, externalID, source, filter, q, _ string) ([]TypeaheadItem, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.socialAccountLocked(externalID); err != nil {
		return nil, err
	}
	if fakeSourceOf(source) == nil {
		return nil, fakeErrDetails(http.StatusBadRequest, CodeInvalidRequest, "unknown source", map[string]any{"fields": []string{"source"}})
	}
	var list []string
	switch filter {
	case "geography":
		list = fakeGeographies
	case "industry":
		list = fakeIndustries
	case "current_title":
		list = fakeTitles
	case "current_company":
		list = fakeCompanies
	default:
		return nil, fakeErrDetails(http.StatusBadRequest, CodeInvalidRequest, "this filter has no suggestions", map[string]any{"fields": []string{"filter"}})
	}
	needle := strings.ToLower(strings.TrimSpace(q))
	out := []TypeaheadItem{}
	for _, v := range list {
		if needle == "" || strings.Contains(strings.ToLower(v), needle) {
			out = append(out, TypeaheadItem{ID: strconv.FormatUint(fakeHash(filter+v)%900_000_000+100_000_000, 10), Name: v})
		}
		if len(out) == 10 {
			break
		}
	}
	return out, nil
}

func (f *Fake) ParseSearchURL(_ context.Context, externalID, rawURL, _ string) (json.RawMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.socialAccountLocked(externalID); err != nil {
		return nil, err
	}
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || !strings.HasSuffix(strings.ToLower(u.Hostname()), "linkedin.com") {
		return nil, fakeErrDetails(http.StatusBadRequest, CodeInvalidRequest, "not a LinkedIn search URL", map[string]any{"fields": []string{"url"}})
	}
	q := Query{Keywords: u.Query().Get("keywords"), Filters: map[string][]QueryValue{}}
	switch {
	case strings.HasPrefix(u.Path, "/sales/search/people"):
		q.Source = SourceSalesNavigatorLeads
	case strings.HasPrefix(u.Path, "/sales/search/company"):
		q.Source = SourceSalesNavigatorAccounts
	case strings.HasPrefix(u.Path, "/search/results/people"):
		q.Source = SourceClassicPeople
	case strings.HasPrefix(u.Path, "/talent/"):
		q.Source = SourceRecruiterPeople
	default:
		return nil, fakeErrDetails(http.StatusBadRequest, CodeInvalidRequest, "not a LinkedIn search URL", map[string]any{"fields": []string{"url"}})
	}
	raw := u.String()
	q.URL = &raw
	out, _ := json.Marshal(map[string]any{"query": q, "skipped": []string{}})
	return out, nil
}

func (f *Fake) SearchLinkedInURL(_ context.Context, externalID string, query json.RawMessage, _ string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.socialAccountLocked(externalID); err != nil {
		return "", err
	}
	q, _, err := readQuery(query)
	if err != nil {
		return "", err
	}
	return fakeSearchURL(q), nil
}

// fakeAIRules are the phrases the sandbox AI builder understands, each mapped to a filter value.
var fakeAIRules = []struct{ phrase, filter, value string }{
	{"head of growth", "current_title", "Head of Growth"}, {"heads of growth", "current_title", "Head of Growth"},
	{"founder", "current_title", "Founder"}, {"cto", "current_title", "Chief Technology Officer"}, {"ceo", "current_title", "Chief Executive Officer"},
	{"vp of sales", "current_title", "VP of Sales"}, {"vp sales", "current_title", "VP of Sales"}, {"recruiter", "current_title", "Talent Acquisition Manager"},
	{"london", "geography", "London, United Kingdom"}, {"uk", "geography", "United Kingdom"}, {"new york", "geography", "New York, United States"},
	{"berlin", "geography", "Berlin, Germany"}, {"paris", "geography", "Paris, France"}, {"us", "geography", "United States"},
	{"fintech", "industry", "Financial Services"}, {"saas", "industry", "Software Development"}, {"agencies", "industry", "Marketing Services"},
	{"series a", "company_headcount", "11-50"}, {"startup", "company_headcount", "11-50"}, {"enterprise", "company_headcount", "10001+"},
}

func (f *Fake) BuildQuery(_ context.Context, externalID string, in AIInput, key string) (*AIResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return replay(f, key, "social:ai:"+externalID, in, func() (*AIResult, error) {
		if err := f.socialAccountLocked(externalID); err != nil {
			return nil, err
		}
		prompt := strings.ToLower(strings.TrimSpace(in.Prompt))
		if prompt == "" {
			return nil, fakeErrDetails(http.StatusBadRequest, CodeInvalidRequest, "prompt is required", map[string]any{"fields": []string{"prompt"}})
		}
		if fakeSourceOf(in.Source) == nil {
			return nil, fakeErrDetails(http.StatusBadRequest, CodeInvalidRequest, "unknown source", map[string]any{"fields": []string{"source"}})
		}
		day := externalID + f.now().UTC().Format("2006-01-02")
		if f.social.aiBuilds[day] >= fakeAIPerDay {
			e := fakeErr(http.StatusTooManyRequests, CodeRateLimited, "the AI builder's daily limit is reached")
			e.RetryAfter = time.Hour
			return nil, e
		}
		f.social.aiBuilds[day]++
		q := Query{Source: in.Source, Filters: map[string][]QueryValue{}}
		if len(in.CurrentQuery) > 0 && string(in.CurrentQuery) != "null" {
			_ = json.Unmarshal(in.CurrentQuery, &q)
			q.Source = in.Source
			if q.Filters == nil {
				q.Filters = map[string][]QueryValue{}
			}
		}
		padded := " " + strings.NewReplacer(",", " ", ".", " ").Replace(prompt) + " "
		var changes []AIChange
		var matched []string
		for _, r := range fakeAIRules {
			if !strings.Contains(padded, " "+r.phrase+" ") || slices.ContainsFunc(q.Filters[r.filter], func(v QueryValue) bool { return v.Text == r.value }) {
				continue
			}
			v := QueryValue{ID: strconv.FormatUint(fakeHash(r.filter+r.value)%900_000_000+100_000_000, 10), Text: r.value}
			q.Filters[r.filter] = append(q.Filters[r.filter], v)
			changes = append(changes, AIChange{Filter: r.filter, Action: "add", Values: []QueryValue{v}})
			matched = append(matched, r.value)
		}
		res := &AIResult{Summary: "People matching: " + in.Prompt, Changes: changes, Assumptions: []string{}, Suggestions: []string{"Add a seniority level to narrow it down"},
			CostMicros: 450, Unresolved: json.RawMessage(`[]`)}
		if len(changes) == 0 {
			q.Keywords = strings.TrimSpace(in.Prompt)
			res.Unresolved = json.RawMessage(`["No filter matched; the description is searched as keywords"]`)
		} else {
			res.Summary = strings.Join(matched, ", ")
			res.Assumptions = []string{"Current positions only"}
		}
		res.Query, _ = json.Marshal(q)
		if res.Changes == nil {
			res.Changes = []AIChange{}
		}
		return res, nil
	})
}

func (f *Fake) GetProfile(_ context.Context, externalID, identifier, accountID string) (*Profile, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.socialAccountLocked(externalID); err != nil {
		return nil, err
	}
	channel := ChannelLinkedIn
	if acct := f.social.accounts[accountID]; acct != nil && acct.external == externalID {
		channel = acct.a.Channel
	}
	public := fakeRecipient(channel, identifier)
	if public == "" {
		return nil, fakeErr(http.StatusUnprocessableEntity, CodeRecipientInvalid, "the recipient names nobody on "+channel)
	}
	if channel != ChannelLinkedIn {
		p := fakePersonOn(channel, public)
		return &Profile{Person: p, Experience: []Experience{}}, nil
	}
	var id *string
	if accountID != "" {
		id = &accountID
	}
	a, err := f.searchAccountLocked(externalID, id, nil, 1)
	if err != nil {
		return nil, err
	}
	a.searchUsed++
	p := fakePersonFor(public)
	title, company, _ := strings.Cut(p.Headline, " at ")
	return &Profile{Person: p, Summary: "Building " + company + ".", CurrentTitle: title, CurrentCompany: company,
		Industry: fakeIndustries[fakeHash(public)%uint64(len(fakeIndustries))], FollowerCount: int(fakeHash(public) % 5000),
		Relation:   ProfileRelation{NetworkDistance: p.NetworkDistance, InvitationPending: f.invitePendingLocked(a.a.ID, public)},
		Experience: []Experience{{Title: title, Company: company, StartedOn: "2023-04"}}}, nil
}

// fakePublicID reads a profile URL, a public id or a provider id.
func fakePublicID(identifier string) string {
	s := strings.TrimSpace(identifier)
	if s == "" {
		return ""
	}
	if u, err := url.Parse(s); err == nil && u.Host != "" {
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) < 2 || parts[0] != "in" || !strings.HasSuffix(strings.ToLower(u.Hostname()), "linkedin.com") {
			return ""
		}
		s = parts[1]
	}
	if strings.ContainsAny(s, " /?#") {
		return ""
	}
	return strings.ToLower(s)
}

// fakePersonFor is the sandbox member behind a public id, keeping the id it was asked for.
func fakePersonFor(public string) Person {
	p := fakePerson(public)
	p.PublicID, p.ProfileURL = public, "https://www.linkedin.com/in/"+public
	parts := strings.Split(public, "-")
	if len(parts) >= 2 && parts[0] != "" && parts[1] != "" {
		p.FirstName = strings.ToUpper(parts[0][:1]) + parts[0][1:]
		p.LastName = strings.ToUpper(parts[1][:1]) + parts[1][1:]
		p.FullName = p.FirstName + " " + p.LastName
	}
	return p
}

// ---- actions --------------------------------------------------------------

func (f *Fake) invitePendingLocked(accountID, public string) bool {
	for _, a := range f.social.actions {
		if a.a.AccountID == accountID && a.a.Type == ActionInvite && a.a.Recipient.PublicID == public &&
			(a.a.Status == ActionQueued || a.a.Status == ActionRunning || (a.a.Status == ActionSucceeded && a.acceptAt != nil)) {
			return true
		}
	}
	return false
}

func (f *Fake) CreateAction(_ context.Context, externalID string, in ActionInput, key string) (*Action, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return replay(f, key, "social:action:"+externalID, in, func() (*Action, error) {
		acct, err := f.liveAccountLocked(externalID, in.AccountID)
		if err != nil {
			return nil, err
		}
		if len(in.Reference) > 128 {
			return nil, fakeErrDetails(http.StatusBadRequest, CodeInvalidRequest, "reference is at most 128 characters", map[string]any{"fields": []string{"reference"}})
		}
		if err := f.checkActionLocked(acct, in); err != nil {
			return nil, err
		}
		public := fakeRecipient(acct.a.Channel, in.Recipient)
		p := fakePersonOn(acct.a.Channel, public)
		now := f.now().UTC()
		run := now.Add(fakeActionAfter)
		if in.NotBefore != nil && in.NotBefore.After(run) {
			run = *in.NotBefore
		}
		exp := run.AddDate(0, 0, 7)
		if in.ExpiresAt != nil {
			exp = *in.ExpiresAt
		}
		a := &fakeAction{external: externalID, runAt: run, a: Action{ID: f.id("sact"), AccountID: acct.a.ID, Channel: acct.a.Channel, Type: in.Type, Status: ActionQueued,
			Recipient:    ActionRecipient{Input: in.Recipient, ProviderID: p.ProviderID, PublicID: public, FullName: p.FullName, ProfileURL: p.ProfileURL},
			ScheduledFor: &run, NotBefore: in.NotBefore, ExpiresAt: &exp, CreatedAt: now, UpdatedAt: now}}
		if in.Reference != "" {
			a.a.Reference = &in.Reference
		}
		if in.Voice != nil {
			a.audioID = in.Voice.AudioID
		}
		if in.Consent != nil {
			a.consent = in.Consent.Basis
		}
		// The body travels with the action until it runs.
		switch {
		case in.Body != nil:
			f.social.messages["pending:"+a.a.ID] = []*fakeMessage{{m: SocialMessage{Body: *in.Body}}}
		case in.Voice != nil:
			f.social.messages["pending:"+a.a.ID] = []*fakeMessage{{m: SocialMessage{Body: "[voice note]"}}}
		}
		f.social.actions[a.a.ID] = a
		f.emitLocked(EventSocialActionUpdated, externalID, a.a)
		return cloneJSON(a.a), nil
	})
}

func (f *Fake) GetAction(_ context.Context, externalID, id string) (*Action, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.socialAccountLocked(externalID); err != nil {
		return nil, err
	}
	a := f.social.actions[id]
	if a == nil || a.external != externalID {
		return nil, fakeErr(http.StatusNotFound, CodeNotFound, "no such action")
	}
	return cloneJSON(a.a), nil
}

func (f *Fake) ListActions(_ context.Context, externalID string, fl ActionFilter) (*Page[Action], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.socialAccountLocked(externalID); err != nil {
		return nil, err
	}
	var all []Action
	for _, a := range f.social.actions {
		if a.external != externalID || (fl.AccountID != "" && a.a.AccountID != fl.AccountID) || (fl.Status != "" && a.a.Status != fl.Status) ||
			(fl.Reference != "" && (a.a.Reference == nil || *a.a.Reference != fl.Reference)) {
			continue
		}
		all = append(all, *cloneJSON(a.a))
	}
	sort.Slice(all, func(i, j int) bool { return all[i].ID > all[j].ID })
	return fakePage(all, fl.Cursor, fl.Limit)
}

func (f *Fake) CancelAction(_ context.Context, externalID, id, key string) (*Action, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return replay(f, key, "social:action-cancel:"+id, nil, func() (*Action, error) {
		if err := f.socialAccountLocked(externalID); err != nil {
			return nil, err
		}
		a := f.social.actions[id]
		if a == nil || a.external != externalID {
			return nil, fakeErr(http.StatusNotFound, CodeNotFound, "no such action")
		}
		if a.a.Status != ActionQueued {
			return nil, fakeErr(http.StatusConflict, CodeStateConflict, "only a queued action can be cancelled")
		}
		a.a.Status, a.a.UpdatedAt = ActionCancelled, f.now().UTC()
		f.emitLocked(EventSocialActionUpdated, externalID, a.a)
		return cloneJSON(a.a), nil
	})
}

// ---- conversations --------------------------------------------------------

func participantOf(p Person) Participant {
	return Participant{ProviderID: p.ProviderID, PublicID: p.PublicID, FullName: p.FullName, Headline: p.Headline, ProfileURL: p.ProfileURL, PictureURL: p.PictureURL}
}

// conversationLocked is the account's conversation with a person, created when missing.
func (f *Fake) conversationLocked(acct *fakeSocialAccount, p Person, kind string) *fakeConv {
	for _, c := range f.social.convs {
		if c.c.AccountID == acct.a.ID && c.c.Participant.PublicID == p.PublicID {
			return c
		}
	}
	c := &fakeConv{external: acct.external, c: Conversation{ID: f.id("scv"), AccountID: acct.a.ID, Channel: acct.a.Channel, Participant: participantOf(p), Kind: kind}}
	f.social.convs[c.c.ID] = c
	return c
}

func (f *Fake) addMessageLocked(c *fakeConv, dir, body, kind string, actionID *string, now time.Time, sendAt *time.Time) *fakeMessage {
	m := &fakeMessage{m: SocialMessage{ID: f.id("smsg"), ConversationID: c.c.ID, Channel: c.c.Channel, Direction: dir, Body: body, Status: "sent", Kind: kind,
		Attachments: []Attachment{}, ActionID: actionID}, sendAt: sendAt}
	if sendAt != nil {
		m.m.Status = "pending"
	} else {
		m.m.SentAt = &now
		c.c.LastMessageAt = &now
		if dir == DirectionInbound {
			c.c.UnreadCount++
		}
	}
	c.c.LastMessage = &m.m
	f.social.messages[c.c.ID] = append(f.social.messages[c.c.ID], m)
	return m
}

// seedConversationLocked gives a newly connected account the synced history a real one has.
func (f *Fake) seedConversationLocked(acct *fakeSocialAccount, now time.Time) {
	seed := "sam-rivera-" + strconv.FormatUint(fakeHash(acct.a.ID)%900+100, 10)
	switch acct.a.Channel {
	case ChannelWhatsApp, ChannelTelegram:
		seed = "+4477009" + strconv.FormatUint(fakeHash(acct.a.ID)%90000+10000, 10)
	case ChannelInstagram, ChannelFacebook:
		seed = "sam.rivera" + strconv.FormatUint(fakeHash(acct.a.ID)%900+100, 10)
	}
	p := fakePersonOn(acct.a.Channel, seed)
	c := f.conversationLocked(acct, p, ActionMessage)
	// Delivered like any other message, after the account, so the workspace stores the history too.
	out := f.addMessageLocked(c, DirectionOutbound, "Great meeting you at the conference last week. Would love to compare notes on outbound sometime.", ActionMessage, nil, now.Add(-49*time.Hour), nil)
	f.emitLocked(EventSocialMessageSent, c.external, MessageEvent{Channel: c.c.Channel, Conversation: c.c, Message: out.m})
	in := f.addMessageLocked(c, DirectionInbound, "Likewise! I'm free Thursday afternoon if that works for a quick call.", ActionMessage, nil, now.Add(-47*time.Hour), nil)
	f.emitLocked(EventSocialMessageReceived, c.external, MessageEvent{Channel: c.c.Channel, Conversation: c.c, Message: in.m})
}

func (f *Fake) convLocked(externalID, id string) (*fakeConv, error) {
	if err := f.socialAccountLocked(externalID); err != nil {
		return nil, err
	}
	c := f.social.convs[id]
	if c == nil || c.external != externalID {
		return nil, fakeErr(http.StatusNotFound, CodeNotFound, "no such conversation")
	}
	if a := f.social.accounts[c.c.AccountID]; a == nil {
		return nil, fakeErr(http.StatusNotFound, CodeNotFound, "no such conversation")
	}
	return c, nil
}

func (f *Fake) ListConversations(_ context.Context, externalID string, fl ConversationFilter) (*Page[Conversation], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.socialAccountLocked(externalID); err != nil {
		return nil, err
	}
	var all []Conversation
	for _, c := range f.social.convs {
		if c.external != externalID || f.social.accounts[c.c.AccountID] == nil || (fl.AccountID != "" && c.c.AccountID != fl.AccountID) || (fl.Channel != "" && c.c.Channel != fl.Channel) {
			continue
		}
		if fl.UpdatedAfter != nil && (c.c.LastMessageAt == nil || !c.c.LastMessageAt.After(*fl.UpdatedAfter)) {
			continue
		}
		all = append(all, *cloneJSON(c.c))
	}
	sort.Slice(all, func(i, j int) bool {
		ti, tj := all[i].LastMessageAt, all[j].LastMessageAt
		if ti == nil || tj == nil {
			return tj == nil && ti != nil
		}
		return ti.After(*tj)
	})
	return fakePage(all, fl.Cursor, fl.Limit)
}

func (f *Fake) GetConversation(_ context.Context, externalID, id string) (*Conversation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, err := f.convLocked(externalID, id)
	if err != nil {
		return nil, err
	}
	return cloneJSON(c.c), nil
}

func (f *Fake) ListMessages(_ context.Context, externalID, conversationID, cursor string, limit int) (*Page[SocialMessage], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, err := f.convLocked(externalID, conversationID)
	if err != nil {
		return nil, err
	}
	list := f.social.messages[c.c.ID]
	all := make([]SocialMessage, 0, len(list))
	for i := len(list) - 1; i >= 0; i-- {
		all = append(all, *cloneJSON(list[i].m))
	}
	return fakePage(all, cursor, limit)
}

func (f *Fake) SendMessage(_ context.Context, externalID, conversationID string, in MessageInput, key string) (*SocialMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return replay(f, key, "social:reply:"+conversationID, in, func() (*SocialMessage, error) {
		c, err := f.convLocked(externalID, conversationID)
		if err != nil {
			return nil, err
		}
		body := ""
		switch {
		case in.Body != nil && in.Voice != nil:
			return nil, fakeErrDetails(http.StatusBadRequest, CodeInvalidRequest, "send a body or a voice note, not both", map[string]any{"fields": []string{"voice"}})
		case in.Voice != nil:
			// Instagram sends voice notes only as actions; Facebook has none.
			if !slices.Contains(fakeCapabilityActions(c.c.Channel), ActionVoiceNote) || c.c.Channel == ChannelInstagram {
				return nil, fakeErrDetails(http.StatusBadRequest, CodeInvalidRequest, c.c.Channel+" takes no voice replies", map[string]any{"fields": []string{"voice"}})
			}
			ch, _ := fakeChannel(c.c.Channel)
			if err := f.checkVoiceLocked(f.social.accounts[c.c.AccountID], ch, in.Voice); err != nil {
				return nil, err
			}
			body = "[voice note]"
		case in.Body != nil:
			body = *in.Body
		}
		if strings.TrimSpace(body) == "" {
			return nil, fakeErrDetails(http.StatusBadRequest, CodeInvalidRequest, "body is required", map[string]any{"fields": []string{"body"}})
		}
		if a := f.social.accounts[c.c.AccountID]; a.a.Status == SocialRemoved {
			return nil, fakeErr(http.StatusConflict, CodeAccountUnavailable, "this account was removed")
		}
		now := f.now().UTC()
		at := now.Add(fakeMessageSendIn)
		m := f.addMessageLocked(c, DirectionOutbound, body, c.c.Kind, nil, now, &at)
		return cloneJSON(m.m), nil
	})
}

func (f *Fake) MarkConversationRead(_ context.Context, externalID, conversationID, key string) (*Conversation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return replay(f, key, "social:read:"+conversationID, nil, func() (*Conversation, error) {
		c, err := f.convLocked(externalID, conversationID)
		if err != nil {
			return nil, err
		}
		c.c.UnreadCount = 0
		return cloneJSON(c.c), nil
	})
}

// ---- time ---------------------------------------------------------------

func (f *Fake) advanceOutreachLocked(now time.Time) []Event {
	var out []Event
	ids := make([]string, 0, len(f.social.actions))
	for id := range f.social.actions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		out = append(out, f.advanceActionLocked(f.social.actions[id], now)...)
	}
	for _, c := range f.social.convs {
		acct := f.social.accounts[c.c.AccountID]
		if acct == nil {
			continue
		}
		for _, m := range f.social.messages[c.c.ID] {
			if m.sendAt == nil || now.Before(*m.sendAt) {
				continue
			}
			m.sendAt, m.m.Status, m.m.SentAt = nil, "sent", &now
			c.c.LastMessageAt, c.c.LastMessage = &now, &m.m
			out = append(out, f.emitLocked(EventSocialMessageSent, c.external, MessageEvent{Channel: c.c.Channel, Conversation: c.c, Message: m.m}))
			if fakeReplies(c.c.Participant.PublicID) && c.replyAt == nil {
				at := now.Add(fakeReplyAfter)
				c.replyAt = &at
			}
		}
		if c.replyAt != nil && !now.Before(*c.replyAt) {
			c.replyAt = nil
			m := f.addMessageLocked(c, DirectionInbound, "Thanks for reaching out! Happy to chat, what does your week look like?", c.c.Kind, nil, now, nil)
			out = append(out, f.emitLocked(EventSocialMessageReceived, c.external, MessageEvent{Channel: c.c.Channel, Conversation: c.c, Message: m.m}))
		}
	}
	return out
}

func (f *Fake) advanceActionLocked(a *fakeAction, now time.Time) []Event {
	var out []Event
	acct := f.social.accounts[a.a.AccountID]
	if acct == nil {
		return nil
	}
	if a.acceptAt != nil && !now.Before(*a.acceptAt) {
		a.acceptAt = nil
		key := acct.a.ID + "|" + a.a.Recipient.PublicID
		f.social.connected[key] = true
		p := fakePersonFor(a.a.Recipient.PublicID)
		p.NetworkDistance = "1st"
		id := a.a.ID
		out = append(out, f.emitLocked(EventSocialInvitationAccepted, a.external, InvitationAccepted{AccountID: acct.a.ID, Channel: acct.a.Channel,
			Person: p, ConnectedAt: now, ActionID: &id}))
		if fakeReplies(a.a.Recipient.PublicID) {
			c := f.conversationLocked(acct, p, ActionMessage)
			at := now.Add(fakeReplyAfter)
			c.replyAt = &at
		}
	}
	if a.a.Status != ActionQueued {
		return out
	}
	if a.a.ExpiresAt != nil && !now.Before(*a.a.ExpiresAt) {
		a.a.Status, a.a.UpdatedAt = ActionExpired, now
		return append(out, f.emitLocked(EventSocialActionUpdated, a.external, a.a))
	}
	if now.Before(a.runAt) || acct.a.Status != SocialActive {
		return out
	}
	a.a.AttemptedAt, a.a.CompletedAt, a.a.UpdatedAt, a.a.Status = &now, &now, now, ActionSucceeded
	public := a.a.Recipient.PublicID
	// Only LinkedIn has connections; everywhere else a message reaches anyone.
	connected := acct.a.Channel != ChannelLinkedIn || f.social.connected[acct.a.ID+"|"+public]
	skip := func(reason string) { a.a.Status, a.a.SkipReason, a.a.CompletedAt = ActionSkipped, &reason, nil }
	var body string
	if pending := f.social.messages["pending:"+a.a.ID]; len(pending) > 0 {
		body = pending[0].m.Body
		delete(f.social.messages, "pending:"+a.a.ID)
	}
	if strings.Contains(public, "gone") {
		skip("recipient_unavailable")
		return append(out, f.emitLocked(EventSocialActionUpdated, a.external, a.a))
	}
	// The two consent bases GraphKit can see are checked when the action runs.
	if (a.consent == "existing_conversation" && !f.hasConversationLocked(acct.a.ID, public)) || (a.consent == "friend" && !strings.Contains(public, "friend")) {
		skip("consent_missing")
		return append(out, f.emitLocked(EventSocialActionUpdated, a.external, a.a))
	}
	switch a.a.Type {
	case ActionInvite:
		if f.social.connected[acct.a.ID+"|"+public] {
			skip("already_connected")
			break
		}
		inv := f.id("sinv")
		a.a.Result.InvitationID = &inv
		if strings.Contains(public, "accept") || fakeReplies(public) {
			at := now.Add(fakeAcceptAfter)
			a.acceptAt = &at
		}
		if acct.a.PendingInvites != nil {
			*acct.a.PendingInvites++
		}
	case ActionMessage, ActionInmail, ActionVoiceNote:
		if a.a.Type != ActionInmail && !connected {
			skip("not_connected")
			break
		}
		p := fakePersonOn(acct.a.Channel, public)
		c := f.conversationLocked(acct, p, a.a.Type)
		id := a.a.ID
		m := f.addMessageLocked(c, DirectionOutbound, body, a.a.Type, &id, now, nil)
		a.a.Result.ConversationID, a.a.Result.MessageID = &c.c.ID, &m.m.ID
		out = append(out, f.emitLocked(EventSocialMessageSent, a.external, MessageEvent{Channel: c.c.Channel, Conversation: c.c, Message: m.m}))
		if fakeReplies(public) {
			at := now.Add(fakeReplyAfter)
			c.replyAt = &at
		}
	case ActionLikePost, ActionCommentPost:
		if strings.Contains(public, "nopost") {
			skip("no_recent_post")
			break
		}
		if a.a.Type == ActionCommentPost && f.commentedLocked(a) {
			skip("already_commented")
			break
		}
		post := f.id("post")
		a.a.Result.PostID = &post
		if a.a.Type == ActionCommentPost {
			cid := f.id("cmt")
			a.a.Result.CommentID = &cid
		}
	case ActionEndorse:
		switch {
		case !f.social.connected[acct.a.ID+"|"+public]:
			skip("not_connected")
		case strings.Contains(public, "noskills"):
			skip("no_skills")
		}
	case ActionWithdrawInvite:
		if !f.invitePendingLocked(acct.a.ID, public) {
			skip("no_pending_invite")
		}
	}
	return append(out, f.emitLocked(EventSocialActionUpdated, a.external, a.a))
}

// hasConversationLocked reports a conversation between the account and the person.
func (f *Fake) hasConversationLocked(accountID, public string) bool {
	for _, c := range f.social.convs {
		if c.c.AccountID == accountID && c.c.Participant.PublicID == public && len(f.social.messages[c.c.ID]) > 0 {
			return true
		}
	}
	return false
}

// commentedLocked reports an earlier comment from the account on the person's latest post.
func (f *Fake) commentedLocked(a *fakeAction) bool {
	for _, o := range f.social.actions {
		if o != a && o.a.AccountID == a.a.AccountID && o.a.Type == ActionCommentPost && o.a.Status == ActionSucceeded && o.a.Recipient.PublicID == a.a.Recipient.PublicID {
			return true
		}
	}
	return false
}
