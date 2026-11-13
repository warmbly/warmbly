package quickmail

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/warmbly/warmbly/internal/pkg/outreachsource"
)

type leadCounts struct {
	Total     int `json:"total"`
	Active    int `json:"active"`
	Available int `json:"available"`
	Completed int `json:"completed"`
	Failed    int `json:"failed"`
}

type variation struct {
	ID       string `json:"id"`
	Position int    `json:"position"`
	Type     string `json:"type"`
	Info     struct {
		Typename string   `json:"__typename"`
		Subject  string   `json:"subject"`
		Body     string   `json:"body"`
		Unit     string   `json:"unit"`
		Value    *float64 `json:"value"`
		Business bool     `json:"business"`
	} `json:"info"`
}

type step struct {
	ID         string `json:"id"`
	Position   int    `json:"position"`
	Variations struct {
		Nodes    []variation `json:"nodes"`
		PageInfo pageInfo    `json:"pageInfo"`
	} `json:"variations"`
}

func (s step) kind() string {
	if len(s.Variations.Nodes) == 0 {
		return ""
	}
	return s.Variations.Nodes[0].Info.Typename
}

type campaign struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Paused     bool       `json:"paused"`
	CreatedAt  string     `json:"createdAt"`
	LeadStatus leadCounts `json:"leadStatus"`
	Steps      struct {
		Nodes []step `json:"nodes"`
	} `json:"steps"`
}

func (c campaign) status() outreachsource.CampaignStatus {
	l := c.LeadStatus
	switch {
	case c.Paused:
		return outreachsource.CampaignPaused
	case l.Total > 0 && l.Active == 0 && l.Available == 0:
		return outreachsource.CampaignCompleted
	}
	return outreachsource.CampaignActive
}

func (c campaign) emailSteps() int {
	n := 0
	for _, st := range c.Steps.Nodes {
		if st.kind() == "EmailVariation" {
			n++
		}
	}
	return n
}

const campaignFields = `id name paused createdAt leadStatus { total active available completed failed }`

// stepKinds asks for each step's type, enough to count email steps.
const stepKinds = ` steps(first: 100) { nodes { id position variations(first: 1) { nodes { info { __typename } } } } }`

// campaigns pages through a workspace's campaigns.
func (s *session) campaigns(ctx context.Context, ws outreachsource.Workspace, kinds bool) ([]campaign, error) {
	fields := campaignFields
	if kinds {
		fields += stepKinds
	}
	conn := `campaigns(first: $first, after: $after) { nodes { ` + fields + ` } pageInfo { hasNextPage endCursor } }`
	q := `query Campaigns($id: ID!, $first: Int!, $after: String) { workspace(id: $id) { ` + conn + ` } }`
	if ws.ID == "" {
		q = `query Campaigns($first: Int!, $after: String) { ` + conn + ` }`
	}
	var out []campaign
	cursor := ""
	for {
		var data struct {
			Workspace *struct {
				Campaigns campaignConn `json:"campaigns"`
			} `json:"workspace"`
			Campaigns campaignConn `json:"campaigns"`
		}
		vars := map[string]any{"first": pageSize, "after": cursorVar(cursor)}
		if ws.ID != "" {
			vars["id"] = ws.ID
		}
		if err := s.query(ctx, q, vars, &data); err != nil {
			return nil, err
		}
		page := data.Campaigns
		if data.Workspace != nil {
			page = data.Workspace.Campaigns
		}
		out = append(out, page.Nodes...)
		if cursor = page.PageInfo.next(); cursor == "" {
			return out, nil
		}
	}
}

type campaignConn struct {
	Nodes    []campaign `json:"nodes"`
	PageInfo pageInfo   `json:"pageInfo"`
}

const stepsQuery = `query Steps($id: ID!, $first: Int!, $after: String) {
  campaign(id: $id) {
    steps(first: $first, after: $after) {
      nodes {
        id
        position
        variations(first: 20) {
          nodes {
            id
            position
            type
            info {
              __typename
              ... on EmailVariation { subject body }
              ... on WaitVariation { unit value business }
            }
          }
          pageInfo { hasNextPage endCursor }
        }
      }
      pageInfo { hasNextPage endCursor }
    }
  }
}`

func (s *session) steps(ctx context.Context, campaignID string) ([]step, error) {
	var out []step
	cursor := ""
	for {
		var data struct {
			Campaign struct {
				Steps struct {
					Nodes    []step   `json:"nodes"`
					PageInfo pageInfo `json:"pageInfo"`
				} `json:"steps"`
			} `json:"campaign"`
		}
		vars := map[string]any{"id": campaignID, "first": pageSize, "after": cursorVar(cursor)}
		if err := s.query(ctx, stepsQuery, vars, &data); err != nil {
			return nil, err
		}
		out = append(out, data.Campaign.Steps.Nodes...)
		if cursor = data.Campaign.Steps.PageInfo.next(); cursor == "" {
			return out, nil
		}
	}
}

// stepNames describe the steps Warmbly cannot run, by variation type.
var stepNames = map[string]string{
	"CallVariation":                 "call",
	"CheckpointVariation":           "checkpoint",
	"LinkedinConnectionVariation":   "LinkedIn connection request",
	"LinkedinInmailVariation":       "LinkedIn InMail",
	"LinkedinMessageVariation":      "LinkedIn message",
	"LinkedinProfileVisitVariation": "LinkedIn profile visit",
	"SlackVariation":                "Slack",
	"SmsVariation":                  "SMS",
	"TaskVariation":                 "task",
	"WebhookVariation":              "webhook",
}

func waitMinutes(v variation) int {
	if v.Info.Value == nil {
		return 0
	}
	n := *v.Info.Value
	switch strings.TrimSuffix(strings.ToLower(v.Info.Unit), "s") {
	case "minute":
	case "hour":
		n *= 60
	default:
		n *= 24 * 60
	}
	return int(math.Round(n))
}

var htmlTag = regexp.MustCompile(`<[a-zA-Z][^>]*>`)

// buildSteps folds QuickMail's wait steps into the delay of the email after
// them and reports every step that is not an email.
func buildSteps(nodes []step) ([]outreachsource.Step, []string) {
	sort.SliceStable(nodes, func(i, j int) bool { return nodes[i].Position < nodes[j].Position })
	out := []outreachsource.Step{}
	var notes []string
	noted := map[string]bool{}
	note := func(n string) {
		if !noted[n] {
			noted[n] = true
			notes = append(notes, n)
		}
	}
	wait := 0
	for i, st := range nodes {
		vs := st.Variations.Nodes
		sort.SliceStable(vs, func(a, b int) bool { return vs[a].Position < vs[b].Position })
		switch kind := st.kind(); kind {
		case "WaitVariation":
			wait += waitMinutes(vs[0])
			if vs[0].Info.Business {
				note("QuickMail waits counted in business days are brought over as calendar days.")
			}
		case "EmailVariation":
			e := outreachsource.Step{WaitDays: wait / (24 * 60), WaitMinutes: wait % (24 * 60)}
			wait = 0
			for _, v := range vs {
				if v.Info.Typename != "EmailVariation" {
					continue
				}
				subj := translate(v.Info.Subject)
				body := translate(v.Info.Body)
				for _, n := range append(subj.notes, body.notes...) {
					note(n)
				}
				e.Variants = append(e.Variants, outreachsource.Variant{
					Subject: strings.TrimSpace(subj.text),
					Body:    body.text,
					HTML:    htmlTag.MatchString(body.text),
				})
			}
			if st.Variations.PageInfo.HasNextPage {
				note(fmt.Sprintf("Step %d has more than %d variations; only the first %d are brought over.", i+1, len(vs), len(vs)))
			}
			out = append(out, e)
		default:
			name := stepNames[kind]
			if name == "" {
				name = "unknown"
			}
			note(fmt.Sprintf("Step %d (%s) is not brought over.", i+1, name))
		}
	}
	return out, notes
}

func (s *session) Campaigns(ctx context.Context, ws outreachsource.Workspace) ([]outreachsource.Campaign, error) {
	camps, err := s.campaigns(ctx, ws, false)
	if err != nil {
		return nil, err
	}
	out := make([]outreachsource.Campaign, 0, len(camps))
	for _, c := range camps {
		nodes, err := s.steps(ctx, c.ID)
		if err != nil {
			return nil, err
		}
		steps, notes := buildSteps(nodes)
		cp := outreachsource.Campaign{
			ID:            c.ID,
			Name:          strings.TrimSpace(c.Name),
			Status:        c.status(),
			Steps:         steps,
			StopOnReply:   true,
			LeadCount:     c.LeadStatus.Total,
			UnmappedNotes: notes,
		}
		if t, err := time.Parse(time.RFC3339, c.CreatedAt); err == nil {
			t = t.UTC()
			cp.CreatedAt = &t
		}
		out = append(out, cp)
	}
	return out, nil
}
