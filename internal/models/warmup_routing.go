package models

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// WarmupRoutingMatchType enumerates the ways a routing rule can match a
// mailbox. 'any' is the wildcard; 'domain' is an exact-domain match;
// 'tld' is a top-level domain (com, io, org); 'provider' is a logical
// bucket (google, microsoft, yahoo, apple, custom) resolved from the
// address's domain.
type WarmupRoutingMatchType string

const (
	WarmupMatchAny      WarmupRoutingMatchType = "any"
	WarmupMatchDomain   WarmupRoutingMatchType = "domain"
	WarmupMatchTLD      WarmupRoutingMatchType = "tld"
	WarmupMatchProvider WarmupRoutingMatchType = "provider"
)

// WarmupProvider names the logical buckets a domain can map to.
// 'custom' covers vanity domains that do not match a known consumer
// or workspace provider.
type WarmupProvider string

const (
	ProviderGoogle    WarmupProvider = "google"
	ProviderMicrosoft WarmupProvider = "microsoft"
	ProviderYahoo     WarmupProvider = "yahoo"
	ProviderApple     WarmupProvider = "apple"
	ProviderProton    WarmupProvider = "proton"
	ProviderZoho      WarmupProvider = "zoho"
	ProviderCustom    WarmupProvider = "custom"
)

// WarmupRoutingRule is a customer-defined preference applied during
// premium-pool partner selection.
type WarmupRoutingRule struct {
	ID                  uuid.UUID              `json:"id"`
	OrganizationID      uuid.UUID              `json:"organization_id"`
	Name                string                 `json:"name"`
	Priority            int                    `json:"priority"`
	SenderMatchType     WarmupRoutingMatchType `json:"sender_match_type"`
	SenderMatchValue    string                 `json:"sender_match_value"`
	RecipientMatchType  WarmupRoutingMatchType `json:"recipient_match_type"`
	RecipientMatchValue string                 `json:"recipient_match_value"`
	Weight              float64                `json:"weight"`
	Enabled             bool                   `json:"enabled"`
	CreatedAt           time.Time              `json:"created_at"`
	UpdatedAt           time.Time              `json:"updated_at"`
}

// Matches reports whether the rule applies to a (sender, recipient) pair.
// Both sides must match; a rule with sender_match='any' applies regardless
// of sender domain.
func (r *WarmupRoutingRule) Matches(senderEmail, recipientEmail string) bool {
	if !r.Enabled {
		return false
	}
	return matchesSide(r.SenderMatchType, r.SenderMatchValue, senderEmail) &&
		matchesSide(r.RecipientMatchType, r.RecipientMatchValue, recipientEmail)
}

func matchesSide(matchType WarmupRoutingMatchType, value, email string) bool {
	domain := strings.ToLower(EmailDomain(email))
	if domain == "" {
		return matchType == WarmupMatchAny
	}
	target := strings.ToLower(strings.TrimSpace(value))
	switch matchType {
	case WarmupMatchAny:
		return true
	case WarmupMatchDomain:
		return domain == target
	case WarmupMatchTLD:
		return EmailTLD(domain) == target
	case WarmupMatchProvider:
		return string(ClassifyProvider(domain)) == target
	default:
		return false
	}
}

// EmailDomain extracts the lowercased domain from an email address.
// Returns "" if the address does not contain an '@'.
func EmailDomain(email string) string {
	at := strings.LastIndex(email, "@")
	if at < 0 || at == len(email)-1 {
		return ""
	}
	return strings.ToLower(email[at+1:])
}

// EmailTLD returns the top-level domain from a hostname. For "mail.gmail.com"
// it returns "com". Returns "" for empty input.
func EmailTLD(domain string) string {
	domain = strings.ToLower(strings.TrimSpace(domain))
	dot := strings.LastIndex(domain, ".")
	if dot < 0 || dot == len(domain)-1 {
		return ""
	}
	return domain[dot+1:]
}

// providerDomainMap maps known mailbox domains to their provider bucket.
// Workspace customers will typically be on a custom vanity domain — those
// fall through to ProviderCustom and rules can still target them via
// 'domain' match.
var providerDomainMap = map[string]WarmupProvider{
	"gmail.com":      ProviderGoogle,
	"googlemail.com": ProviderGoogle,
	"outlook.com":    ProviderMicrosoft,
	"hotmail.com":    ProviderMicrosoft,
	"live.com":       ProviderMicrosoft,
	"msn.com":        ProviderMicrosoft,
	"office365.com":  ProviderMicrosoft,
	"yahoo.com":      ProviderYahoo,
	"yahoo.co.uk":    ProviderYahoo,
	"ymail.com":      ProviderYahoo,
	"rocketmail.com": ProviderYahoo,
	"icloud.com":     ProviderApple,
	"me.com":         ProviderApple,
	"mac.com":        ProviderApple,
	"proton.me":      ProviderProton,
	"protonmail.com": ProviderProton,
	"pm.me":          ProviderProton,
	"zoho.com":       ProviderZoho,
	"zohomail.com":   ProviderZoho,
}

// ClassifyProvider maps a domain to a provider bucket. Unknown domains map
// to ProviderCustom — this is intentional: most B2B mailboxes are on
// custom domains hosted on Google Workspace or Microsoft 365, and we have
// no reliable way to distinguish them from MX records at this layer.
// Customers who want provider-aware routing for their custom domain should
// use a 'domain' match instead of 'provider'.
func ClassifyProvider(domain string) WarmupProvider {
	if p, ok := providerDomainMap[strings.ToLower(strings.TrimSpace(domain))]; ok {
		return p
	}
	return ProviderCustom
}
