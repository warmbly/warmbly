package aitools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/microcosm-cc/bluemonday"

	"github.com/warmbly/warmbly/internal/app/webhook"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
	"github.com/warmbly/warmbly/internal/pkg/safehttp"
	"github.com/warmbly/warmbly/internal/pkg/webread"
)

const (
	fetchMaxBytes   = 2 << 20 // 2 MB cap on a fetched page
	fetchTimeout    = 10 * time.Second
	fetchCacheTTL   = 15 * time.Minute
	fetchCachePrefx = "aitools:fetch:"
)

// fetchTextPolicy strips all tags, leaving text content only.
var fetchTextPolicy = bluemonday.StrictPolicy()

var wsCollapse = regexp.MustCompile(`[ \t]*\n\s*\n\s*`)

func (d Deps) registerWebTools(r *Registry) {
	r.Register(Tool{
		Name:        "search_web",
		Description: "Search the public web and return the top results (title, url, snippet). Use to find current information about a company or person.",
		InputSchema: objectSchema(map[string]any{
			"query": strProp("The search query."),
			"limit": intProp("Max results (1-10, default 5)."),
		}, "query"),
		Risk:            generation.RiskRead,
		RequiredOrgPerm: models.PermUseAI,
		RequiredAPIPerm: models.APIPermAIAgent,
		Handler:         d.searchWeb,
	})

	r.Register(Tool{
		Name:        "fetch_url",
		Description: "Fetch a public web page and return its text content (HTML stripped). Only https public URLs are allowed; private and internal addresses are blocked.",
		InputSchema: objectSchema(map[string]any{
			"url": strProp("The https URL to fetch."),
		}, "url"),
		Risk:            generation.RiskRead,
		RequiredOrgPerm: models.PermUseAI,
		RequiredAPIPerm: models.APIPermAIAgent,
		Handler:         d.fetchURL,
	})
}

func (d Deps) searchWeb(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}](args)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(in.Query) == "" {
		return "", ErrInvalidArgs
	}
	client := d.Search
	if client == nil && d.WorkspaceSearch != nil {
		client = d.WorkspaceSearch(ctx, inv.OrgID)
	}
	if client == nil {
		return "", generation.ErrSearchNotConfigured
	}
	results, err := client.Search(ctx, in.Query, in.Limit)
	if err != nil {
		return "", err
	}
	return generation.FormatSearchResults(results), nil
}

func (d Deps) fetchURL(ctx context.Context, _ Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		URL string `json:"url"`
	}](args)
	if err != nil {
		return "", err
	}
	raw := strings.TrimSpace(in.URL)
	// SSRF guard: reuse the webhook validator (https only, publicly routable,
	// no credentials, blocks localhost/metadata/private IPs).
	if err := webhook.ValidateOutboundURL(raw); err != nil {
		return "", err
	}

	cacheKey := fetchCachePrefx + hashURL(raw)
	if d.Cache != nil {
		var cached string
		if cerr := d.Cache.GetJSON(ctx, cacheKey, &cached); cerr == nil && cached != "" {
			return cached, nil
		}
	}

	// safehttp.Client re-blocks private IPs at dial time (defends against DNS
	// rebinding after the URL-string check).
	client := safehttp.Client(fetchTimeout)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "WarmblyBot/1.0 (+https://warmbly.com)")
	req.Header.Set("Accept", "text/html,text/plain;q=0.9,*/*;q=0.5")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, fetchMaxBytes))
	if err != nil {
		return "", err
	}

	text, status, via := string(body), resp.StatusCode, ""
	isHTML := strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "html")
	if webread.Blocked(status) && d.Reader != nil {
		if p, rerr := d.Reader.Read(ctx, raw); rerr == nil {
			text, status, via, isHTML = p.HTML, http.StatusOK, p.Via, true
		}
	}
	if isHTML {
		text = htmlToText(text)
	}
	text = strings.TrimSpace(text)
	text = truncateRunes(text, 12000)

	out := map[string]any{"url": raw, "status": status, "text": text}
	if via != "" {
		out["read_via"] = via
	}
	result, err := jsonResult(out)
	if err != nil {
		return "", err
	}
	if d.Cache != nil {
		_ = d.Cache.SetJSON(ctx, cacheKey, result, fetchCacheTTL)
	}
	return result, nil
}

// htmlToText strips tags and collapses whitespace so a page fits in the model
// context.
func htmlToText(html string) string {
	stripped := fetchTextPolicy.Sanitize(html)
	stripped = wsCollapse.ReplaceAllString(stripped, "\n\n")
	return strings.TrimSpace(stripped)
}

func hashURL(u string) string {
	sum := sha256.Sum256([]byte(u))
	return hex.EncodeToString(sum[:])
}

// truncateRunes caps s to maxRunes runes (not bytes), appending an ellipsis
// when it trims, so a multi-byte UTF-8 sequence is never split mid-rune.
func truncateRunes(s string, maxRunes int) string {
	if len(s) <= maxRunes {
		return s // fast path: byte length <= rune cap means rune count is too
	}
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	return string(r[:maxRunes]) + "…"
}
