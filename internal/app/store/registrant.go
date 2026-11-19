package store

import (
	"net/mail"
	"regexp"
	"strings"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/pkg/displayname"
	"github.com/warmbly/warmbly/internal/pkg/graphkit"
)

// Registrant is the ICANN registrant a workspace's domains are registered under.
type Registrant struct {
	FirstName    string `json:"first_name"`
	LastName     string `json:"last_name"`
	Organization string `json:"organization"`
	Email        string `json:"email"`
	Phone        string `json:"phone"`
	Address1     string `json:"address1"`
	Address2     string `json:"address2"`
	City         string `json:"city"`
	State        string `json:"state"`
	PostalCode   string `json:"postal_code"`
	Country      string `json:"country"`
}

var (
	phoneRe   = regexp.MustCompile(`^\+[0-9]{1,3}\.[0-9]{4,14}$`)
	countryRe = regexp.MustCompile(`^[A-Z]{2}$`)
)

// ErrIDContactInvalid refuses a registrant; the response lists the fields.
const ErrIDContactInvalid = "contact_invalid"

// ContactError is a registrant refusal naming the offending fields; build it with NewContactError.
type ContactError struct {
	Fields []string
}

func (e *ContactError) Error() string { return "registrant invalid: " + strings.Join(e.Fields, ", ") }

// contactFields are the Registrant's json names in form order, each with the label a member reads.
var contactFields = []struct{ key, label string }{
	{"first_name", "first name"}, {"last_name", "last name"}, {"organization", "organization"}, {"email", "email"},
	{"phone", "phone"}, {"address1", "address"}, {"address2", "address line 2"}, {"city", "city"},
	{"state", "state or region"}, {"postal_code", "postal code"}, {"country", "country"},
}

// NewContactError keeps only the registrant fields this form has, in form order; anything else a refusal names is dropped.
func NewContactError(fields []string) *ContactError {
	named := map[string]bool{}
	for _, f := range fields {
		named[strings.TrimSpace(f)] = true
	}
	out := []string{}
	for _, f := range contactFields {
		if named[f.key] {
			out = append(out, f.key)
		}
	}
	return &ContactError{Fields: out}
}

// Message is the refusal in fixed words: the labels of the known fields, or a general ask when none is known.
func (e *ContactError) Message() string {
	var labels []string
	for _, f := range contactFields {
		for _, k := range e.Fields {
			if k == f.key {
				labels = append(labels, f.label)
			}
		}
	}
	if len(labels) == 0 {
		return "Check the domain owner's details and try again."
	}
	return "Check the domain owner's details: " + strings.Join(labels, ", ") + "."
}

// Normalize trims every field, upper-cases the country and folds the phone's separators.
func (r Registrant) Normalize() Registrant {
	t := strings.TrimSpace
	r.FirstName, r.LastName = displayname.Normalize(r.FirstName), displayname.Normalize(r.LastName)
	r.Organization = displayname.Normalize(r.Organization)
	r.Email = strings.ToLower(t(r.Email))
	r.Phone = strings.NewReplacer(" ", "", "-", "", "(", "", ")", "").Replace(t(r.Phone))
	r.Address1, r.Address2, r.City, r.State, r.PostalCode = t(r.Address1), t(r.Address2), t(r.City), t(r.State), t(r.PostalCode)
	r.Country = strings.ToUpper(t(r.Country))
	return r
}

// Validate returns the normalized registrant, or a ContactError naming every bad field.
func (r Registrant) Validate() (Registrant, error) {
	r = r.Normalize()
	var bad []string
	if _, rej := displayname.Check(r.FirstName, displayname.Person); rej != displayname.OK {
		bad = append(bad, "first_name")
	}
	if _, rej := displayname.Check(r.LastName, displayname.Person); rej != displayname.OK {
		bad = append(bad, "last_name")
	}
	if r.Organization != "" {
		if _, rej := displayname.Check(r.Organization, displayname.Workspace); rej != displayname.OK {
			bad = append(bad, "organization")
		}
	}
	if a, err := mail.ParseAddress(r.Email); err != nil || a.Address != r.Email {
		bad = append(bad, "email")
	}
	if !phoneRe.MatchString(r.Phone) {
		bad = append(bad, "phone")
	}
	for field, v := range map[string]string{"address1": r.Address1, "city": r.City, "postal_code": r.PostalCode} {
		if v == "" || len(v) > 128 {
			bad = append(bad, field)
		}
	}
	if len(r.Address2) > 128 {
		bad = append(bad, "address2")
	}
	if len(r.State) > 64 {
		bad = append(bad, "state")
	}
	if !countryRe.MatchString(r.Country) {
		bad = append(bad, "country")
	}
	if len(bad) > 0 {
		return r, NewContactError(bad)
	}
	return r, nil
}

func (r Registrant) contact() *graphkit.Contact {
	c := graphkit.Contact(r)
	return &c
}

func contactErr(fields []string) *errx.Error {
	ce := NewContactError(fields)
	e := errx.NewWithIdentifier(errx.BadRequest, ErrIDContactInvalid, ce.Message())
	e.Reason = strings.Join(ce.Fields, ",")
	return e
}

// BillingDetails is what Stripe holds about the workspace's billing contact.
type BillingDetails struct {
	Name       string
	Email      string
	Phone      string
	Line1      string
	Line2      string
	City       string
	State      string
	PostalCode string
	Country    string
}

// Prefill drafts a registrant from Stripe billing details and the signed-in
// user, for the buyer to confirm. Nothing here is validated.
func Prefill(b *BillingDetails, firstName, lastName, email, workspace string) Registrant {
	r := Registrant{FirstName: firstName, LastName: lastName, Email: email, Organization: workspace}
	if b != nil {
		if r.FirstName == "" && r.LastName == "" && b.Name != "" {
			first, last, _ := strings.Cut(strings.TrimSpace(b.Name), " ")
			r.FirstName, r.LastName = first, last
		}
		if b.Email != "" {
			r.Email = b.Email
		}
		r.Phone, r.Address1, r.Address2 = b.Phone, b.Line1, b.Line2
		r.City, r.State, r.PostalCode, r.Country = b.City, b.State, b.PostalCode, b.Country
	}
	return r.Normalize()
}
