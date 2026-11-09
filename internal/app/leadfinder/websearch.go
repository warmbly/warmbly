package leadfinder

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/app/credits"
	"github.com/warmbly/warmbly/internal/app/instancesettings"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
	"github.com/warmbly/warmbly/internal/pkg/webread"
)

const (
	// emailSourceSearch marks an address a web search showed on a page about the business.
	emailSourceSearch = "search"
	// searchResults is how many hits one lead finder search asks for.
	searchResults = 8
	// searchMaxEmails bounds the addresses one search adds to a lead.
	searchMaxEmails = 4
)

// WorkspaceSearch is the web search a workspace set up itself (an MCP tool it
// picked), nil when it has none.
type WorkspaceSearch func(ctx context.Context, orgID uuid.UUID) generation.SearchClient

// searchSource is one web search a run can use. Only the instance's own is
// charged to the balance.
type searchSource struct {
	name    string
	client  generation.SearchClient
	charged bool
}

// webTools is how one workspace reads sites and searches the web.
type webTools struct {
	crawler  *Crawler
	searches []searchSource
	// workspaceJina is set when the workspace's own Jina key reads and searches.
	workspaceJina bool
}

// webTools resolves the workspace's readers and searches. The instance's
// reader goes first because a read costs the same whoever fetches it; the
// workspace's own searches go before the instance's, which is charged.
func (s *Service) webTools(ctx context.Context, orgID uuid.UUID) webTools {
	jinaKey := s.integrationKey(ctx, orgID, models.IntegrationJina, s.connections(ctx, orgID))
	var readers []webread.Reader
	if s.Reader != nil {
		readers = append(readers, s.Reader)
	}
	if jinaKey != "" {
		readers = append(readers, webread.Jina(jinaKey))
	}
	t := webTools{crawler: s.crawler, workspaceJina: jinaKey != ""}
	switch len(readers) {
	case 1:
		t.crawler = s.crawler.withFallback(readers[0])
	case 2:
		t.crawler = s.crawler.withFallback(readerChain(readers))
	}
	if s.WorkspaceSearch != nil {
		if c := s.WorkspaceSearch(ctx, orgID); c != nil {
			t.searches = append(t.searches, searchSource{name: "mcp", client: c})
		}
	}
	if jinaKey != "" {
		if c := generation.NewSearchClient("jina", "", jinaKey); c != nil {
			t.searches = append(t.searches, searchSource{name: "jina", client: c})
		}
	}
	if s.WebSearch != nil {
		t.searches = append(t.searches, searchSource{name: "instance", client: s.WebSearch, charged: s.metered()})
	}
	return t
}

// WorkspaceWebSearch is the web search the workspace brought itself (an MCP
// tool it picked, its Jina key), nil when it has none.
func (s *Service) WorkspaceWebSearch(ctx context.Context, orgID uuid.UUID) generation.SearchClient {
	var own searchChain
	for _, src := range s.webTools(ctx, orgID).searches {
		if !src.charged && src.name != "instance" {
			own = append(own, src.client)
		}
	}
	switch len(own) {
	case 0:
		return nil
	case 1:
		return own[0]
	}
	return own
}

type searchChain []generation.SearchClient

func (c searchChain) Search(ctx context.Context, query string, limit int) ([]generation.SearchResult, error) {
	var errs []error
	for _, s := range c {
		res, err := s.Search(ctx, query, limit)
		if err == nil && len(res) > 0 {
			return res, nil
		}
		if err != nil {
			errs = append(errs, err)
		}
	}
	return nil, errors.Join(errs...)
}

// fallbacks describes the workspace's readers and searches to the dashboard.
func (s *Service) fallbacks(ctx context.Context, orgID uuid.UUID) (WebFallback, WebFallback) {
	t := s.webTools(ctx, orgID)
	reader := WebFallback{Sources: []string{}}
	if s.Reader != nil {
		reader.Sources = append(reader.Sources, strings.Split(s.Reader.Name(), ",")...)
	}
	if t.workspaceJina {
		reader.Sources = append(reader.Sources, "workspace_jina")
	}
	reader.Available = len(reader.Sources) > 0
	search := WebFallback{Sources: []string{}}
	for _, src := range t.searches {
		name := src.name
		if name == "jina" {
			name = "workspace_jina"
		}
		search.Sources = append(search.Sources, name)
		if src.charged {
			search.CostUSD = s.Credits.Quote(ctx, credits.UnitUsage(instancesettings.UnitWebSearch, 1)).USD()
		}
	}
	search.Available = len(search.Sources) > 0
	return reader, search
}

type readers []webread.Reader

func readerChain(rs []webread.Reader) webread.Reader { return readers(rs) }

func (rs readers) Name() string { return "chain" }

func (rs readers) Read(ctx context.Context, target string) (*webread.Page, error) {
	var errs []error
	for _, r := range rs {
		p, err := r.Read(ctx, target)
		if err == nil {
			return p, nil
		}
		errs = append(errs, err)
	}
	return nil, errors.Join(errs...)
}

// search runs query through the first source that answers. The instance's
// search is charged one web search, refunded when it finds nothing; a
// workspace that cannot pay for it is left with what its own sources found.
func (s *Service) search(ctx context.Context, orgID uuid.UUID, sources []searchSource, query, idem string) []generation.SearchResult {
	for _, src := range sources {
		if ctx.Err() != nil {
			return nil
		}
		key := "lead_search:" + idem + ":" + src.name
		if src.charged {
			if _, err := s.Credits.ChargeUpfront(ctx, orgID, credits.UnitUsage(instancesettings.UnitWebSearch, 1), "lead_web_search", key); err != nil {
				if !errors.Is(err, credits.ErrInsufficientCredits) && !errors.Is(err, credits.ErrCapExceeded) {
					log.Warn().Err(err).Msg("leadfinder: could not charge a web search")
				}
				continue
			}
		}
		res, err := src.client.Search(ctx, query, searchResults)
		if err == nil && len(res) > 0 {
			return res
		}
		if src.charged {
			if _, rerr := s.Credits.RefundCharge(context.WithoutCancel(ctx), orgID, key, "lead_web_search_refund"); rerr != nil {
				log.Warn().Err(rerr).Msg("leadfinder: could not refund a web search")
			}
		}
	}
	return nil
}

// directoryLabels are sites that list businesses without being one, by the
// label before their public suffix.
var directoryLabels = toSet(`yelp facebook instagram linkedin twitter x tiktok youtube pinterest tripadvisor yellowpages yell
	google bing duckduckgo yahoo apple mapquest foursquare bbb wikipedia nextdoor angi angieslist houzz thumbtack opentable
	booking expedia hotels airbnb amazon ebay etsy zocdoc healthgrades vitals webmd glassdoor indeed crunchbase zoominfo dnb
	manta chamberofcommerce groupon doordash ubereats grubhub seamless menupages allmenus postmates birdeye superpages
	cylex hotfrog brownbook merchantcircle citysearch local judysbook trustpilot reddit medium quora wix squarespace
	godaddysites wordpress blogspot sites weebly linktr linktree carrd`)

// searchedHosts are the hosts in results that could be the business's own site.
func searchedHosts(results []generation.SearchResult) []string {
	var out []string
	for _, r := range results {
		u, err := url.Parse(r.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			continue
		}
		host := strings.ToLower(u.Hostname())
		reg := registrable(host)
		label, _, _ := strings.Cut(reg, ".")
		if host == "" || reg == "" || directoryLabels[label] || containsStr(out, host) {
			continue
		}
		out = append(out, host)
		if len(out) >= siteFindMaxSearched {
			break
		}
	}
	return out
}

// siteQuery asks the web for a business by name and where it is.
func siteQuery(l *models.Lead) string {
	q := `"` + strings.TrimSpace(l.Name) + `"`
	for _, part := range []string{l.City, l.Street} {
		if part = strings.TrimSpace(part); part != "" {
			q += " " + part
			break
		}
	}
	return q
}

// searchedEmails are the addresses at domain that results show.
func searchedEmails(results []generation.SearchResult, domain string) []FoundEmail {
	var out []FoundEmail
	seen := map[string]bool{}
	for _, r := range results {
		text := obfuscDot.ReplaceAllString(obfuscAt.ReplaceAllString(r.Title+" "+r.Snippet, "@"), ".")
		for _, m := range emailInText.FindAllString(text, 20) {
			e := acceptEmail(m)
			if e == "" || seen[e] || !emailDomainMatches(e, domain) {
				continue
			}
			seen[e] = true
			out = append(out, FoundEmail{Email: e, URL: r.URL, Source: emailSourceSearch})
			if len(out) >= searchMaxEmails {
				return out
			}
		}
	}
	return out
}
