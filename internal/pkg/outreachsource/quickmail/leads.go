package quickmail

import (
	"context"
	"sort"
	"strconv"
	"strings"

	"github.com/warmbly/warmbly/internal/pkg/outreachsource"
)

type lead struct {
	ID           string  `json:"id"`
	Email        string  `json:"email"`
	FirstName    *string `json:"firstName"`
	LastName     *string `json:"lastName"`
	Title        *string `json:"title"`
	Role         *string `json:"role"`
	Phone        *string `json:"phone"`
	LinkedinID   *string `json:"linkedinId"`
	Score        *int    `json:"score"`
	Language     *string `json:"language"`
	DoNotContact bool    `json:"doNotContact"`
	Tags         struct {
		Nodes []struct {
			Name string `json:"name"`
		} `json:"nodes"`
	} `json:"tags"`
	CustomProperties struct {
		Nodes []struct {
			Name  string  `json:"name"`
			Value *string `json:"value"`
		} `json:"nodes"`
	} `json:"customProperties"`
}

const leadFields = `id email firstName lastName title role phone linkedinId score language doNotContact
        tags(first: 50) { nodes { name } }
        customProperties(first: 100) { nodes { name value } }`

// leadsPage reads one page of a workspace's leads. include limits it to one
// campaign; none limits it to leads in no campaign.
func (s *session) leadsPage(ctx context.Context, ws outreachsource.Workspace, include string, none bool, cursor string) ([]lead, string, error) {
	args := `first: $first, after: $after, includeCampaignPrefixIds: $include, maxCampaigns: $max`
	conn := `leads(` + args + `) { nodes { ` + leadFields + ` } pageInfo { hasNextPage endCursor } }`
	q := `query Leads($id: ID!, $first: Int!, $after: String, $include: [ID!], $max: Int) { workspace(id: $id) { ` + conn + ` } }`
	vars := map[string]any{"first": pageSize, "after": cursorVar(cursor), "include": nil, "max": nil}
	if ws.ID == "" {
		q = `query Leads($first: Int!, $after: String, $include: [ID!], $max: Int) { ` + conn + ` }`
	} else {
		vars["id"] = ws.ID
	}
	if include != "" {
		vars["include"] = []string{include}
	}
	if none {
		vars["max"] = 0
	}
	var data struct {
		Workspace *struct {
			Leads leadConn `json:"leads"`
		} `json:"workspace"`
		Leads leadConn `json:"leads"`
	}
	if err := s.query(ctx, q, vars, &data); err != nil {
		return nil, "", err
	}
	page := data.Leads
	if data.Workspace != nil {
		page = data.Workspace.Leads
	}
	return page.Nodes, page.PageInfo.next(), nil
}

type leadConn struct {
	Nodes    []lead   `json:"nodes"`
	PageInfo pageInfo `json:"pageInfo"`
}

// Leads pages a campaign's leads, or the workspace's leads in no campaign.
// QuickMail does not say where a lead is in a campaign, so a campaign lead is
// reported in progress with an inexact step count.
func (s *session) Leads(ctx context.Context, ws outreachsource.Workspace, campaignID, cursor string) ([]outreachsource.Lead, string, error) {
	nodes, next, err := s.leadsPage(ctx, ws, campaignID, campaignID == "", cursor)
	if err != nil {
		return nil, "", err
	}
	out := make([]outreachsource.Lead, 0, len(nodes))
	for _, n := range nodes {
		email := strings.ToLower(strings.TrimSpace(n.Email))
		if email == "" {
			continue
		}
		l := outreachsource.Lead{
			ID:        n.ID,
			Email:     email,
			FirstName: str(n.FirstName),
			LastName:  str(n.LastName),
			Title:     str(n.Title),
			Phone:     str(n.Phone),
			LinkedIn:  str(n.LinkedinID),
			Custom:    map[string]string{},
			Status:    outreachsource.LeadNotStarted,
		}
		if campaignID != "" {
			l.Status = outreachsource.LeadInProgress
		}
		if n.DoNotContact {
			l.Status = outreachsource.LeadUnsubscribed
		}
		if v := str(n.Role); v != "" {
			l.Custom["role"] = v
		}
		if v := str(n.Language); v != "" {
			l.Custom["language"] = v
		}
		if n.Score != nil {
			l.Custom["score"] = strconv.Itoa(*n.Score)
		}
		var tags []string
		for _, t := range n.Tags.Nodes {
			if name := strings.TrimSpace(t.Name); name != "" {
				tags = append(tags, name)
			}
		}
		if len(tags) > 0 {
			sort.Strings(tags)
			l.Custom["tags"] = strings.Join(tags, ", ")
		}
		for _, p := range n.CustomProperties.Nodes {
			if v := str(p.Value); v != "" {
				if key := outreachsource.FieldKey(p.Name); key != "" {
					l.Custom[key] = v
				}
			}
		}
		if len(l.Custom) == 0 {
			l.Custom = nil
		}
		out = append(out, l)
	}
	return out, next, nil
}

// Blocklist scans the workspace's leads for Do Not Contact ones; the API has
// no filter for them and does not expose the Do Not Contact lists themselves.
func (s *session) Blocklist(ctx context.Context, ws outreachsource.Workspace, cursor string) ([]outreachsource.BlockEntry, string, error) {
	nodes, next, err := s.leadsPage(ctx, ws, "", false, cursor)
	if err != nil {
		return nil, "", err
	}
	var out []outreachsource.BlockEntry
	for _, n := range nodes {
		if email := strings.ToLower(strings.TrimSpace(n.Email)); n.DoNotContact && email != "" {
			out = append(out, outreachsource.BlockEntry{Value: email, Reason: "Do Not Contact in QuickMail"})
		}
	}
	return out, next, nil
}

func str(p *string) string {
	if p == nil {
		return ""
	}
	return strings.TrimSpace(*p)
}
