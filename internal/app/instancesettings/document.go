package instancesettings

import "time"

// Bounds on the invitation lifetime. An hour is the shortest window a person
// can realistically act in; 30 days is the longest a bearer link should live.
const (
	TTLHoursMin     = 1
	TTLHoursMax     = 720
	TTLHoursDefault = 168
)

// Invitations holds the invite-flow knobs.
type Invitations struct {
	// LinksEnabled exposes the copyable invite link. It is a bearer
	// credential, so a regulated deployment turns it off and relies on mail.
	LinksEnabled bool `json:"links_enabled"`
	// TTLHours is how long an invitation stays valid.
	TTLHours int `json:"ttl_hours"`
}

// Access holds the account-creation knobs.
type Access struct {
	// AllowInvitedSignup lets the invitee create their own account. Off means
	// an admin must create it for them.
	AllowInvitedSignup bool `json:"allow_invited_signup"`
}

// Document is the whole Tier B settings document. Every key here is one no
// environment variable owns, so there is no precedence to resolve.
type Document struct {
	Invitations Invitations `json:"invitations"`
	Access      Access      `json:"access"`
}

// Defaults is the document a fresh instance behaves as if it had.
func Defaults() Document {
	return Document{
		Invitations: Invitations{
			LinksEnabled: true,
			TTLHours:     TTLHoursDefault,
		},
		Access: Access{
			AllowInvitedSignup: true,
		},
	}
}

// Normalize clamps a document into its accepted range. It is applied on read
// as well as on write, so a row written by an older version still resolves.
func (d *Document) Normalize() {
	if d.Invitations.TTLHours <= 0 {
		d.Invitations.TTLHours = TTLHoursDefault
	}
	if d.Invitations.TTLHours < TTLHoursMin {
		d.Invitations.TTLHours = TTLHoursMin
	}
	if d.Invitations.TTLHours > TTLHoursMax {
		d.Invitations.TTLHours = TTLHoursMax
	}
}

// TTL is the invitation lifetime as a duration.
func (d Document) TTL() time.Duration {
	return time.Duration(d.Invitations.TTLHours) * time.Hour
}

// Patch is a partial update. Absent fields keep their stored value, so a
// client that does not know about a key cannot clear it.
type Patch struct {
	Invitations *struct {
		LinksEnabled *bool `json:"links_enabled"`
		TTLHours     *int  `json:"ttl_hours"`
	} `json:"invitations"`
	Access *struct {
		AllowInvitedSignup *bool `json:"allow_invited_signup"`
	} `json:"access"`
}

// Apply merges a patch onto a document.
func (p Patch) Apply(doc Document) Document {
	if p.Invitations != nil {
		if p.Invitations.LinksEnabled != nil {
			doc.Invitations.LinksEnabled = *p.Invitations.LinksEnabled
		}
		if p.Invitations.TTLHours != nil {
			// An explicit zero is a caller mistake, not "use the default": the
			// documented accepted range starts at one hour.
			doc.Invitations.TTLHours = *p.Invitations.TTLHours
			if doc.Invitations.TTLHours < TTLHoursMin {
				doc.Invitations.TTLHours = TTLHoursMin
			}
		}
	}
	if p.Access != nil && p.Access.AllowInvitedSignup != nil {
		doc.Access.AllowInvitedSignup = *p.Access.AllowInvitedSignup
	}
	return doc
}
