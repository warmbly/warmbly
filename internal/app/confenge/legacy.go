package confenge

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/warmbly/warmbly/internal/models"
)

// DetectAndNormalize accepts either a native confenge.outreach.v1 document or
// common legacy extra-cli artifacts (leads.json array, commercial-leads wrapper,
// single-run export). Missing fields are left empty; never invented.
func DetectAndNormalize(raw []byte) (*Feed, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("empty payload")
	}
	// Peek schema_version without failing on legacy shapes.
	var peek struct {
		SchemaVersion string          `json:"schema_version"`
		Leads         json.RawMessage `json:"leads"`
	}
	_ = json.Unmarshal(raw, &peek)
	var wrap map[string]any
	_ = json.Unmarshal(raw, &wrap)
	if isOperatorProjection(strField(wrap, "schema_id"), peek.SchemaVersion, wrap) {
		return normalizeOperatorProjection(raw)
	}
	if peek.SchemaVersion == models.OutreachSchemaV1 {
		return ParseFeed(raw)
	}

	// Top-level array of leads (leads.json).
	trim := strings.TrimSpace(string(raw))
	if strings.HasPrefix(trim, "[") {
		var arr []map[string]any
		if err := json.Unmarshal(raw, &arr); err != nil {
			return nil, fmt.Errorf("legacy leads array: %w", err)
		}
		return legacyMapToFeed(arr, FeedSource{System: "extra-cli", ProfileID: "confenge"}), nil
	}

	// Object with leads key (commercial run export / commercial-leads.json).
	if wrap == nil {
		if err := json.Unmarshal(raw, &wrap); err != nil {
			return nil, fmt.Errorf("legacy object: %w", err)
		}
	}
	src := FeedSource{
		System:         "extra-cli",
		ProfileID:      "confenge",
		RunID:          strField(wrap, "run_id", "id", "source_run_id"),
		SnapshotHash:   strField(wrap, "snapshot_hash"),
		RepoSHA:        strField(wrap, "repo_sha", "git_sha"),
		ProfileVersion: strField(wrap, "profile_version"),
	}
	if v := strField(wrap, "profile_id", "profile"); v != "" {
		src.ProfileID = v
	}
	leadsRaw, ok := wrap["leads"]
	if !ok {
		// Some exports nest under "data" or "queue".
		if d, ok := wrap["data"].(map[string]any); ok {
			leadsRaw = d["leads"]
		} else if q, ok := wrap["queue"].([]any); ok {
			leadsRaw = q
		}
	}
	arr, err := asMapSlice(leadsRaw)
	if err != nil {
		return nil, fmt.Errorf("legacy leads: %w", err)
	}
	feed := legacyMapToFeed(arr, src)
	if g := strField(wrap, "generated_at", "created_at"); g != "" {
		feed.GeneratedAt = g
	}
	return feed, nil
}

func legacyMapToFeed(arr []map[string]any, src FeedSource) *Feed {
	leads := make([]FeedLead, 0, len(arr))
	for i, m := range arr {
		leads = append(leads, mapLegacyLead(m, i+1))
	}
	return &Feed{
		SchemaVersion: models.OutreachSchemaV1,
		Source:        src,
		Pagination:    FeedPagination{HasMore: false},
		Leads:         leads,
		Legacy:        true,
	}
}

func mapLegacyLead(m map[string]any, rankFallback int) FeedLead {
	cnpj := digitsOnly(strField(m, "cnpj14", "cnpj", "company_cnpj"))
	root := digitsOnly(strField(m, "cnpj_root", "cnpj_basico"))
	if root == "" && len(cnpj) == 14 {
		root = cnpj[:8]
	}
	razao := strField(m, "razao_social", "company_name", "name")
	fantasia := strField(m, "nome_fantasia", "trade_name")
	sourceLead := strField(m, "source_lead_id", "lead_id", "id")
	if sourceLead == "" && cnpj != "" {
		sourceLead = "cnpj:" + cnpj
	}

	// Nested company object if present.
	if co, ok := m["company"].(map[string]any); ok {
		if v := digitsOnly(strField(co, "cnpj14", "cnpj")); v != "" {
			cnpj = v
		}
		if v := digitsOnly(strField(co, "cnpj_root")); v != "" {
			root = v
		}
		if v := strField(co, "razao_social", "name"); v != "" {
			razao = v
		}
		if v := strField(co, "nome_fantasia"); v != "" {
			fantasia = v
		}
	}

	rank := intField(m, "rank_position", "rank", "priority_rank")
	if rank == 0 {
		rank = rankFallback
	}
	score := floatField(m, "score_total", "score", "priority_score")
	tier := strField(m, "priority", "tier", "priority_tier")
	offerCode := strField(m, "suggested_offer", "service_code", "offer")
	offerName := strField(m, "service_name", "suggested_offer_name")
	entry := strField(m, "entry_offer", "next_human_step")
	state := strField(m, "commercial_state")
	if state == "" {
		state = "NEW"
	}

	momentCode := strField(m, "moment_code", "signal_primary")
	momentSummary := strField(m, "moment_summary", "explanation", "why")
	if momentSummary == "" {
		// signals_fired is common in commercial_leads exports; take first name only.
		if sigs, ok := m["signals_fired"].([]any); ok && len(sigs) > 0 {
			switch s := sigs[0].(type) {
			case string:
				momentCode = firstNonEmpty(momentCode, s)
				momentSummary = s
			case map[string]any:
				momentCode = firstNonEmpty(momentCode, strField(s, "code", "id", "name"))
				momentSummary = strField(s, "summary", "label", "name", "code")
			}
		}
	}

	fact := strField(m, "fact_to_mention", "public_fact")
	question := strField(m, "question_to_ask")
	cta := strField(m, "cta")

	// messaging_context nested
	if mc, ok := m["messaging_context"].(map[string]any); ok {
		fact = firstNonEmpty(fact, strField(mc, "fact_to_mention"))
		question = firstNonEmpty(question, strField(mc, "question_to_ask"))
		cta = firstNonEmpty(cta, strField(mc, "cta"))
	}

	contacts := mapLegacyContacts(m)
	evidence := mapLegacyEvidence(m)

	var contracts []json.RawMessage
	if raw, ok := m["contracts"]; ok {
		if b, err := json.Marshal(raw); err == nil {
			var arr []json.RawMessage
			if json.Unmarshal(b, &arr) == nil {
				contracts = arr
			}
		}
	}

	municipio := strField(m, "municipio", "city")
	uf := strField(m, "uf", "state")
	website := strField(m, "website", "site")
	if co, ok := m["company"].(map[string]any); ok {
		municipio = firstNonEmpty(municipio, strField(co, "municipio", "city"))
		uf = firstNonEmpty(uf, strField(co, "uf", "state"))
		website = firstNonEmpty(website, strField(co, "website"))
	}

	return FeedLead{
		SourceLeadID: sourceLead,
		Company: FeedCompany{
			CNPJ14:       cnpj,
			CNPJRoot:     root,
			RazaoSocial:  razao,
			NomeFantasia: fantasia,
			Municipio:    municipio,
			UF:           uf,
			Website:      website,
		},
		Priority: FeedPriority{
			Rank:       rank,
			Score:      score,
			Tier:       tier,
			Confidence: strField(m, "confidence", "priority_confidence"),
		},
		Moment: FeedMoment{
			Code:       momentCode,
			Summary:    momentSummary,
			ObservedAt: strField(m, "observed_at", "moment_observed_at"),
			Confidence: strField(m, "moment_confidence"),
		},
		Offer: FeedOffer{
			ServiceCode: offerCode,
			ServiceName: offerName,
			EntryOffer:  entry,
			Rationale:   strField(m, "rationale", "offer_rationale"),
		},
		MessagingContext: FeedMessaging{
			FactToMention: fact,
			QuestionToAsk: question,
			CTA:           cta,
		},
		Contacts:        contacts,
		Contracts:       contracts,
		Evidence:        evidence,
		CommercialState: state,
	}
}

func mapLegacyContacts(m map[string]any) []FeedContact {
	var out []FeedContact
	raw, ok := m["contacts"]
	if !ok {
		// Flat single contact fields.
		email := strField(m, "email", "contact_email")
		name := strField(m, "contact_name", "contato")
		if email == "" && name == "" {
			return nil
		}
		return []FeedContact{{
			SourceContactID:    strField(m, "source_contact_id", "contact_id"),
			Name:               name,
			Role:               strField(m, "role", "cargo", "contact_role"),
			Email:              email,
			Phone:              strField(m, "phone", "telefone"),
			VerificationStatus: strField(m, "verification_status", "email_status"),
			Recommended:        true,
		}}
	}
	arr, err := asMapSlice(raw)
	if err != nil {
		return nil
	}
	for i, c := range arr {
		sid := strField(c, "source_contact_id", "id", "contact_id")
		if sid == "" {
			sid = fmt.Sprintf("legacy-%d", i+1)
		}
		out = append(out, FeedContact{
			SourceContactID:    sid,
			Name:               strField(c, "name", "nome"),
			Role:               strField(c, "role", "cargo"),
			Email:              strField(c, "email"),
			Phone:              strField(c, "phone", "telefone"),
			LinkedInURL:        strField(c, "linkedin_url", "linkedin"),
			SourceURL:          strField(c, "source_url"),
			SourceDocument:     strField(c, "source_document"),
			SourceDate:         strField(c, "source_date"),
			VerificationStatus: strField(c, "verification_status", "status"),
			Confidence:         strField(c, "confidence"),
			Recommended:        boolField(c, "recommended", true),
		})
	}
	return out
}

func mapLegacyEvidence(m map[string]any) []FeedEvidence {
	raw, ok := m["evidence"]
	if !ok {
		// evidence_ids alone cannot invent full evidence rows.
		return nil
	}
	arr, err := asMapSlice(raw)
	if err != nil {
		return nil
	}
	out := make([]FeedEvidence, 0, len(arr))
	for i, e := range arr {
		id := strField(e, "id", "source_evidence_id", "evidence_id")
		if id == "" {
			id = fmt.Sprintf("legacy-ev-%d", i+1)
		}
		out = append(out, FeedEvidence{
			ID:             id,
			Type:           strField(e, "type", "evidence_type"),
			Title:          strField(e, "title"),
			URL:            strField(e, "url", "source_url"),
			Document:       strField(e, "document"),
			Date:           strField(e, "date", "evidence_date"),
			Location:       strField(e, "location", "page", "section"),
			Excerpt:        strField(e, "excerpt", "snippet", "trecho"),
			Synthesis:      strField(e, "synthesis", "summary"),
			EpistemicClass: strField(e, "epistemic_class", "class"),
			Reliability:    strField(e, "reliability"),
			ConsultedAt:    strField(e, "consulted_at"),
		})
	}
	return out
}

func asMapSlice(v any) ([]map[string]any, error) {
	if v == nil {
		return nil, nil
	}
	switch t := v.(type) {
	case []map[string]any:
		return t, nil
	case []any:
		out := make([]map[string]any, 0, len(t))
		for _, item := range t {
			m, ok := item.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("lead item is not an object")
			}
			out = append(out, m)
		}
		return out, nil
	default:
		// Re-marshal path for typed JSON.
		b, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		var out []map[string]any
		if err := json.Unmarshal(b, &out); err != nil {
			return nil, err
		}
		return out, nil
	}
}

func strField(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			switch t := v.(type) {
			case string:
				return strings.TrimSpace(t)
			case float64:
				// JSON numbers
				if t == float64(int64(t)) {
					return strconv.FormatInt(int64(t), 10)
				}
				return strconv.FormatFloat(t, 'f', -1, 64)
			case json.Number:
				return t.String()
			case bool:
				return strconv.FormatBool(t)
			}
		}
	}
	return ""
}

func intField(m map[string]any, keys ...string) int {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			switch t := v.(type) {
			case float64:
				return int(t)
			case int:
				return t
			case int64:
				return int(t)
			case string:
				n, _ := strconv.Atoi(strings.TrimSpace(t))
				return n
			case json.Number:
				n, _ := t.Int64()
				return int(n)
			}
		}
	}
	return 0
}

func floatField(m map[string]any, keys ...string) float64 {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			switch t := v.(type) {
			case float64:
				return t
			case int:
				return float64(t)
			case int64:
				return float64(t)
			case string:
				f, _ := strconv.ParseFloat(strings.TrimSpace(t), 64)
				return f
			case json.Number:
				f, _ := t.Float64()
				return f
			}
		}
	}
	return 0
}

func boolField(m map[string]any, key string, def bool) bool {
	v, ok := m[key]
	if !ok || v == nil {
		return def
	}
	switch t := v.(type) {
	case bool:
		return t
	case string:
		b, err := strconv.ParseBool(t)
		if err != nil {
			return def
		}
		return b
	default:
		return def
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
