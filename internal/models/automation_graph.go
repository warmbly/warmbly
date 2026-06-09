package models

// Automation condition vocabulary. Because an automation runs synchronously when
// its trigger event fires, every condition is decidable now from the event's
// data map (no "within N days" windows like campaign branches need). Node types
// and condition fields/operators are validated on write; the executor lives in
// the integration package next to the action handlers.

const (
	AutomationNodeTrigger   = "trigger"
	AutomationNodeCondition = "condition"
	AutomationNodeAction    = "action"

	// The conventional id of the single entry node.
	AutomationTriggerNodeID = "trigger"
)

// Condition fields. The generic "field" tests any key in the event data via the
// condition's Key + Operator; the rest are legacy semantic shortcuts kept valid
// for back-compat (older saved automations) and for the special random split.
const (
	AutoCondField      = "field"       // generic: test data[Key] with Operator
	AutoCondExpression = "expression"  // free-form Go-template predicate
	AutoCondIntent     = "intent"      // legacy: data["intent"]
	AutoCondConfidence = "confidence"  // legacy: classifier confidence float
	AutoCondSource     = "source"      // legacy: campaign / provider source
	AutoCondHasContact = "has_contact" // legacy: a contact email is present
	AutoCondRandom     = "random"      // deterministic percentage split
)

// Condition operators.
const (
	AutoOpEquals    = "equals"
	AutoOpNotEquals = "not_equals"
	AutoOpContains  = "contains"
	AutoOpGte       = "gte"
	AutoOpLte       = "lte"
	AutoOpExists    = "exists"
	AutoOpIsTrue    = "is_true"
	AutoOpChance    = "chance"
)

var automationConditionFields = map[string]bool{
	AutoCondField:      true,
	AutoCondExpression: true,
	AutoCondIntent:     true,
	AutoCondConfidence: true,
	AutoCondSource:     true,
	AutoCondHasContact: true,
	AutoCondRandom:     true,
}

var automationConditionOperators = map[string]bool{
	AutoOpEquals:    true,
	AutoOpNotEquals: true,
	AutoOpContains:  true,
	AutoOpGte:       true,
	AutoOpLte:       true,
	AutoOpExists:    true,
	AutoOpIsTrue:    true,
	AutoOpChance:    true,
}

func ValidAutomationConditionField(f string) bool    { return automationConditionFields[f] }
func ValidAutomationConditionOperator(o string) bool { return automationConditionOperators[o] }
