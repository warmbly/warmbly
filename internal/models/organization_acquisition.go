package models

import (
	"net/url"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

// acquisitionFieldMax bounds every captured value. These arrive from a query
// string a stranger controls, so they are clamped before they reach the
// database rather than trusted because a form was involved.
const acquisitionFieldMax = 255

// OrgAcquisition is where a workspace came from, recorded once at signup and
// never updated. It is first-party on purpose: the analytics provider answers
// "which page converts" for a day, this answers "which channel pays" for as
// long as the account exists.
type OrgAcquisition struct {
	OrganizationID uuid.UUID `json:"organization_id"`
	LandingPath    string    `json:"landing_path,omitempty"`
	ReferrerHost   string    `json:"referrer_host,omitempty"`
	UTMSource      string    `json:"utm_source,omitempty"`
	UTMMedium      string    `json:"utm_medium,omitempty"`
	UTMCampaign    string    `json:"utm_campaign,omitempty"`
	UTMTerm        string    `json:"utm_term,omitempty"`
	UTMContent     string    `json:"utm_content,omitempty"`
	// PartnerClickID is the partner-program click the signup carried. It rides
	// the signup to the new workspace and is stored apart from the channel
	// (organization_partner_referrals), never in organization_acquisition.
	PartnerClickID string `json:"partner_click_id,omitempty"`
}

// Empty reports whether the signup carried no attribution at all, which is the
// normal case for someone who typed the dashboard's address. Nothing is stored
// for an empty record.
func (a OrgAcquisition) Empty() bool {
	return a.channelEmpty() && a.PartnerClickID == ""
}

// Channel is the record without the partner click: what organization_acquisition holds.
func (a OrgAcquisition) Channel() OrgAcquisition {
	a.PartnerClickID = ""
	return a
}

func (a OrgAcquisition) channelEmpty() bool {
	return a.LandingPath == "" && a.ReferrerHost == "" &&
		a.UTMSource == "" && a.UTMMedium == "" && a.UTMCampaign == "" &&
		a.UTMTerm == "" && a.UTMContent == ""
}

// Normalize clamps every field to what is safe to store: the landing path is
// reduced to a path (so a full URL, with whatever query string it carried,
// cannot smuggle anything in), the referrer to a bare host, and everything is
// trimmed and truncated.
func (a OrgAcquisition) Normalize() OrgAcquisition {
	return OrgAcquisition{
		OrganizationID: a.OrganizationID,
		LandingPath:    clampField(normalizePath(a.LandingPath)),
		ReferrerHost:   clampField(normalizeHost(a.ReferrerHost)),
		UTMSource:      clampField(a.UTMSource),
		UTMMedium:      clampField(a.UTMMedium),
		UTMCampaign:    clampField(a.UTMCampaign),
		UTMTerm:        clampField(a.UTMTerm),
		UTMContent:     clampField(a.UTMContent),
		PartnerClickID: NormalizeDubClickID(a.PartnerClickID),
	}
}

// emailPattern is the one identifier worth catching by shape. These values
// arrive on a query string anybody can write, and an address in a utm_source
// would be stored on the organization and sent as an analytics property,
// breaking the rule that no event names a person.
//
// It is a shape check, not a PII scrubber: it cannot catch a name or an
// account number, and it is not meant to. The defence that carries the weight
// is that these fields are never joined to anything and are only ever read as
// a channel name.
var emailPattern = regexp.MustCompile(`[\w.+-]+@[\w-]+\.[\w.-]+`)

func clampField(v string) string {
	v = strings.TrimSpace(v)
	// Redact before truncating, so a value cut mid-address cannot leave a
	// recognisable fragment behind.
	v = emailPattern.ReplaceAllString(v, "[redacted]")
	if len(v) > acquisitionFieldMax {
		v = v[:acquisitionFieldMax]
	}
	// A control character in a value that ends up in a CSV export or a log line
	// is never wanted, and no legitimate UTM value has one.
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, v)
}

// normalizePath keeps the path and drops everything else, so "/pricing" and
// "https://warmbly.com/pricing?utm_source=x" both record "/pricing".
//
// The result must be absolute. url.Parse accepts a bare word as a relative
// path, so the check is on what comes out, not on what went in.
func normalizePath(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if u, err := url.Parse(v); err == nil && strings.HasPrefix(u.Path, "/") {
		return u.Path
	}
	return ""
}

// hostPattern is what a stored referrer must look like once reduced. Anything
// else is dropped rather than stored: the field's whole purpose is to name a
// site, so a value that is not a hostname has nothing useful in it and might
// have somebody's data in it.
var hostPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)

// normalizeHost keeps a bare hostname: no scheme, no path, no port, no query,
// no fragment, lowercase.
//
// A full referrer URL carries the referring page's own query string, which is
// somebody else's data and not ours to store. url.Parse does not help on its
// own: it reads "example.com?u=alice@example.com" as a relative path with an
// empty Hostname, so the reduction below runs on the raw string and the result
// is then checked against hostPattern rather than trusted.
func normalizeHost(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if u, err := url.Parse(v); err == nil && u.Hostname() != "" {
		return keepHost(u.Hostname())
	}
	// Bare host, possibly with a scheme, path, port, query or fragment glued on.
	v = strings.TrimPrefix(strings.TrimPrefix(v, "https://"), "http://")
	for _, sep := range []string{"/", "?", "#", "@", ":"} {
		v, _, _ = strings.Cut(v, sep)
	}
	return keepHost(v)
}

func keepHost(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	if !hostPattern.MatchString(v) {
		return ""
	}
	return v
}
