package replyio

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	source "github.com/warmbly/warmbly/internal/pkg/outreachsource"
)

type contact struct {
	ID           int    `json:"id"`
	Email        string `json:"email"`
	FirstName    string `json:"firstName"`
	LastName     string `json:"lastName"`
	Company      string `json:"company"`
	Title        string `json:"title"`
	Phone        string `json:"phone"`
	LinkedInURL  string `json:"linkedInUrl"`
	City         string `json:"city"`
	State        string `json:"state"`
	Country      string `json:"country"`
	TimeZoneID   string `json:"timeZoneId"`
	Industry     string `json:"industry"`
	CompanySize  string `json:"companySize"`
	Stage        string `json:"stage"`
	IsOptedOut   bool   `json:"isOptedOut"`
	MeetingState string `json:"meetingStatus"`
	CustomFields []struct {
		Key   string  `json:"key"`
		Value *string `json:"value"`
	} `json:"customFields"`
}

type seqContact struct {
	ContactID        int    `json:"contactId"`
	Email            string `json:"email"`
	FirstName        string `json:"firstName"`
	LastName         string `json:"lastName"`
	Company          string `json:"company"`
	Title            string `json:"title"`
	StatusInSequence string `json:"statusInSequence"`
	IsOptedOut       bool   `json:"isOptedOut"`
	MeetingStatus    string `json:"meetingStatus"`
	CurrentStep      struct {
		StepID      *int   `json:"stepId"`
		DisplayName string `json:"displayName"`
	} `json:"currentStep"`
	EmailDisposition *struct {
		IsReplied bool `json:"isReplied"`
		IsBounced bool `json:"isBounced"`
	} `json:"emailDisposition"`
}

func (s *session) Leads(ctx context.Context, _ source.Workspace, campaignID, cursor string) ([]source.Lead, string, error) {
	skip := 0
	if cursor != "" {
		n, err := strconv.Atoi(cursor)
		if err != nil || n < 0 {
			return nil, "", fmt.Errorf("%s: invalid cursor", label)
		}
		skip = n
	}
	if campaignID == "" {
		return s.looseLeads(ctx, skip)
	}
	id, err := strconv.Atoi(campaignID)
	if err != nil {
		return nil, "", fmt.Errorf("%s: invalid campaign id", label)
	}
	sq, err := s.sequence(ctx, id)
	if err != nil {
		return nil, "", err
	}
	if err := s.loadContacts(ctx); err != nil {
		return nil, "", err
	}
	q := url.Values{"top": {strconv.Itoa(pageSize)}, "skip": {strconv.Itoa(skip)}}
	var p page[seqContact]
	if err := s.c.Get(ctx, "/v3/sequences/"+campaignID+"/contacts", q, &p); err != nil {
		return nil, "", err
	}
	before, total := emailsBefore(sq)
	out := make([]source.Lead, 0, len(p.Items))
	for _, sc := range p.Items {
		s.mu.Lock()
		c, ok := s.contacts[sc.ContactID]
		s.mu.Unlock()
		if !ok {
			c = contact{ID: sc.ContactID, Email: sc.Email, FirstName: sc.FirstName, LastName: sc.LastName, Company: sc.Company, Title: sc.Title}
		}
		l := contactLead(c)
		if l.Email == "" {
			continue
		}
		l.Status = sequenceStatus(sc, c.Stage)
		l.StepsSent = stepsSent(sc, before, total)
		out = append(out, l)
	}
	next := ""
	if p.HasMore && len(p.Items) > 0 {
		next = strconv.Itoa(skip + len(p.Items))
	}
	return out, next, nil
}

// loadContacts reads every contact once: the sequence contact list carries no
// custom fields, and Reply has no lookup of many contacts by id.
func (s *session) loadContacts(ctx context.Context) error {
	s.mu.Lock()
	loaded := s.contacts != nil
	s.mu.Unlock()
	if loaded {
		return nil
	}
	list, err := all[contact](ctx, s.c, "/v3/contacts", nil)
	if err != nil {
		return err
	}
	byID := make(map[int]contact, len(list))
	order := make([]int, 0, len(list))
	for _, c := range list {
		if _, dup := byID[c.ID]; !dup {
			order = append(order, c.ID)
		}
		byID[c.ID] = c
	}
	s.mu.Lock()
	s.contacts, s.order = byID, order
	s.mu.Unlock()
	return nil
}

// looseLeads pages the contacts enrolled in no sequence.
func (s *session) looseLeads(ctx context.Context, skip int) ([]source.Lead, string, error) {
	if err := s.loadContacts(ctx); err != nil {
		return nil, "", err
	}
	if err := s.loadEnrolled(ctx); err != nil {
		return nil, "", err
	}
	s.mu.Lock()
	var loose []contact
	for _, id := range s.order {
		if !s.enrolled[id] {
			loose = append(loose, s.contacts[id])
		}
	}
	s.mu.Unlock()
	if skip >= len(loose) {
		return nil, "", nil
	}
	end := min(skip+pageSize, len(loose))
	var out []source.Lead
	for _, c := range loose[skip:end] {
		l := contactLead(c)
		if l.Email == "" {
			continue
		}
		l.Status = source.LeadNotStarted
		switch {
		case c.IsOptedOut:
			l.Status = source.LeadUnsubscribed
		case c.MeetingState == "meetingBooked":
			l.Status = source.LeadMeeting
		case stageStatus(c.Stage) != "":
			l.Status = stageStatus(c.Stage)
		}
		out = append(out, l)
	}
	next := ""
	if end < len(loose) {
		next = strconv.Itoa(end)
	}
	return out, next, nil
}

// loadEnrolled collects every contact that sits in a sequence.
func (s *session) loadEnrolled(ctx context.Context) error {
	s.mu.Lock()
	loaded := s.enrolled != nil
	s.mu.Unlock()
	if loaded {
		return nil
	}
	seqs, err := s.listSequences(ctx)
	if err != nil {
		return err
	}
	enrolled := map[int]bool{}
	for _, sq := range seqs {
		items, err := all[seqContact](ctx, s.c, "/v3/sequences/"+strconv.Itoa(sq.ID)+"/contacts", nil)
		if err != nil {
			return err
		}
		for _, c := range items {
			enrolled[c.ContactID] = true
		}
	}
	s.mu.Lock()
	s.enrolled = enrolled
	s.mu.Unlock()
	return nil
}

func contactLead(c contact) source.Lead {
	l := source.Lead{
		ID:        strconv.Itoa(c.ID),
		Email:     strings.ToLower(strings.TrimSpace(c.Email)),
		FirstName: c.FirstName,
		LastName:  c.LastName,
		Company:   c.Company,
		Title:     c.Title,
		Phone:     c.Phone,
		LinkedIn:  c.LinkedInURL,
		Location:  joinNonEmpty(c.City, c.State, c.Country),
		Timezone:  c.TimeZoneID,
	}
	if z := ianaZone(c.TimeZoneID); z != "" {
		l.Timezone = z
	}
	custom := map[string]string{}
	if c.Industry != "" {
		custom["industry"] = c.Industry
	}
	if c.CompanySize != "" && c.CompanySize != "Empty" {
		custom["company_size"] = c.CompanySize
	}
	for _, f := range c.CustomFields {
		if f.Value != nil && strings.TrimSpace(*f.Value) != "" {
			custom[source.FieldKey(f.Key)] = strings.TrimSpace(*f.Value)
		}
	}
	if len(custom) > 0 {
		l.Custom = custom
	}
	return l
}

func joinNonEmpty(parts ...string) string {
	var out []string
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, ", ")
}

// sequenceStatus reads the opt-out, bounce, meeting and reply flags before the
// in-sequence status; stage carries interest when the team uses the default names.
func sequenceStatus(sc seqContact, stage string) source.LeadStatus {
	d := sc.EmailDisposition
	switch {
	case sc.IsOptedOut:
		return source.LeadUnsubscribed
	case d != nil && d.IsBounced:
		return source.LeadBounced
	case sc.MeetingStatus == "meetingBooked":
		return source.LeadMeeting
	case stageStatus(stage) != "":
		return stageStatus(stage)
	case d != nil && d.IsReplied:
		return source.LeadReplied
	}
	switch sc.StatusInSequence {
	case "finished":
		return source.LeadCompleted
	case "paused", "outOfOffice", "inactive":
		return source.LeadPaused
	}
	if d == nil {
		return source.LeadNotStarted
	}
	return source.LeadInProgress
}

func stageStatus(stage string) source.LeadStatus {
	switch strings.ToLower(strings.TrimSpace(stage)) {
	case "interested":
		return source.LeadInterested
	case "not interested":
		return source.LeadNotInterest
	case "do not contact":
		return source.LeadUnsubscribed
	}
	return ""
}

// emailsBefore counts, for each main-path step, the email steps ahead of it.
func emailsBefore(sq *sequence) (map[int]int, int) {
	before, n := map[int]int{}, 0
	for _, st := range topLevel(sq.Steps) {
		if st.ID != nil {
			before[*st.ID] = n
		}
		if st.Type == "email" {
			n++
		}
	}
	return before, n
}

// stepsSent estimates the email steps sent before the one a contact waits on.
func stepsSent(sc seqContact, before map[int]int, total int) int {
	switch {
	case sc.EmailDisposition == nil:
		return 0
	case sc.CurrentStep.StepID == nil:
		return total
	}
	return before[*sc.CurrentStep.StepID]
}
