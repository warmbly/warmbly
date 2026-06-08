package integration

import (
	"strings"

	"github.com/warmbly/warmbly/internal/models"
)

// project.go turns a Warmbly source record into provider destination properties
// using the connection's configured field map — this is the pivot that makes the
// CRM handlers config-driven instead of hardcoding props["firstname"]. When no
// map is configured the provider's default identity map is used, so an
// unconfigured connection behaves exactly as it did before the framework.

// projectFields applies a resolved field map to a source payload, producing
// {externalField: value}. Empty values are dropped (matching provider upsert
// semantics: don't overwrite with blanks).
func projectFields(entries []models.FieldMapEntry, source map[string]any) map[string]any {
	out := map[string]any{}
	for _, e := range entries {
		var val string
		if e.Transform == models.FieldTransformStatic {
			val = e.StaticValue
		} else {
			val = applyTransform(e.Transform, warmblyValue(source, e.WarmblyField))
		}
		if strings.TrimSpace(val) == "" {
			continue
		}
		out[e.ExternalField] = val
	}
	return out
}

// warmblyValue reads a source field. "custom:<key>" reads from the contact's
// custom_fields map; everything else is a top-level normalized field.
func warmblyValue(source map[string]any, field string) string {
	if strings.HasPrefix(field, "custom:") {
		key := strings.TrimPrefix(field, "custom:")
		switch cf := source["custom_fields"].(type) {
		case map[string]string:
			return strings.TrimSpace(cf[key])
		case map[string]any:
			if v, ok := cf[key].(string); ok {
				return strings.TrimSpace(v)
			}
		}
		return ""
	}
	return stringFromMap(source, field)
}

func applyTransform(t models.FieldTransform, v string) string {
	switch t {
	case models.FieldTransformUppercase:
		return strings.ToUpper(v)
	case models.FieldTransformLowercase:
		return strings.ToLower(v)
	case models.FieldTransformTrim:
		return strings.TrimSpace(v)
	default:
		return v
	}
}

// defaultObject is the CRM object an upsert action writes for a provider.
func defaultObject(provider models.IntegrationProvider) string {
	switch provider {
	case models.IntegrationPipedrive:
		return "person"
	case models.IntegrationClose:
		return "lead"
	default:
		return "contact"
	}
}

// defaultFieldMap is the provider's identity projection — the standard fields the
// handlers used to hardcode. Configured mappings overlay this.
func defaultFieldMap(provider models.IntegrationProvider) []models.FieldMapEntry {
	switch provider {
	case models.IntegrationHubSpot:
		return []models.FieldMapEntry{
			{WarmblyField: "email", ExternalField: "email"},
			{WarmblyField: "first_name", ExternalField: "firstname"},
			{WarmblyField: "last_name", ExternalField: "lastname"},
			{WarmblyField: "company", ExternalField: "company"},
			{WarmblyField: "phone", ExternalField: "phone"},
		}
	case models.IntegrationSalesforce:
		return []models.FieldMapEntry{
			{WarmblyField: "email", ExternalField: "Email"},
			{WarmblyField: "first_name", ExternalField: "FirstName"},
			{WarmblyField: "last_name", ExternalField: "LastName"},
			{WarmblyField: "phone", ExternalField: "Phone"},
		}
	case models.IntegrationPipedrive:
		return []models.FieldMapEntry{
			{WarmblyField: "name", ExternalField: "name"},
			{WarmblyField: "email", ExternalField: "email"},
			{WarmblyField: "phone", ExternalField: "phone"},
		}
	case models.IntegrationClose:
		return []models.FieldMapEntry{
			{WarmblyField: "name", ExternalField: "name"},
			{WarmblyField: "email", ExternalField: "email"},
			{WarmblyField: "phone", ExternalField: "phone"},
			{WarmblyField: "company", ExternalField: "company"},
		}
	}
	return nil
}

// effectiveFieldMap resolves the final field map for an object with precedence:
// provider defaults < connection-default rows (subscription_id NULL) <
// subscription-scoped rows < inline subscription config. Later entries override
// earlier ones keyed by destination field; original order is preserved.
func effectiveFieldMap(provider models.IntegrationProvider, object string, rows []models.IntegrationFieldMapping, subID string, inline []models.FieldMapEntry) []models.FieldMapEntry {
	byExt := map[string]models.FieldMapEntry{}
	order := []string{}
	add := func(e models.FieldMapEntry) {
		if e.ExternalField == "" {
			return
		}
		if _, seen := byExt[e.ExternalField]; !seen {
			order = append(order, e.ExternalField)
		}
		byExt[e.ExternalField] = e
	}

	for _, e := range defaultFieldMap(provider) {
		add(e)
	}
	rowToEntry := func(r models.IntegrationFieldMapping) models.FieldMapEntry {
		return models.FieldMapEntry{
			WarmblyField:  r.WarmblyField,
			ExternalField: r.ExternalField,
			Transform:     models.FieldTransform(r.Transform),
			StaticValue:   r.StaticValue,
		}
	}
	for _, r := range rows { // connection defaults
		if r.ObjectName == object && r.SubscriptionID == nil {
			add(rowToEntry(r))
		}
	}
	if subID != "" {
		for _, r := range rows { // subscription-scoped
			if r.ObjectName == object && r.SubscriptionID != nil && r.SubscriptionID.String() == subID {
				add(rowToEntry(r))
			}
		}
	}
	for _, e := range inline {
		add(e)
	}

	out := make([]models.FieldMapEntry, 0, len(order))
	for _, k := range order {
		out = append(out, byExt[k])
	}
	return out
}

// eventSource normalizes an event/dispatch payload into the Warmbly source field
// vocabulary the field map reads from.
func eventSource(data map[string]any) map[string]any {
	src := map[string]any{
		"email":      stringFromMap(data, "contact_email", "invitee_email", "email", "recipient"),
		"first_name": stringFromMap(data, "first_name", "contact_first_name"),
		"last_name":  stringFromMap(data, "last_name", "contact_last_name"),
		"name":       stringFromMap(data, "contact_name", "invitee_name", "name"),
		"company":    stringFromMap(data, "company", "contact_company"),
		"phone":      stringFromMap(data, "phone", "contact_phone"),
	}
	if cf, ok := data["custom_fields"]; ok {
		src["custom_fields"] = cf
	}
	return src
}

// contactEmail extracts the dedupe email from a dispatch payload.
func contactEmail(data map[string]any) string {
	return stringFromMap(data, "contact_email", "invitee_email", "email", "recipient")
}

// strProp reads a string value from a projected props map.
func strProp(props map[string]any, key string) string {
	if v, ok := props[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

// toStr coerces a projected value (always a string today) to string.
func toStr(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
