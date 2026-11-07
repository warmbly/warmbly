package instancesettings

import "strings"

// Usage prices what a workspace's balance is charged: the provider's own price
// for what a call or a request used, plus the markup.
type Usage struct {
	// MarkupPercent is added on top of every provider price; nil is the
	// compiled default, so a document from before this section keeps it.
	MarkupPercent *int `json:"markup_percent"`
	// Models prices AI tokens per million, matched on the model name: an exact
	// match first, then the longest prefix, then the "*" row.
	Models []ModelPrice `json:"models"`
	// Units prices everything that is not tokens, in dollars per unit before
	// the markup (a web search, a placement test).
	Units map[string]float64 `json:"units"`
}

// ModelPrice is one model's price per million input and output tokens.
type ModelPrice struct {
	Model         string  `json:"model"`
	InputPerMTok  float64 `json:"input_per_mtok"`
	OutputPerMTok float64 `json:"output_per_mtok"`
}

const (
	UsageMarkupDefault = 25
	UsageMarkupMax     = 500
	// UnitWebSearch is one web search made for an AI step.
	UnitWebSearch = "web_search"
	// UnitPlacementTest is one inbox placement test past the free allowance.
	UnitPlacementTest = "placement_test"
	// UnitLeadWebsiteRead is one lead website read, with the people it names and their addresses.
	UnitLeadWebsiteRead = "lead_website_read"
	// UnitLeadPersonEmail is finding one named person's email on request.
	UnitLeadPersonEmail = "lead_person_email"
	// UnitLeadSearchPage is one page of a lead search through a source the instance pays for.
	UnitLeadSearchPage = "lead_search_page"
	// AnyModel prices a model with no row of its own.
	AnyModel = "*"
)

// DefaultUsage is the compiled price list.
func DefaultUsage() Usage {
	markup := UsageMarkupDefault
	return Usage{
		MarkupPercent: &markup,
		Models: []ModelPrice{
			{Model: "claude-sonnet-4", InputPerMTok: 3, OutputPerMTok: 15},
			{Model: "claude-haiku-4", InputPerMTok: 1, OutputPerMTok: 5},
			{Model: "claude-3-5-haiku", InputPerMTok: 0.8, OutputPerMTok: 4},
			{Model: "gpt-6-luna", InputPerMTok: 0.10, OutputPerMTok: 0.50},
			{Model: AnyModel, InputPerMTok: 3, OutputPerMTok: 15},
		},
		Units: map[string]float64{
			UnitWebSearch:       0.01,
			UnitPlacementTest:   0.40,
			UnitLeadWebsiteRead: 0.016,
			UnitLeadPersonEmail: 0.016,
			// Google Places Text Search list price; edit to match the provider.
			UnitLeadSearchPage: 0.032,
		},
	}
}

// Normalize clamps the markup, drops malformed rows and fills what a document
// written before this section existed is missing.
func (u *Usage) Normalize() {
	def := DefaultUsage()
	markup := UsageMarkupDefault
	if u.MarkupPercent != nil {
		markup = max(0, min(*u.MarkupPercent, UsageMarkupMax))
	}
	u.MarkupPercent = &markup
	if len(u.Models) == 0 {
		u.Models = def.Models
	}
	seen := map[string]bool{}
	// Fresh slice and map: the cached document shares the originals.
	models := make([]ModelPrice, 0, len(u.Models)+1)
	for _, m := range u.Models {
		m.Model = strings.TrimSpace(m.Model)
		if m.Model == "" || seen[m.Model] || m.InputPerMTok < 0 || m.OutputPerMTok < 0 {
			continue
		}
		seen[m.Model] = true
		models = append(models, m)
	}
	if !seen[AnyModel] {
		models = append(models, def.Models[len(def.Models)-1])
	}
	u.Models = models
	units := make(map[string]float64, len(u.Units)+len(def.Units))
	for k, v := range u.Units {
		units[k] = v
	}
	u.Units = units
	for k, v := range def.Units {
		if p, ok := u.Units[k]; !ok || p < 0 {
			u.Units[k] = v
		}
	}
}

// Markup is the resolved markup percent.
func (u Usage) Markup() int {
	if u.MarkupPercent == nil {
		return UsageMarkupDefault
	}
	return *u.MarkupPercent
}

// ModelPrice returns the row that prices a model.
func (u Usage) ModelPrice(model string) ModelPrice {
	model = strings.ToLower(strings.TrimSpace(model))
	var best *ModelPrice
	for i := range u.Models {
		m := &u.Models[i]
		key := strings.ToLower(m.Model)
		if key == model {
			return *m
		}
		if key != AnyModel && strings.HasPrefix(model, key) && (best == nil || len(key) > len(best.Model)) {
			best = m
		}
	}
	if best != nil {
		return *best
	}
	for _, m := range u.Models {
		if m.Model == AnyModel {
			return m
		}
	}
	return DefaultUsage().Models[len(DefaultUsage().Models)-1]
}
