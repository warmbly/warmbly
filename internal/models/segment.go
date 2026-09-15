package models

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/utils"
)

// Segment is a saved, reusable audience: a list of conditions over contacts
// plus per-contact manual overrides. Membership is evaluated at read time.
type Segment struct {
	ID             uuid.UUID          `json:"id"`
	OrganizationID uuid.UUID          `json:"organization_id"`
	CreatedBy      *uuid.UUID         `json:"created_by,omitempty"`
	Name           string             `json:"name"`
	Description    string             `json:"description"`
	Color          string             `json:"color"`
	Match          SegmentMatch       `json:"match"`
	Conditions     []SegmentCondition `json:"conditions"`

	// ContactCount is the live membership size; IncludedCount and
	// ExcludedCount are the manual overrides. Populated by reads.
	ContactCount  int `json:"contact_count"`
	IncludedCount int `json:"included_count"`
	ExcludedCount int `json:"excluded_count"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// SegmentMatch says whether every condition or any condition must hold.
type SegmentMatch string

const (
	SegmentMatchAll SegmentMatch = "all"
	SegmentMatchAny SegmentMatch = "any"
)

// SegmentMemberMode is a manual override on one contact.
type SegmentMemberMode string

const (
	SegmentMemberInclude SegmentMemberMode = "include"
	SegmentMemberExclude SegmentMemberMode = "exclude"
	// SegmentMemberAuto clears the override so the conditions decide again.
	SegmentMemberAuto SegmentMemberMode = "auto"
)

// SegmentCondition is one predicate. Field picks the column or derived value,
// Operator the comparison; scalar operators read Value, list operators read
// Values. Custom fields are addressed as "custom.<key>".
type SegmentCondition struct {
	Field    string   `json:"field"`
	Operator string   `json:"operator"`
	Value    string   `json:"value,omitempty"`
	Values   []string `json:"values,omitempty"`
}

// SegmentFieldKind groups fields by the operators they accept.
type SegmentFieldKind string

const (
	SegmentFieldText     SegmentFieldKind = "text"
	SegmentFieldEnum     SegmentFieldKind = "enum"
	SegmentFieldBool     SegmentFieldKind = "bool"
	SegmentFieldDate     SegmentFieldKind = "date"
	SegmentFieldNumber   SegmentFieldKind = "number"
	SegmentFieldCategory SegmentFieldKind = "category"
	SegmentFieldCampaign SegmentFieldKind = "campaign"
	SegmentFieldSegment  SegmentFieldKind = "segment"
)

// Segment condition operators.
const (
	SegOpEquals        = "equals"
	SegOpNotEquals     = "not_equals"
	SegOpContains      = "contains"
	SegOpNotContains   = "not_contains"
	SegOpStartsWith    = "starts_with"
	SegOpEndsWith      = "ends_with"
	SegOpIsEmpty       = "is_empty"
	SegOpIsNotEmpty    = "is_not_empty"
	SegOpIn            = "in"
	SegOpNotIn         = "not_in"
	SegOpIsTrue        = "is_true"
	SegOpIsFalse       = "is_false"
	SegOpBefore        = "before"
	SegOpAfter         = "after"
	SegOpWithinDays    = "within_days"
	SegOpNotWithinDays = "not_within_days"
	SegOpGT            = "gt"
	SegOpGTE           = "gte"
	SegOpLT            = "lt"
	SegOpLTE           = "lte"
)

// SegmentFieldSpec describes one filterable field for validation and for the
// dashboard's condition builder (GET /segments/fields).
type SegmentFieldSpec struct {
	Field string           `json:"field"`
	Label string           `json:"label"`
	Group string           `json:"group"`
	Kind  SegmentFieldKind `json:"kind"`
	// Options lists the accepted values of an enum field.
	Options []string `json:"options,omitempty"`
}

// SegmentFieldCatalog is every non-custom field a condition may name.
var SegmentFieldCatalog = []SegmentFieldSpec{
	{Field: "first_name", Label: "First name", Group: "Contact", Kind: SegmentFieldText},
	{Field: "last_name", Label: "Last name", Group: "Contact", Kind: SegmentFieldText},
	{Field: "email", Label: "Email", Group: "Contact", Kind: SegmentFieldText},
	{Field: "email_domain", Label: "Email domain", Group: "Contact", Kind: SegmentFieldText},
	{Field: "phone", Label: "Phone", Group: "Contact", Kind: SegmentFieldText},
	{Field: "subscribed", Label: "Subscribed", Group: "Contact", Kind: SegmentFieldBool},
	{Field: "suppressed", Label: "On the suppression list", Group: "Contact", Kind: SegmentFieldBool},
	{Field: "source", Label: "Source", Group: "Contact", Kind: SegmentFieldEnum, Options: []string{"unknown", "manual", "campaign", "import", "sheet_sync", "api", "ai_assistant", "form"}},
	{Field: "verification_status", Label: "Verification status", Group: "Contact", Kind: SegmentFieldEnum, Options: []string{"valid", "risky", "invalid", "unknown"}},
	{Field: "is_catch_all", Label: "Catch-all domain", Group: "Contact", Kind: SegmentFieldBool},
	{Field: "esp_provider", Label: "Email provider", Group: "Contact", Kind: SegmentFieldEnum, Options: []string{"gmail", "outlook", "other"}},
	{Field: "created_at", Label: "Created", Group: "Contact", Kind: SegmentFieldDate},
	{Field: "updated_at", Label: "Updated", Group: "Contact", Kind: SegmentFieldDate},
	{Field: "category", Label: "Category", Group: "Contact", Kind: SegmentFieldCategory},

	{Field: "company", Label: "Company name", Group: "Company", Kind: SegmentFieldText},

	{Field: "campaign", Label: "In campaign", Group: "Campaign activity", Kind: SegmentFieldCampaign},
	{Field: "campaign_count", Label: "Number of campaigns", Group: "Campaign activity", Kind: SegmentFieldNumber},
	{Field: "emails_sent", Label: "Emails sent", Group: "Email engagement", Kind: SegmentFieldNumber},
	{Field: "emails_opened", Label: "Emails opened", Group: "Email engagement", Kind: SegmentFieldNumber},
	{Field: "emails_clicked", Label: "Links clicked", Group: "Email engagement", Kind: SegmentFieldNumber},
	{Field: "emails_replied", Label: "Replies", Group: "Email engagement", Kind: SegmentFieldNumber},
	{Field: "emails_bounced", Label: "Bounces", Group: "Email engagement", Kind: SegmentFieldNumber},
	{Field: "last_sent_at", Label: "Last email sent", Group: "Email engagement", Kind: SegmentFieldDate},
	{Field: "last_opened_at", Label: "Last open", Group: "Email engagement", Kind: SegmentFieldDate},
	{Field: "last_clicked_at", Label: "Last click", Group: "Email engagement", Kind: SegmentFieldDate},
	{Field: "last_replied_at", Label: "Last reply", Group: "Email engagement", Kind: SegmentFieldDate},

	{Field: "segment", Label: "In segment", Group: "Segments", Kind: SegmentFieldSegment},
}

// SegmentCustomFieldPrefix addresses a contact custom field: "custom.industry".
const SegmentCustomFieldPrefix = "custom."

// Segment validation limits.
const (
	SegmentMaxConditions  = 50
	SegmentMaxListValues  = 200
	SegmentMaxNameLen     = 120
	SegmentMaxDescLen     = 1000
	SegmentMaxValueLen    = 500
	SegmentMaxNestingDeep = 5
	SegmentsPerOrgMax     = 200
)

var segmentColorRe = regexp.MustCompile(`^#[a-fA-F0-9]{6}$`)

// SegmentFieldSpecFor resolves a condition's field to its spec. Custom fields
// resolve to a synthetic text spec.
func SegmentFieldSpecFor(field string) (SegmentFieldSpec, bool) {
	if strings.HasPrefix(field, SegmentCustomFieldPrefix) {
		key := utils.NormalizeJSONKey(strings.TrimPrefix(field, SegmentCustomFieldPrefix))
		if key == "" || !utils.IsValidJSONKey(key) {
			return SegmentFieldSpec{}, false
		}
		return SegmentFieldSpec{Field: SegmentCustomFieldPrefix + key, Label: key, Group: "Custom field", Kind: SegmentFieldText}, true
	}
	for _, s := range SegmentFieldCatalog {
		if s.Field == field {
			return s, true
		}
	}
	return SegmentFieldSpec{}, false
}

// OperatorsForKind lists the operators a field kind accepts.
func OperatorsForKind(kind SegmentFieldKind) []string {
	switch kind {
	case SegmentFieldText:
		return []string{SegOpEquals, SegOpNotEquals, SegOpContains, SegOpNotContains, SegOpStartsWith, SegOpEndsWith, SegOpIsEmpty, SegOpIsNotEmpty}
	case SegmentFieldEnum:
		return []string{SegOpIn, SegOpNotIn}
	case SegmentFieldBool:
		return []string{SegOpIsTrue, SegOpIsFalse}
	case SegmentFieldDate:
		return []string{SegOpBefore, SegOpAfter, SegOpWithinDays, SegOpNotWithinDays, SegOpIsEmpty, SegOpIsNotEmpty}
	case SegmentFieldNumber:
		return []string{SegOpEquals, SegOpNotEquals, SegOpGT, SegOpGTE, SegOpLT, SegOpLTE}
	case SegmentFieldCategory, SegmentFieldCampaign:
		return []string{SegOpIn, SegOpNotIn, SegOpIsEmpty, SegOpIsNotEmpty}
	case SegmentFieldSegment:
		return []string{SegOpIn, SegOpNotIn}
	}
	return nil
}

// SegmentWrite is the create/update body.
type SegmentWrite struct {
	Name        *string             `json:"name,omitempty"`
	Description *string             `json:"description,omitempty"`
	Color       *string             `json:"color,omitempty"`
	Match       *SegmentMatch       `json:"match,omitempty"`
	Conditions  *[]SegmentCondition `json:"conditions,omitempty"`
}

// SegmentPreview is the body of POST /segments/preview: an unsaved definition
// to count. ID, when set, keeps that segment's manual overrides in the count.
type SegmentPreview struct {
	ID         *uuid.UUID         `json:"id,omitempty"`
	Match      SegmentMatch       `json:"match"`
	Conditions []SegmentCondition `json:"conditions"`
}

// SegmentMembersWrite sets a manual override on a batch of contacts.
type SegmentMembersWrite struct {
	ContactSelection
	Mode SegmentMemberMode `json:"mode"`
}

// SegmentAddToCampaign enrols the segment's current members as leads.
type SegmentAddToCampaign struct {
	CampaignID string `json:"campaign_id"`
}

// SegmentAddToCampaignResult reports how many leads were actually new.
type SegmentAddToCampaignResult struct {
	CampaignID uuid.UUID `json:"campaign_id"`
	Added      int       `json:"added"`
	Members    int       `json:"members"`
	// Status is the campaign's status at enrol time, for the wake/restart decision.
	Status string `json:"-"`
}

// CampaignSegmentsMax bounds how many segments one campaign can link.
const CampaignSegmentsMax = 20

// CampaignSegmentsWrite replaces a campaign's linked segments.
type CampaignSegmentsWrite struct {
	SegmentIDs []string `json:"segment_ids"`
}

// CampaignAudienceChange is what replacing a campaign's linked segments did to
// the leads that were already there. Detaching a link withdraws the audience
// it brought; Contacted is the part of that audience the campaign had already
// written to, which stays.
type CampaignAudienceChange struct {
	Withdrawn int `json:"withdrawn"`
	Contacted int `json:"contacted"`
}

// CampaignSegmentLink is one segment linked to a campaign, for the Leads tab;
// the counts are live: members now, members that are leads, members held out.
type CampaignSegmentLink struct {
	SegmentID    uuid.UUID `json:"segment_id"`
	Name         string    `json:"name"`
	Color        string    `json:"color"`
	Description  string    `json:"description"`
	ContactCount int       `json:"contact_count"`
	LeadCount    int       `json:"lead_count"`
	HeldOutCount int       `json:"held_out_count"`
	LinkedAt     time.Time `json:"linked_at"`
}

// LinkedCampaign is one campaign that has segments attached, for the sync
// sweep and the targeted per-segment syncs.
type LinkedCampaign struct {
	CampaignID     uuid.UUID
	OrganizationID uuid.UUID
	Status         string
}

// ContactSegment is one segment a contact belongs to, with its override.
type ContactSegment struct {
	ID    uuid.UUID `json:"id"`
	Name  string    `json:"name"`
	Color string    `json:"color"`
	// Mode is "include" or "exclude" when the contact carries a manual
	// override, empty when the conditions alone decide.
	Mode SegmentMemberMode `json:"mode,omitempty"`
	// Member is whether the contact is currently in the segment.
	Member bool `json:"member"`
}

// SegmentOverride is one manually included or excluded contact.
type SegmentOverride struct {
	ContactID uuid.UUID         `json:"contact_id"`
	FirstName string            `json:"first_name"`
	LastName  string            `json:"last_name"`
	Email     string            `json:"email"`
	Company   string            `json:"company"`
	Mode      SegmentMemberMode `json:"mode"`
	CreatedAt time.Time         `json:"created_at"`
}

// SegmentOverridesMax bounds one overrides listing.
const SegmentOverridesMax = 500

// ValidateSegmentName trims and bounds the name.
func ValidateSegmentName(name string) (string, *errx.Error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errx.New(errx.BadRequest, "segment name is required")
	}
	if len(name) > SegmentMaxNameLen {
		return "", errx.New(errx.BadRequest, fmt.Sprintf("segment name must be at most %d characters", SegmentMaxNameLen))
	}
	return name, nil
}

// ValidateSegmentColor accepts a #rrggbb color.
func ValidateSegmentColor(color string) (string, *errx.Error) {
	color = strings.ToLower(strings.TrimSpace(color))
	if !segmentColorRe.MatchString(color) {
		return "", errx.New(errx.BadRequest, "color must be a #rrggbb value")
	}
	return color, nil
}

// ValidateSegmentMatch accepts all|any.
func ValidateSegmentMatch(m SegmentMatch) *errx.Error {
	if m != SegmentMatchAll && m != SegmentMatchAny {
		return errx.New(errx.BadRequest, "match must be all or any")
	}
	return nil
}

// ValidateSegmentConditions normalizes every condition in place and rejects
// anything the SQL builder would not know how to compile. selfID, when set,
// refuses a segment that references itself.
func ValidateSegmentConditions(conds []SegmentCondition, selfID *uuid.UUID) *errx.Error {
	if len(conds) > SegmentMaxConditions {
		return errx.New(errx.BadRequest, fmt.Sprintf("at most %d conditions per segment", SegmentMaxConditions))
	}
	for i := range conds {
		c := &conds[i]
		c.Field = strings.TrimSpace(c.Field)
		c.Operator = strings.TrimSpace(c.Operator)
		spec, ok := SegmentFieldSpecFor(c.Field)
		if !ok {
			return errx.New(errx.BadRequest, fmt.Sprintf("condition %d: unknown field %q", i+1, c.Field))
		}
		c.Field = spec.Field
		if !containsString(OperatorsForKind(spec.Kind), c.Operator) {
			return errx.New(errx.BadRequest, fmt.Sprintf("condition %d: operator %q is not valid for %s", i+1, c.Operator, spec.Label))
		}
		if len(c.Value) > SegmentMaxValueLen {
			return errx.New(errx.BadRequest, fmt.Sprintf("condition %d: value too long", i+1))
		}
		if len(c.Values) > SegmentMaxListValues {
			return errx.New(errx.BadRequest, fmt.Sprintf("condition %d: at most %d values", i+1, SegmentMaxListValues))
		}
		switch c.Operator {
		case SegOpIsEmpty, SegOpIsNotEmpty, SegOpIsTrue, SegOpIsFalse:
			c.Value, c.Values = "", nil
			continue
		case SegOpIn, SegOpNotIn:
			c.Value = ""
			if len(c.Values) == 0 {
				return errx.New(errx.BadRequest, fmt.Sprintf("condition %d: pick at least one value", i+1))
			}
		default:
			c.Values = nil
			if strings.TrimSpace(c.Value) == "" {
				return errx.New(errx.BadRequest, fmt.Sprintf("condition %d: a value is required", i+1))
			}
		}
		switch spec.Kind {
		case SegmentFieldEnum:
			for _, v := range c.Values {
				if !containsString(spec.Options, v) {
					return errx.New(errx.BadRequest, fmt.Sprintf("condition %d: %q is not a valid %s", i+1, v, spec.Label))
				}
			}
		case SegmentFieldCategory, SegmentFieldCampaign, SegmentFieldSegment:
			for _, v := range c.Values {
				id, err := uuid.Parse(v)
				if err != nil {
					return errx.New(errx.BadRequest, fmt.Sprintf("condition %d: %q is not a valid id", i+1, v))
				}
				if spec.Kind == SegmentFieldSegment && selfID != nil && id == *selfID {
					return errx.New(errx.BadRequest, "a segment cannot reference itself")
				}
			}
		case SegmentFieldNumber:
			n, err := strconv.Atoi(strings.TrimSpace(c.Value))
			if err != nil || n < 0 {
				return errx.New(errx.BadRequest, fmt.Sprintf("condition %d: value must be a whole number", i+1))
			}
			c.Value = strconv.Itoa(n)
		case SegmentFieldDate:
			switch c.Operator {
			case SegOpWithinDays, SegOpNotWithinDays:
				n, err := strconv.Atoi(strings.TrimSpace(c.Value))
				if err != nil || n < 1 || n > 3650 {
					return errx.New(errx.BadRequest, fmt.Sprintf("condition %d: days must be between 1 and 3650", i+1))
				}
				c.Value = strconv.Itoa(n)
			default:
				t, err := parseSegmentDate(c.Value)
				if err != nil {
					return errx.New(errx.BadRequest, fmt.Sprintf("condition %d: value must be a date (YYYY-MM-DD)", i+1))
				}
				c.Value = t.UTC().Format(time.RFC3339)
			}
		}
	}
	return nil
}

// SegmentReferences lists the other segments the conditions depend on.
func SegmentReferences(conds []SegmentCondition) []uuid.UUID {
	var out []uuid.UUID
	for _, c := range conds {
		if c.Field != "segment" {
			continue
		}
		for _, v := range c.Values {
			if id, err := uuid.Parse(v); err == nil {
				out = append(out, id)
			}
		}
	}
	return out
}

func parseSegmentDate(v string) (time.Time, error) {
	v = strings.TrimSpace(v)
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t, nil
	}
	return time.Parse("2006-01-02", v)
}

func containsString(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}
