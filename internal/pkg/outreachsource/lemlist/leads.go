package lemlist

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	source "github.com/warmbly/warmbly/internal/pkg/outreachsource"
)

// exportSkip are export columns that are neither a lead field nor custom data.
var exportSkip = map[string]bool{
	"_id": true, "emailStatus": true, "picture": true, "lastState": true, "status": true,
	"salesforceId": true, "pipedriveId": true, "hubspotId": true, "attioId": true,
	"email": true, "firstName": true, "lastName": true, "phone": true, "linkedinUrl": true,
	"timezone": true, "companyName": true, "companyDomain": true, "jobTitle": true,
}

func (s *session) Leads(ctx context.Context, _ source.Workspace, campaignID, cursor string) ([]source.Lead, string, error) {
	offset, err := parseOffset(cursor)
	if err != nil {
		return nil, "", err
	}
	if campaignID == "" {
		return s.looseContacts(ctx, offset)
	}
	all, err := s.export(ctx, campaignID)
	if err != nil {
		return nil, "", err
	}
	if offset >= len(all) {
		return nil, "", nil
	}
	end := min(offset+pageSize, len(all))
	next := ""
	if end < len(all) {
		next = strconv.Itoa(end)
	}
	return all[offset:end], next, nil
}

// export reads a campaign's leads once, since lemlist's full lead export is a
// single unpaged response, and serves later pages from memory.
func (s *session) export(ctx context.Context, campaignID string) ([]source.Lead, error) {
	s.mu.Lock()
	cached, ok := s.exports[campaignID]
	s.mu.Unlock()
	if ok {
		return cached, nil
	}
	var rows []map[string]any
	q := url.Values{"state": {"all"}, "format": {"json"}}
	if err := s.c.Get(ctx, "/v2/campaigns/"+url.PathEscape(campaignID)+"/export/leads", q, &rows); err != nil {
		return nil, err
	}
	leads := make([]source.Lead, 0, len(rows))
	for _, r := range rows {
		if l, ok := exportLead(r); ok {
			leads = append(leads, l)
		}
	}
	s.mu.Lock()
	s.exports[campaignID] = leads
	s.mu.Unlock()
	return leads, nil
}

func exportLead(r map[string]any) (source.Lead, bool) {
	l := source.Lead{
		ID:        str(r["_id"]),
		Email:     strings.ToLower(strings.TrimSpace(str(r["email"]))),
		FirstName: str(r["firstName"]),
		LastName:  str(r["lastName"]),
		Company:   str(r["companyName"]),
		Title:     str(r["jobTitle"]),
		Phone:     str(r["phone"]),
		Website:   str(r["companyDomain"]),
		LinkedIn:  str(r["linkedinUrl"]),
		Timezone:  str(r["timezone"]),
		Status:    leadStatus(str(r["status"]), str(r["lastState"])),
	}
	if l.Email == "" {
		return l, false
	}
	for k, v := range r {
		if exportSkip[k] {
			continue
		}
		if val := str(v); val != "" {
			if l.Custom == nil {
				l.Custom = map[string]string{}
			}
			l.Custom[source.FieldKey(k)] = val
		}
	}
	return l, true
}

func str(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(t)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	}
	return strings.TrimSpace(fmt.Sprint(v))
}

// leadStatus reads the lead's status ("review", "done", "interested", ...)
// together with its last state ("emailsReplied", "emailsBounced", ...).
func leadStatus(status, last string) source.LeadStatus {
	switch status {
	case "unsubscribed":
		return source.LeadUnsubscribed
	case "interested":
		return source.LeadInterested
	case "notInterested":
		return source.LeadNotInterest
	case "paused":
		return source.LeadPaused
	}
	switch {
	case last == "emailsUnsubscribed":
		return source.LeadUnsubscribed
	case last == "emailsBounced":
		return source.LeadBounced
	case last == "meetingBooked":
		return source.LeadMeeting
	case strings.HasSuffix(last, "NotInterested") || last == "notInterested":
		return source.LeadNotInterest
	case strings.HasSuffix(last, "Interested") || last == "interested":
		return source.LeadInterested
	case strings.HasSuffix(last, "Replied") || last == "warmed":
		return source.LeadReplied
	case last == "paused":
		return source.LeadPaused
	}
	if status == "done" {
		return source.LeadCompleted
	}
	switch last {
	case "", "imported", "scanned", "skipped", "reviewed":
		return source.LeadNotStarted
	}
	return source.LeadInProgress
}

type contactList struct {
	Data []struct {
		ID           string `json:"_id"`
		Email        string `json:"email"`
		FirstName    string `json:"firstName"`
		LastName     string `json:"lastName"`
		Phone        string `json:"phone"`
		JobTitle     string `json:"jobTitle"`
		LinkedinURL  string `json:"linkedinUrl"`
		Unsubscribed bool   `json:"unsubscribed"`
	} `json:"data"`
	Total int `json:"total"`
}

// looseContacts pages the team's contacts that are in no campaign.
func (s *session) looseContacts(ctx context.Context, offset int) ([]source.Lead, string, error) {
	q := url.Values{"notInAnyCampaign": {"true"}, "limit": {strconv.Itoa(pageSize)}, "offset": {strconv.Itoa(offset)}}
	var out contactList
	if err := s.c.Get(ctx, "/contacts", q, &out); err != nil {
		return nil, "", err
	}
	var leads []source.Lead
	for _, c := range out.Data {
		email := strings.ToLower(strings.TrimSpace(c.Email))
		if email == "" {
			continue
		}
		st := source.LeadNotStarted
		if c.Unsubscribed {
			st = source.LeadUnsubscribed
		}
		leads = append(leads, source.Lead{
			ID: c.ID, Email: email, FirstName: c.FirstName, LastName: c.LastName,
			Phone: c.Phone, Title: c.JobTitle, LinkedIn: c.LinkedinURL, Status: st,
		})
	}
	next := ""
	if len(out.Data) == pageSize && (out.Total == 0 || offset+pageSize < out.Total) {
		next = strconv.Itoa(offset + pageSize)
	}
	return leads, next, nil
}
