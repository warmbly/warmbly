package companyenrich

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"net/mail"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// The fixed operations and input schemas come from the public OpenAPI
// references linked in each entry. Unknown body/filter properties are retained
// for provider evolution; only documented types and bounds are checked here.
//
//go:embed operations.json
var operationsJSON []byte

type schema struct {
	Type       string            `json:"type"`
	Format     string            `json:"format"`
	Nullable   bool              `json:"nullable"`
	Properties map[string]schema `json:"properties"`
	Items      *schema           `json:"items"`
	Required   []string          `json:"required"`
	Enum       []any             `json:"enum"`
	Minimum    *float64          `json:"minimum"`
	Maximum    *float64          `json:"maximum"`
	MaxLength  int               `json:"maxLength"`
}

type operation struct {
	Method     string `json:"method"`
	Path       string `json:"path"`
	Parameters []struct {
		Name     string `json:"name"`
		Required bool   `json:"required"`
		Schema   schema `json:"schema"`
	} `json:"parameters"`
	Body schema `json:"body_schema"`
}

var operations = func() map[string]operation {
	var out map[string]operation
	if err := json.Unmarshal(operationsJSON, &out); err != nil {
		panic("invalid CompanyEnrich operation definitions")
	}
	return out
}()

func invalid(message string) *errx.Error {
	return errx.NewWithIdentifier(errx.BadRequest, "companyenrich_invalid_input", message)
}

func prepare(in models.CompanyEnrichQuery) (operation, []byte, url.Values, error) {
	op, ok := operations[in.Operation]
	if !ok {
		return op, nil, nil, invalid("choose a supported CompanyEnrich operation")
	}
	// Normalize direct Go callers as well as HTTP callers without converting
	// integer tokens through float64. Marshal also refuses cycles/NaN/Inf.
	raw, err := json.Marshal(in)
	if err != nil || len(raw) > MaxRequestBytes {
		return op, nil, nil, invalid("request must be valid JSON within 256 KiB")
	}
	if err = decodeJSON(raw, &in); err != nil {
		return op, nil, nil, invalid("request must be valid JSON")
	}
	if in.Body == nil {
		in.Body = map[string]any{}
	}
	if in.Query == nil {
		in.Query = map[string]any{}
	}
	if op.Method == "GET" && len(in.Body) > 0 {
		return op, nil, nil, invalid("this operation does not accept a body")
	}
	if in.Operation == "get_person" {
		id, e := personID(in.Query["id"])
		if e != nil {
			return op, nil, nil, e
		}
		in.Query["id"] = json.Number(id)
	}
	query := make(url.Values)
	known := map[string]bool{}
	for _, p := range op.Parameters {
		known[p.Name] = true
		v, exists := in.Query[p.Name]
		if !exists {
			if p.Required {
				return op, nil, nil, invalid("query." + p.Name + " is required")
			}
			continue
		}
		if e := validateSchema(v, p.Schema, "query."+p.Name, 0); e != nil {
			return op, nil, nil, e
		}
		if a, ok := v.([]any); ok {
			for _, item := range a {
				query.Add(p.Name, fmt.Sprint(item))
			}
		} else {
			query.Set(p.Name, fmt.Sprint(v))
		}
	}
	for key := range in.Query {
		if !known[key] {
			return op, nil, nil, invalid("query contains an unsupported parameter")
		}
	}
	if e := validateSchema(in.Body, op.Body, "body", 0); e != nil {
		return op, nil, nil, e
	}
	if e := validateOperation(in.Operation, in.Body, in.Query); e != nil {
		return op, nil, nil, e
	}
	var body []byte
	if op.Method == "POST" {
		body, err = json.Marshal(in.Body)
	}
	return op, body, query, err
}

func validateSchema(v any, s schema, label string, depth int) error {
	if depth > 24 {
		return invalid("filter nesting is too deep")
	}
	if v == nil && s.Nullable {
		return nil
	}
	if len(s.Enum) > 0 {
		found := false
		for _, item := range s.Enum {
			if reflect.DeepEqual(v, item) {
				found = true
				break
			}
		}
		if !found {
			return invalid(label + " has an unsupported value")
		}
	}
	switch s.Type {
	case "string":
		x, ok := v.(string)
		if !ok {
			return invalid(label + " must be a string")
		}
		if strings.TrimSpace(x) == "" {
			return invalid(label + " must not be empty")
		}
		if s.MaxLength > 0 && utf8.RuneCountInString(x) > s.MaxLength {
			return invalid(label + " is too long")
		}
		if s.Format == "uuid" {
			if id, e := uuid.Parse(x); e != nil || id == uuid.Nil {
				return invalid(label + " must be a UUID")
			}
		}
	case "integer", "number":
		n, ok := v.(json.Number)
		if !ok {
			return invalid(label + " must be a JSON number")
		}
		x, e := n.Float64()
		if e != nil || math.IsInf(x, 0) || math.IsNaN(x) {
			return invalid(label + " must be finite")
		}
		if s.Type == "integer" {
			i, e := n.Int64()
			if e != nil {
				return invalid(label + " must be an integer")
			}
			if s.Format == "int32" && (i < math.MinInt32 || i > math.MaxInt32) {
				return invalid(label + " is outside int32 range")
			}
		}
		if (s.Minimum != nil && x < *s.Minimum) || (s.Maximum != nil && x > *s.Maximum) {
			return invalid(label + " is outside the supported range")
		}
	case "boolean":
		if _, ok := v.(bool); !ok {
			return invalid(label + " must be a boolean")
		}
	case "array":
		a, ok := v.([]any)
		if !ok {
			return invalid(label + " must be an array")
		}
		if s.Items != nil {
			for _, item := range a {
				if e := validateSchema(item, *s.Items, label+"[]", depth+1); e != nil {
					return e
				}
			}
		}
	case "object":
		o, ok := v.(map[string]any)
		if !ok {
			return invalid(label + " must be an object")
		}
		for _, key := range s.Required {
			if _, ok := o[key]; !ok {
				return invalid(label + "." + key + " is required")
			}
		}
		for key, property := range s.Properties {
			if item, ok := o[key]; ok {
				if e := validateSchema(item, property, label+"."+key, depth+1); e != nil {
					return e
				}
			}
		}
	}
	return nil
}

func personID(v any) (string, error) {
	var raw string
	switch x := v.(type) {
	case string:
		raw = x
	case json.Number:
		raw = x.String()
	default:
		return "", invalid("query.id must be a positive int64 integer or decimal string")
	}
	if raw == "" || strings.Trim(raw, "0123456789") != "" {
		return "", invalid("query.id must be a positive decimal int64")
	}
	i, e := strconv.ParseInt(raw, 10, 64)
	if e != nil || i <= 0 {
		return "", invalid("query.id must be a positive int64")
	}
	return strconv.FormatInt(i, 10), nil
}

func validateOperation(name string, body, query map[string]any) error {
	switch name {
	case "enrich_company":
		return domainValue(query["domain"])
	case "workforce":
		_, id := query["id"]
		_, domain := query["domain"]
		if id == domain {
			return invalid("workforce requires exactly one query.id or query.domain")
		}
		if domain {
			return domainValue(query["domain"])
		}
	case "enrich_company_properties":
		found := false
		for _, key := range []string{"name", "linkedinUrl", "linkedinId", "twitterUrl", "facebookUrl", "instagramUrl", "youTubeUrl"} {
			if s, ok := body[key].(string); ok && strings.TrimSpace(s) != "" {
				found = true
			}
		}
		if !found {
			return invalid("supply at least one supported company matching property")
		}
	case "enrich_companies_batch":
		return domainList(body["domains"], 50)
	case "lookup_person":
		email, _ := body["email"].(string)
		parsed, e := mail.ParseAddress(email)
		if e != nil || parsed.Address != email || !strings.Contains(email, "@") {
			return invalid("body.email must be an email address")
		}
	case "similar_companies", "count_similar":
		if name == "similar_companies" && body["domain"] != nil {
			if body["domains"] != nil {
				return invalid("use either domain or domains, not both")
			}
			if e := domainValue(body["domain"]); e != nil {
				return e
			}
		} else if e := domainList(body["domains"], 10); e != nil {
			return e
		}
	}
	if name == "search_companies" || name == "search_people" || name == "similar_companies" {
		if _, ok := body["page"]; !ok {
			body["page"] = json.Number("1")
		}
		if _, ok := body["pageSize"]; !ok {
			body["pageSize"] = json.Number("10")
		}
		page, ok := body["page"].(json.Number)
		size, sok := body["pageSize"].(json.Number)
		if !ok || !sok {
			return invalid("page and pageSize must be integers")
		}
		p, pe := page.Int64()
		n, ne := size.Int64()
		if pe != nil || ne != nil || p < 1 || n < 1 || n > 100 || p > 10000/n {
			return invalid("pageSize must be 1–100 and page × pageSize must not exceed 10,000")
		}
	}
	// People companyFilter can include a similar seed subquery.
	if filter, ok := body["companyFilter"].(map[string]any); ok {
		if similar, ok := filter["similar"].(map[string]any); ok {
			if e := domainList(similar["domains"], 10); e != nil {
				return e
			}
		}
	}
	return nil
}

func domainValue(v any) error {
	s, ok := v.(string)
	if !ok || strings.TrimSpace(s) == "" || utf8.RuneCountInString(s) > 1024 {
		return invalid("domain must be a nonempty string of at most 1,024 characters")
	}
	return nil
}

func domainList(v any, max int) error {
	a, ok := v.([]any)
	if !ok || len(a) < 1 || len(a) > max {
		return invalid(fmt.Sprintf("domains must contain 1–%d entries", max))
	}
	for _, item := range a {
		if e := domainValue(item); e != nil {
			return e
		}
	}
	return nil
}
