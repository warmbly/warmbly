package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/models"
)

// segmentQuerier is the subset of pgx both the pool and a transaction satisfy.
type segmentQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// segmentDef is the part of a segment the predicate compiler needs.
type segmentDef struct {
	ID         uuid.UUID
	Match      models.SegmentMatch
	Conditions []models.SegmentCondition
}

// segmentBuilder compiles segment definitions into a WHERE fragment over the
// `contacts c` alias. Values are always bound, never interpolated; the only
// strings that reach the SQL text are column names picked from a fixed map.
type segmentBuilder struct {
	orgID uuid.UUID
	args  []any
	graph map[uuid.UUID]*segmentDef
}

func (b *segmentBuilder) bind(v any) string {
	b.args = append(b.args, v)
	return fmt.Sprintf("$%d", len(b.args))
}

// loadSegmentGraph fetches the referenced segments transitively, stopping at
// SegmentMaxNestingDeep hops: anything deeper compiles to FALSE.
func loadSegmentGraph(ctx context.Context, q segmentQuerier, orgID uuid.UUID, roots []uuid.UUID) (map[uuid.UUID]*segmentDef, error) {
	graph := map[uuid.UUID]*segmentDef{}
	pending := roots
	for depth := 0; depth <= models.SegmentMaxNestingDeep && len(pending) > 0; depth++ {
		var want []uuid.UUID
		for _, id := range pending {
			if _, ok := graph[id]; !ok {
				want = append(want, id)
			}
		}
		if len(want) == 0 {
			break
		}
		rows, err := q.Query(ctx, `SELECT id, match, conditions FROM segments WHERE organization_id = $1 AND id = ANY($2::uuid[])`, orgID, want)
		if err != nil {
			return nil, err
		}
		var next []uuid.UUID
		for rows.Next() {
			var d segmentDef
			var raw []byte
			if err := rows.Scan(&d.ID, &d.Match, &raw); err != nil {
				rows.Close()
				return nil, err
			}
			if err := json.Unmarshal(raw, &d.Conditions); err != nil {
				rows.Close()
				return nil, err
			}
			graph[d.ID] = &d
			next = append(next, models.SegmentReferences(d.Conditions)...)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
		pending = next
	}
	return graph, nil
}

// segmentClause compiles one segment (saved or preview) to a predicate.
// withOverrides folds the manual include/exclude rows in when the segment has
// an id. visited guards reference cycles: a loop compiles to FALSE.
func (b *segmentBuilder) segmentClause(def *segmentDef, withOverrides bool, visited map[uuid.UUID]bool) string {
	if def.ID != uuid.Nil {
		if visited[def.ID] {
			return "FALSE"
		}
		visited[def.ID] = true
		defer delete(visited, def.ID)
	}
	dyn := "FALSE"
	if len(def.Conditions) > 0 {
		parts := make([]string, 0, len(def.Conditions))
		for _, c := range def.Conditions {
			parts = append(parts, b.condition(c, visited))
		}
		joiner := " AND "
		if def.Match == models.SegmentMatchAny {
			joiner = " OR "
		}
		dyn = "(" + strings.Join(parts, joiner) + ")"
	}
	if !withOverrides || def.ID == uuid.Nil {
		return dyn
	}
	id := b.bind(def.ID)
	return fmt.Sprintf(
		"((%s OR c.id IN (SELECT sm.contact_id FROM segment_members sm WHERE sm.segment_id = %s AND sm.mode = 'include')) "+
			"AND c.id NOT IN (SELECT sm.contact_id FROM segment_members sm WHERE sm.segment_id = %s AND sm.mode = 'exclude'))",
		dyn, id, id)
}

var segmentTextColumns = map[string]string{
	"first_name":   "c.first_name",
	"last_name":    "c.last_name",
	"email":        "c.email",
	"email_domain": "split_part(c.email, '@', 2)",
	"phone":        "c.phone",
	"company":      "c.company",
}

var segmentEnumColumns = map[string]string{
	"source":              "c.source",
	"verification_status": "c.verification_status",
	"esp_provider":        "c.esp_provider",
	"mail_host":           "c.mail_host",
}

var segmentDateExprs = map[string]string{
	"created_at":      "c.created_at",
	"updated_at":      "c.updated_at",
	"last_sent_at":    "(SELECT MAX(p.sent_at) FROM campaign_contact_progress p WHERE p.contact_id = c.id AND " + progressIsEmailStep("p") + ")",
	"last_opened_at":  "(SELECT MAX(p.opened_at) FROM campaign_contact_progress p WHERE p.contact_id = c.id AND NOT p.opened_machine)",
	"last_clicked_at": "(SELECT MAX(p.clicked_at) FROM campaign_contact_progress p WHERE p.contact_id = c.id)",
	"last_replied_at": "(SELECT MAX(p.replied_at) FROM campaign_contact_progress p WHERE p.contact_id = c.id)",
}

var segmentNumberExprs = map[string]string{
	"campaign_count": "(SELECT COUNT(*) FROM campaign_leads cl WHERE cl.contact_id = c.id)",
	"emails_sent":    "(SELECT COUNT(*) FROM campaign_contact_progress p WHERE p.contact_id = c.id AND p.sent_at IS NOT NULL AND " + progressIsEmailStep("p") + ")",
	"emails_opened":  "(SELECT COUNT(*) FROM campaign_contact_progress p WHERE p.contact_id = c.id AND p.opened_at IS NOT NULL AND NOT p.opened_machine)",
	"emails_clicked": "(SELECT COUNT(*) FROM campaign_contact_progress p WHERE p.contact_id = c.id AND p.clicked_at IS NOT NULL)",
	"emails_replied": "(SELECT COUNT(*) FROM campaign_contact_progress p WHERE p.contact_id = c.id AND p.replied_at IS NOT NULL)",
	"emails_bounced": "(SELECT COUNT(*) FROM campaign_contact_progress p WHERE p.contact_id = c.id AND p.bounced_at IS NOT NULL)",
}

// segmentInt and segmentTime re-parse validated values so the bound parameter
// carries the Postgres type the cast expects.
func segmentInt(v string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(v))
	return n
}

func segmentTime(v string) time.Time {
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(v))
	if err != nil {
		t, _ = time.Parse("2006-01-02", strings.TrimSpace(v))
	}
	return t
}

// escapeLike makes a user string safe inside an ILIKE pattern.
func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

func (b *segmentBuilder) condition(c models.SegmentCondition, visited map[uuid.UUID]bool) string {
	spec, ok := models.SegmentFieldSpecFor(c.Field)
	if !ok {
		return "FALSE"
	}
	switch spec.Kind {
	case models.SegmentFieldText:
		expr, ok := segmentTextColumns[spec.Field]
		if !ok {
			key := strings.TrimPrefix(spec.Field, models.SegmentCustomFieldPrefix)
			expr = fmt.Sprintf("COALESCE(c.custom_fields ->> %s::text, '')", b.bind(key))
		}
		return b.textOp(expr, c)
	case models.SegmentFieldEnum:
		expr := segmentEnumColumns[spec.Field]
		list := b.bind(c.Values)
		if c.Operator == models.SegOpNotIn {
			return fmt.Sprintf("NOT (%s = ANY(%s::text[]))", expr, list)
		}
		return fmt.Sprintf("%s = ANY(%s::text[])", expr, list)
	case models.SegmentFieldBool:
		var expr string
		switch spec.Field {
		case "subscribed":
			expr = "c.subscribed"
		case "is_catch_all":
			expr = "c.is_catch_all"
		case "suppressed":
			expr = fmt.Sprintf("recipient_suppressed(%s, c.email)", b.bind(b.orgID))
		default:
			return "FALSE"
		}
		if c.Operator == models.SegOpIsFalse {
			return "NOT " + expr
		}
		return expr
	case models.SegmentFieldDate:
		expr := segmentDateExprs[spec.Field]
		switch c.Operator {
		case models.SegOpBefore:
			return fmt.Sprintf("%s < %s::timestamptz", expr, b.bind(segmentTime(c.Value)))
		case models.SegOpAfter:
			return fmt.Sprintf("%s > %s::timestamptz", expr, b.bind(segmentTime(c.Value)))
		case models.SegOpWithinDays:
			return fmt.Sprintf("%s >= now() - (%s::int * interval '1 day')", expr, b.bind(segmentInt(c.Value)))
		case models.SegOpNotWithinDays:
			return fmt.Sprintf("(%[1]s IS NULL OR %[1]s < now() - (%[2]s::int * interval '1 day'))", expr, b.bind(segmentInt(c.Value)))
		case models.SegOpIsEmpty:
			return fmt.Sprintf("%s IS NULL", expr)
		case models.SegOpIsNotEmpty:
			return fmt.Sprintf("%s IS NOT NULL", expr)
		}
	case models.SegmentFieldNumber:
		expr := segmentNumberExprs[spec.Field]
		ops := map[string]string{
			models.SegOpEquals: "=", models.SegOpNotEquals: "<>",
			models.SegOpGT: ">", models.SegOpGTE: ">=", models.SegOpLT: "<", models.SegOpLTE: "<=",
		}
		if op, ok := ops[c.Operator]; ok {
			return fmt.Sprintf("%s %s %s::bigint", expr, op, b.bind(segmentInt(c.Value)))
		}
	case models.SegmentFieldCategory, models.SegmentFieldCampaign:
		table, col := "contact_categories", "category_id"
		if spec.Kind == models.SegmentFieldCampaign {
			table, col = "campaign_leads", "campaign_id"
		}
		switch c.Operator {
		case models.SegOpIn:
			return fmt.Sprintf("EXISTS (SELECT 1 FROM %s x WHERE x.contact_id = c.id AND x.%s = ANY(%s::uuid[]))", table, col, b.bind(c.Values))
		case models.SegOpNotIn:
			return fmt.Sprintf("NOT EXISTS (SELECT 1 FROM %s x WHERE x.contact_id = c.id AND x.%s = ANY(%s::uuid[]))", table, col, b.bind(c.Values))
		case models.SegOpIsEmpty:
			return fmt.Sprintf("NOT EXISTS (SELECT 1 FROM %s x WHERE x.contact_id = c.id)", table)
		case models.SegOpIsNotEmpty:
			return fmt.Sprintf("EXISTS (SELECT 1 FROM %s x WHERE x.contact_id = c.id)", table)
		}
	case models.SegmentFieldSegment:
		parts := make([]string, 0, len(c.Values))
		for _, v := range c.Values {
			id, err := uuid.Parse(v)
			if err != nil {
				continue
			}
			def, ok := b.graph[id]
			if !ok {
				parts = append(parts, "FALSE")
				continue
			}
			parts = append(parts, b.segmentClause(def, true, visited))
		}
		if len(parts) == 0 {
			return "FALSE"
		}
		anyOf := "(" + strings.Join(parts, " OR ") + ")"
		if c.Operator == models.SegOpNotIn {
			return "NOT " + anyOf
		}
		return anyOf
	}
	return "FALSE"
}

func (b *segmentBuilder) textOp(expr string, c models.SegmentCondition) string {
	switch c.Operator {
	case models.SegOpEquals:
		return fmt.Sprintf("lower(%s) = lower(%s)", expr, b.bind(c.Value))
	case models.SegOpNotEquals:
		return fmt.Sprintf("lower(%s) <> lower(%s)", expr, b.bind(c.Value))
	case models.SegOpContains:
		return fmt.Sprintf("%s ILIKE %s", expr, b.bind("%"+escapeLike(c.Value)+"%"))
	case models.SegOpNotContains:
		return fmt.Sprintf("%s NOT ILIKE %s", expr, b.bind("%"+escapeLike(c.Value)+"%"))
	case models.SegOpStartsWith:
		return fmt.Sprintf("%s ILIKE %s", expr, b.bind(escapeLike(c.Value)+"%"))
	case models.SegOpEndsWith:
		return fmt.Sprintf("%s ILIKE %s", expr, b.bind("%"+escapeLike(c.Value)))
	case models.SegOpIsEmpty:
		return fmt.Sprintf("COALESCE(%s, '') = ''", expr)
	case models.SegOpIsNotEmpty:
		return fmt.Sprintf("COALESCE(%s, '') <> ''", expr)
	}
	return "FALSE"
}

// compileSegment returns the membership predicate for a saved segment or an
// unsaved preview, with `args` extended by whatever it bound. Callers append
// the clause to a query whose parameter list is exactly `args`.
func compileSegment(ctx context.Context, q segmentQuerier, orgID uuid.UUID, def *segmentDef, args []any) (string, []any, error) {
	roots := models.SegmentReferences(def.Conditions)
	if def.ID != uuid.Nil {
		roots = append(roots, def.ID)
	}
	graph, err := loadSegmentGraph(ctx, q, orgID, roots)
	if err != nil {
		return "", args, err
	}
	b := &segmentBuilder{orgID: orgID, args: args, graph: graph}
	visited := map[uuid.UUID]bool{}
	clause := b.segmentClause(def, true, visited)
	return clause, b.args, nil
}

// compileSavedSegment loads a segment by id and compiles it. An unknown id
// compiles to FALSE, so a stale filter matches nothing rather than erroring.
func compileSavedSegment(ctx context.Context, q segmentQuerier, orgID, segmentID uuid.UUID, args []any) (string, []any, error) {
	graph, err := loadSegmentGraph(ctx, q, orgID, []uuid.UUID{segmentID})
	if err != nil {
		return "", args, err
	}
	def, ok := graph[segmentID]
	if !ok {
		return "FALSE", args, nil
	}
	b := &segmentBuilder{orgID: orgID, args: args, graph: graph}
	visited := map[uuid.UUID]bool{}
	clause := b.segmentClause(def, true, visited)
	return clause, b.args, nil
}
