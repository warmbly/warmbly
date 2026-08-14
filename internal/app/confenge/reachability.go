package confenge

import (
	"strings"

	"github.com/warmbly/warmbly/internal/models"
)

// MapReachability translates an extra-cli (or fixture) reachability token
// into a canonical class. Empty input is left empty: Warmbly never invents
// a class when upstream omitted one. Unknown non-empty tokens fail closed
// to UNMAPPED (no auto-send).
func MapReachability(raw string) string {
	s := strings.ToUpper(strings.TrimSpace(raw))
	if s == "" {
		return ""
	}
	if mapped, ok := reachabilityAliases[s]; ok {
		return mapped
	}
	return models.ReachabilityUnmapped
}

var reachabilityAliases = map[string]string{
	"R1": models.ReachabilityR1Direct, "R1_DIRECT": models.ReachabilityR1Direct,
	"DIRECT": models.ReachabilityR1Direct, "DIRECT_EMAIL": models.ReachabilityR1Direct,
	"DIRECT_EMAIL_VALIDATED": models.ReachabilityR1Direct,
	"R2":                     models.ReachabilityR2Inferred, "R2_HIGH_CONFIDENCE_DIRECT": models.ReachabilityR2Inferred,
	"HIGH_CONFIDENCE_DIRECT": models.ReachabilityR2Inferred, "INFERRED_DIRECT": models.ReachabilityR2Inferred,
	"INFERRED_EMAIL": models.ReachabilityR2Inferred,
	"R3":             models.ReachabilityR3Routed, "R3_ROUTED_TO_NAMED_PERSON": models.ReachabilityR3Routed,
	"ROUTES_TO_NAMED_PERSON": models.ReachabilityR3Routed, "ROUTED": models.ReachabilityR3Routed,
	"ROUTED_TO_NAMED_PERSON": models.ReachabilityR3Routed,
	"R4":                     models.ReachabilityR4Role, "R4_ROLE_ROUTE": models.ReachabilityR4Role,
	"ROLE_ROUTE": models.ReachabilityR4Role, "ROLE_MAILBOX": models.ReachabilityR4Role,
	"R5": models.ReachabilityR5Corporate, "R5_CORPORATE_ONLY": models.ReachabilityR5Corporate,
	"CORPORATE_ONLY": models.ReachabilityR5Corporate, "GENERIC_CORPORATE": models.ReachabilityR5Corporate,
	"R0": models.ReachabilityR0None, "R0_NO_ACTIONABLE_ROUTE": models.ReachabilityR0None,
	"NO_ACTIONABLE_ROUTE": models.ReachabilityR0None, "NO_ROUTE": models.ReachabilityR0None,
	"BLOCKED": models.ReachabilityBlocked, "DNC": models.ReachabilityBlocked,
	"DO_NOT_CONTACT": models.ReachabilityBlocked, "SUPPRESSED": models.ReachabilityBlocked,
}

func MapRouteRelation(raw string) string {
	s := strings.ToUpper(strings.TrimSpace(raw))
	switch s {
	case "", models.RouteRelUnknown:
		return ""
	case models.RouteRelBelongsToNamedPerson, "DIRECT", "PERSONAL", "OWNED_BY_PERSON", "PERSON_OWNS_CHANNEL":
		return models.RouteRelBelongsToNamedPerson
	case models.RouteRelRoutesToNamedPerson, "SWITCHBOARD", "CORPORATE_PHONE", "RECEPTION":
		return models.RouteRelRoutesToNamedPerson
	case models.RouteRelRoleMailbox, "ROLE", "FUNCTIONAL", "ROUTES_TO_ROLE":
		return models.RouteRelRoleMailbox
	case models.RouteRelCorporateGeneric, "GENERIC", "INSTITUTIONAL", "ACCOUNT_LEVEL_ONLY":
		return models.RouteRelCorporateGeneric
	default:
		return s
	}
}

func routeStrength(class string) int {
	switch class {
	case models.ReachabilityR1Direct:
		return 5
	case models.ReachabilityR2Inferred, models.ReachabilityR3Routed:
		return 4
	case models.ReachabilityR4Role:
		return 2
	case models.ReachabilityR5Corporate:
		return 1
	default:
		return 0
	}
}

func whyNowStrength(conf string) int {
	switch strings.ToUpper(strings.TrimSpace(conf)) {
	case "HIGH":
		return 3
	case "MEDIUM":
		return 2
	case "LOW":
		return 1
	default:
		return 0
	}
}
