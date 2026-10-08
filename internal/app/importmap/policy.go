// Package importmap asks TypeSafe which field an import column holds when no
// header rule placed it. The model answers, resolve decides, and the state
// carries headers, value kinds and field names, never a cell.
package importmap

import (
	"context"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/typesafe"
)

// Thresholds. Tuning these is tuning the product.
const (
	// ConfFloor is the confidence below which a choice is not applied. A
	// mapping the user has to undo costs more than one they have to pick.
	ConfFloor = 0.70

	// MaxQuestions bounds one call. A file wider than this is a CRM dump, and
	// the columns past it stay for the user or "Keep N more as custom fields".
	MaxQuestions = 40

	// MaxCustomOptions bounds the custom fields offered, most used first, well
	// inside the API's 255 options per choice.
	MaxCustomOptions = 150

	// headerRunes caps one header in the state.
	headerRunes = 80

	// Timeout bounds the call so a slow judge costs the suggestion, never the
	// preview.
	Timeout = 4 * time.Second
)

// optionNone is the answer for a column that holds none of the fields offered.
const optionNone = "none"

// standardOptions are the standard fields the model may pick, and the value
// kinds each is offered for. Email is found by its values, and Subscribed and
// Categories stay the user's call: a wrong guess there unsubscribes people or
// mints categories from cell values.
var standardOptions = []struct {
	target models.ContactImportColumnTarget
	desc   string
	holds  []Shape
}{
	{models.ContactImportTargetFirstName, "The contact's first name, or given name", []Shape{ShapeText}},
	{models.ContactImportTargetLastName, "The contact's last name, surname, or family name", []Shape{ShapeText}},
	{models.ContactImportTargetCompany, "The name of the company or organization the contact works for", []Shape{ShapeText}},
	{models.ContactImportTargetPhone, "The contact's phone or mobile number", []Shape{ShapePhone, ShapeNumber, ShapeMixed}},
}

// placeholderRe is the name a column gets when its header cell is blank or
// the file has no header row; it says nothing about the column.
var placeholderRe = regexp.MustCompile(`^Column [0-9]+$`)

// Column is one column as the model sees it.
type Column struct {
	Header string `json:"header"`
	Holds  Shape  `json:"holds"`
}

type state struct {
	Columns      []Column `json:"columns"`
	CustomFields []string `json:"existing_custom_fields"`
}

// option is one answer a column question may give.
type option struct {
	target    models.ContactImportColumnTarget
	customKey string
}

// Infer fills the ignored columns of mapping that the model places with
// confidence, and returns the indexes it filled. headers and shapes are per
// column; existingKeys is most used first. Any failure returns mapping as it
// was: the deterministic suggestion always stands on its own.
func Infer(
	ctx context.Context,
	asker typesafe.Asker,
	mapping []models.ContactImportColumnMapping,
	headers []string,
	shapes []Shape,
	existingKeys []string,
) ([]models.ContactImportColumnMapping, []int) {
	if asker == nil || !headersAreNames(mapping, headers) {
		return mapping, nil
	}
	taken := takenTargets(mapping)
	options := buildOptions(taken, existingKeys)

	st := state{Columns: make([]Column, len(headers)), CustomFields: capKeys(existingKeys)}
	questions := map[string]typesafe.Question{}
	offered := map[int]map[string]bool{}
	for i, h := range headers {
		col := Column{Header: capRunes(strings.TrimSpace(h), headerRunes), Holds: shapeAt(shapes, i)}
		st.Columns[i] = col
		if len(questions) >= MaxQuestions || i >= len(mapping) || mapping[i].Target != models.ContactImportTargetIgnore ||
			col.Holds == ShapeEmpty || col.Header == "" || placeholderRe.MatchString(col.Header) {
			continue
		}
		criteria := columnCriteria(options, col.Holds)
		if len(criteria) <= 1 {
			continue
		}
		offered[i] = make(map[string]bool, len(criteria))
		for id := range criteria {
			offered[i][id] = true
		}
		questions[questionID(i)] = typesafe.Choice(
			"Which contact field does the column headed \""+col.Header+"\" hold? Its values are "+string(col.Holds)+".",
			criteria,
		)
	}
	if len(questions) == 0 {
		return mapping, nil
	}

	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	resp, err := asker.Ask(typesafe.WithUsage(ctx, "contact_import", ""), st, questions)
	if err != nil || resp == nil {
		return mapping, nil
	}
	return resolve(mapping, resp.Answers, options, offered, taken)
}

// resolve applies the answers: a confident choice, among the options that
// column was offered, of a field nobody fills yet maps its column, and when
// two columns want one field the more confident wins. It never touches a
// column the suggester already mapped.
func resolve(
	mapping []models.ContactImportColumnMapping,
	answers map[string]typesafe.Answer,
	options map[string]offer,
	offered map[int]map[string]bool,
	taken map[string]bool,
) ([]models.ContactImportColumnMapping, []int) {
	type candidate struct {
		col  int
		opt  option
		id   string
		conf float64
	}
	var cands []candidate
	for qid, a := range answers {
		col, ok := columnOf(qid)
		if !ok || col >= len(mapping) || mapping[col].Target != models.ContactImportTargetIgnore {
			continue
		}
		if a.Confidence < ConfFloor || a.Choice == optionNone || !offered[col][a.Choice] {
			continue
		}
		o, ok := options[a.Choice]
		if !ok {
			continue
		}
		cands = append(cands, candidate{col: col, opt: o.option, id: identity(o.option), conf: a.Confidence})
	}
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].conf != cands[j].conf {
			return cands[i].conf > cands[j].conf
		}
		return cands[i].col < cands[j].col
	})

	out := append([]models.ContactImportColumnMapping(nil), mapping...)
	claimed := make(map[string]bool, len(taken))
	for k := range taken {
		claimed[k] = true
	}
	var inferred []int
	for _, c := range cands {
		if claimed[c.id] {
			continue
		}
		claimed[c.id] = true
		out[c.col] = models.ContactImportColumnMapping{Index: c.col, Target: c.opt.target, CustomKey: c.opt.customKey}
		inferred = append(inferred, c.col)
	}
	sort.Ints(inferred)
	return out, inferred
}

// offer is one option with what it says to the model and which value kinds
// it is offered for (nil: every kind).
type offer struct {
	option
	desc  string
	holds []Shape
}

// buildOptions lists the fields still free, under short ids so no field name
// has to survive as an option key.
func buildOptions(taken map[string]bool, existingKeys []string) map[string]offer {
	options := map[string]offer{}
	for _, s := range standardOptions {
		o := option{target: s.target}
		if !taken[identity(o)] {
			options[string(s.target)] = offer{option: o, desc: s.desc, holds: s.holds}
		}
	}
	for i, k := range capKeys(existingKeys) {
		o := option{target: models.ContactImportTargetCustom, customKey: k}
		if !taken[identity(o)] {
			options["field_"+strconv.Itoa(i+1)] = offer{option: o, desc: "The workspace's existing custom field named \"" + k + "\""}
		}
	}
	return options
}

// columnCriteria is what one column is asked to choose from: the options its
// value kind allows, and none.
func columnCriteria(options map[string]offer, holds Shape) map[string]string {
	out := map[string]string{optionNone: "None of these: the column holds something else"}
	for id, o := range options {
		if o.holds == nil || slices.Contains(o.holds, holds) {
			out[id] = o.desc
		}
	}
	return out
}

// headersAreNames reports whether the header row is headers rather than a
// contact. No cell may be shaped like a value, and the Email column needs a
// real header: in a contact's row that cell is its address or blank.
func headersAreNames(mapping []models.ContactImportColumnMapping, headers []string) bool {
	for _, h := range headers {
		switch ShapeOf([]string{h}) {
		case ShapeText, ShapeLongText, ShapeEmpty:
		default:
			return false
		}
	}
	for _, m := range mapping {
		if m.Target == models.ContactImportTargetEmail && m.Index < len(headers) {
			h := strings.TrimSpace(headers[m.Index])
			return h != "" && !placeholderRe.MatchString(h)
		}
	}
	return false
}

// takenTargets is every destination the suggestion already fills.
func takenTargets(mapping []models.ContactImportColumnMapping) map[string]bool {
	taken := map[string]bool{}
	for _, m := range mapping {
		o := option{target: m.Target, customKey: m.CustomKey}
		if id := identity(o); id != "" {
			taken[id] = true
		}
	}
	return taken
}

// identity names where an option writes. Every target Infer can pick holds
// one column's value.
func identity(o option) string {
	switch o.target {
	case models.ContactImportTargetIgnore, "":
		return ""
	case models.ContactImportTargetCustom:
		return "custom:" + o.customKey
	}
	return string(o.target)
}

func capKeys(keys []string) []string {
	if len(keys) > MaxCustomOptions {
		return keys[:MaxCustomOptions]
	}
	if keys == nil {
		return []string{}
	}
	return keys
}

func capRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

func shapeAt(shapes []Shape, i int) Shape {
	if i < len(shapes) {
		return shapes[i]
	}
	return ShapeEmpty
}

func questionID(col int) string { return "column_" + strconv.Itoa(col+1) }

func columnOf(id string) (int, bool) {
	const prefix = "column_"
	if len(id) <= len(prefix) || id[:len(prefix)] != prefix {
		return 0, false
	}
	n, err := strconv.Atoi(id[len(prefix):])
	if err != nil || n < 1 {
		return 0, false
	}
	return n - 1, true
}
