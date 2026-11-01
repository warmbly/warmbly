package companyenrich

import (
	"context"
	"encoding/json"
	"net"
	"net/mail"
	"net/url"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/displayname"
)

// EnrichContact uses a stored contact's email or company domain for one lookup,
// then writes profile fields only. Consent, address, verification, campaigns,
// categories and segment membership are never part of the update.
func (s *Service) EnrichContact(ctx context.Context, orgID, connID, userID uuid.UUID, in models.CompanyEnrichContactRequest) (*models.CompanyEnrichContactResult, error) {
	if in.ContactID == uuid.Nil || userID == uuid.Nil || (in.Kind != "person" && in.Kind != "company") {
		return nil, invalid("contact_id and kind person or company are required")
	}
	cl, err := s.connection(ctx, orgID, connID)
	if err != nil {
		return nil, err
	}
	c, err := s.contact(ctx, orgID, in.ContactID)
	if err != nil {
		return nil, err
	}
	request, err := contactQuery(*c, in.Kind)
	if err != nil {
		return nil, err
	}
	result, err := cl.Query(ctx, request)
	s.noteHealth(ctx, connID, err)
	if err != nil {
		return nil, publicError(err)
	}
	obj, ok := result.Data.(map[string]any)
	if !ok {
		return nil, publicError(ErrResponse)
	}
	// Re-read after the external call so the blank-only policy uses current
	// values, and a deleted/moved contact never reaches the write.
	latest, err := s.contact(ctx, orgID, in.ContactID)
	if err != nil {
		return nil, err
	}
	currentQuery, err := contactQuery(*latest, in.Kind)
	if err != nil {
		return nil, err
	}
	original, _ := json.Marshal(request)
	current, _ := json.Marshal(currentQuery)
	if string(original) != string(current) {
		return nil, errx.NewWithIdentifier(errx.Conflict, "companyenrich_contact_changed", "The contact's lookup identity changed during enrichment; no profile fields were saved")
	}
	patch, fields := profilePatch(*latest, obj, in.Kind, in.Overwrite)
	out := &models.CompanyEnrichContactResult{CompanyEnrichResult: *result, Contact: latest, UpdatedFields: fields}
	if len(fields) == 0 {
		return out, nil
	}
	if xerr := s.Contacts.ApplyCompanyEnrichProfile(ctx, orgID, in.ContactID, latest.UpdatedAt, latest.Email, patch); xerr != nil {
		if xerr.Code == errx.Conflict {
			return nil, xerr
		}
		return nil, errx.NewWithIdentifier(errx.Unprocessable, "companyenrich_save_failed", "The CompanyEnrich lookup completed but the profile was not saved. Check the contact before making another paid request")
	}
	// Read the complete contact after the guarded profile-only write.
	if out.Contact, err = s.contact(ctx, orgID, in.ContactID); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Service) contact(ctx context.Context, orgID, id uuid.UUID) (*models.Contact, error) {
	if s.Contacts == nil {
		return nil, errx.New(errx.ServiceUnavailable, "Contact enrichment is not available on this instance")
	}
	rows, xerr := s.Contacts.GetByIDsAndOrganization(ctx, orgID, []uuid.UUID{id})
	if xerr != nil {
		return nil, xerr
	}
	for i := range rows {
		if rows[i].ID == id {
			return &rows[i], nil
		}
	}
	return nil, errx.New(errx.NotFound, "contact not found")
}

func contactQuery(c models.Contact, kind string) (models.CompanyEnrichQuery, error) {
	if kind == "person" {
		address := strings.TrimSpace(c.Email)
		parsed, err := mail.ParseAddress(address)
		if err != nil || parsed.Address != address {
			return models.CompanyEnrichQuery{}, invalid("the contact must have a valid email address")
		}
		return models.CompanyEnrichQuery{Operation: "lookup_person", Body: map[string]any{"email": address}}, nil
	}
	for _, key := range []string{"company_domain", "companyenrich_domain", "website"} {
		if raw := strings.TrimSpace(c.CustomFields[key]); raw != "" {
			if domain := companyDomain(raw); domain != "" {
				return models.CompanyEnrichQuery{Operation: "enrich_company", Query: map[string]any{"domain": domain}}, nil
			}
			return models.CompanyEnrichQuery{}, invalid("the contact's company domain or website is invalid")
		}
	}
	if at := strings.LastIndex(c.Email, "@"); at >= 0 {
		if domain := companyDomain(c.Email[at+1:]); domain != "" {
			return models.CompanyEnrichQuery{Operation: "enrich_company", Query: map[string]any{"domain": domain}}, nil
		}
	}
	return models.CompanyEnrichQuery{}, invalid("the contact needs a company_domain, companyenrich_domain, website, or email domain")
}

func companyDomain(raw string) string {
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.Port() != "" {
		return ""
	}
	d := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if !strings.Contains(d, ".") || net.ParseIP(d) != nil || len(d) > 253 || strings.ContainsAny(d, " \t\r\n") {
		return ""
	}
	for _, label := range strings.Split(d, ".") {
		if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return ""
		}
	}
	return d
}

func profilePatch(c models.Contact, profile map[string]any, kind string, overwrite bool) (*models.CompanyEnrichProfilePatch, []string) {
	patch := &models.CompanyEnrichProfilePatch{}
	fields := []string{}
	custom := map[string]string{}
	put := func(name, existing, value string, target **string) {
		value = bounded(value, 500)
		if value == "" || value == existing || (!overwrite && strings.TrimSpace(existing) != "") {
			return
		}
		*target = &value
		fields = append(fields, name)
	}
	putCustom := func(name, value string) {
		value = bounded(value, 500)
		if value == "" || value == c.CustomFields[name] || (!overwrite && strings.TrimSpace(c.CustomFields[name]) != "") {
			return
		}
		custom[name] = value
		fields = append(fields, "custom_fields."+name)
	}
	if kind == "person" {
		put("first_name", c.FirstName, displayname.Clean(textField(profile, "first_name"), displayname.Person), &patch.FirstName)
		put("last_name", c.LastName, displayname.Clean(textField(profile, "last_name"), displayname.Person), &patch.LastName)
		putCustom("companyenrich_person_id", textField(profile, "id"))
		for _, name := range []string{"position", "seniority", "department"} {
			putCustom("companyenrich_"+name, textField(profile, name))
		}
		if socials, ok := profile["socials"].(map[string]any); ok {
			putCustom("companyenrich_person_linkedin_url", safeProfileURL(textField(socials, "linkedin_url")))
		}
		if experiences, ok := profile["experiences"].([]any); ok {
			for _, item := range experiences {
				exp, ok := item.(map[string]any)
				if !ok || exp["isCurrent"] != true {
					continue
				}
				if company, ok := exp["company"].(map[string]any); ok {
					put("company", c.Company, displayname.Clean(textField(company, "name"), displayname.Workspace), &patch.Company)
					putCustom("companyenrich_company_id", textField(company, "id"))
					putCustom("companyenrich_domain", companyDomain(textField(company, "domain")))
					break
				}
			}
		}
	} else {
		put("company", c.Company, displayname.Clean(textField(profile, "name"), displayname.Workspace), &patch.Company)
		putCustom("companyenrich_company_id", textField(profile, "id"))
		putCustom("companyenrich_domain", companyDomain(textField(profile, "domain")))
		putCustom("companyenrich_website", safeProfileURL(textField(profile, "website")))
		for _, name := range []string{"industry", "employees", "revenue", "founded_year"} {
			putCustom("companyenrich_"+name, textField(profile, name))
		}
		if socials, ok := profile["socials"].(map[string]any); ok {
			putCustom("companyenrich_company_linkedin_url", safeProfileURL(textField(socials, "linkedin_url")))
		}
	}
	if len(custom) > 0 {
		patch.CustomFields = custom
	}
	sort.Strings(fields)
	return patch, fields
}

func textField(obj map[string]any, key string) string {
	switch v := obj[key].(type) {
	case string:
		return v
	case json.Number:
		return v.String()
	}
	return ""
}

func bounded(s string, limit int) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > limit {
		return string([]rune(s)[:limit])
	}
	return s
}

func safeProfileURL(s string) string {
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil {
		return ""
	}
	return u.String()
}
