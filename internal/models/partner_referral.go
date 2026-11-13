package models

import (
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

// PartnerReferral is the partner-program click a workspace signed up through.
type PartnerReferral struct {
	OrganizationID uuid.UUID
	DubClickID     string
	// LeadReportedAt is nil until Dub has accepted the signup.
	LeadReportedAt *time.Time
}

// dubClickIDPattern is the shape of a Dub click id (a short nanoid); anything
// else arrived on a query string a stranger wrote and is dropped.
var dubClickIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// NormalizeDubClickID returns the id when it looks like one Dub issued, else "".
func NormalizeDubClickID(v string) string {
	v = strings.TrimSpace(v)
	if !dubClickIDPattern.MatchString(v) {
		return ""
	}
	return v
}
