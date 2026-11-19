package partner

import (
	"context"
	"encoding/json"
	"errors"
	"net/mail"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/token"
	"github.com/warmbly/warmbly/internal/app/user"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
	"github.com/warmbly/warmbly/internal/pkg/displayname"
	"github.com/warmbly/warmbly/internal/repository"
)

// LoginTTL is how long a login link can be opened.
const LoginTTL = 60 * time.Second

const loginKeyPrefix = "partner_login:"

var ErrLoginInvalid = errx.NewWithIdentifier(errx.BadRequest, "partner_login_invalid",
	"This sign-in link has expired or was already used. Open it again from where you came from.")

// roleNames maps a partner role to the seeded role a member gets.
var roleNames = map[string]struct {
	seed string
	role models.Role
}{
	"owner":  {"Admin", models.RoleAdmin},
	"admin":  {"Admin", models.RoleAdmin},
	"member": {"Manager", models.RoleManager},
	"viewer": {"Viewer", models.RoleViewer},
}

func validatePerson(p *PersonInput) *errx.Error {
	p.ExternalID = strings.TrimSpace(p.ExternalID)
	if p.ExternalID == "" || len(p.ExternalID) > maxExternalID {
		return invalid("person.external_id is 1-255 characters.")
	}
	email, ok := contactEmail(p.Email)
	if !ok {
		return invalid("person.email is not a valid address.")
	}
	p.Email = email
	p.Role = strings.ToLower(strings.TrimSpace(p.Role))
	if p.Role == "" {
		p.Role = "member"
	}
	if _, ok := roleNames[p.Role]; !ok {
		return invalid("person.role is admin, member or viewer.")
	}
	return nil
}

// contactEmail parses the address a person is shown under. Optional; it is never a sign-in or a mail destination.
func contactEmail(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", true
	}
	addr, err := mail.ParseAddress(raw)
	if err != nil || len(addr.Address) < 3 || len(addr.Address) > 254 || models.InternalAddress(addr.Address) {
		return "", false
	}
	return strings.ToLower(addr.Address), true
}

// splitName turns a full name into the first and last names a member is shown under.
func splitName(name, email string) (string, string) {
	first, last, _ := strings.Cut(strings.TrimSpace(name), " ")
	first = displayname.CleanOr(first, displayname.Person, "")
	last = displayname.CleanOr(strings.TrimSpace(last), displayname.Person, "")
	if first == "" {
		first = displayname.FromEmail(email)
	}
	if first == "" {
		first = "Member"
	}
	return first, last
}

// ensurePerson finds or creates the account bound to the person and makes it a member with the partner's role.
// The account is matched on the partner's id for the person within the key's mode, never on the email address.
func (s *Service) ensurePerson(ctx context.Context, c *Caller, orgID uuid.UUID, in *PersonInput) (*models.PartnerIdentity, bool, *errx.Error) {
	p := c.Partner
	first, last := splitName(in.Name, in.Email)
	name := strings.TrimSpace(first + " " + last)
	id, err := s.Repo.GetPartnerIdentity(ctx, p.ID, c.Mode, in.ExternalID)
	if err != nil {
		return nil, false, internal(err)
	}
	if id == nil {
		id, err = s.Repo.CreatePartnerUser(ctx, p.ID, c.Mode, in.ExternalID, in.Email, name, first, last, personEmail(p.Slug))
		if errors.Is(err, repository.ErrPartnerIdentityExists) {
			id, err = s.Repo.GetPartnerIdentity(ctx, p.ID, c.Mode, in.ExternalID)
		}
		if err != nil || id == nil {
			return nil, false, internal(errors.Join(err, errors.New("partner identity not created")))
		}
	} else {
		id.Email, id.Name = in.Email, name
		if err := s.Repo.RefreshPartnerIdentity(ctx, id, first, last); err != nil {
			return nil, false, internal(err)
		}
		s.forgetUser(ctx, id.UserID)
	}
	changed, xerr := s.syncMember(ctx, p, orgID, id.UserID, in.Role)
	return id, changed, xerr
}

// forgetUser drops the cached /auth/me copy so a refreshed name and contact email show at once.
func (s *Service) forgetUser(ctx context.Context, userID uuid.UUID) {
	if s.Cache != nil {
		_ = s.Cache.Del(ctx, user.CacheKey(userID)).Err()
	}
}

// syncMember makes the account a member with the partner's role, the partner being the source of truth for it.
func (s *Service) syncMember(ctx context.Context, p *models.Partner, orgID, userID uuid.UUID, partnerRole string) (bool, *errx.Error) {
	if s.Orgs == nil {
		return false, nil
	}
	mapped := roleNames[partnerRole]
	roles, err := s.Orgs.ListRoles(ctx, orgID)
	if err != nil {
		return false, internal(err)
	}
	var roleID *uuid.UUID
	for i := range roles {
		if strings.EqualFold(roles[i].Name, mapped.seed) {
			roleID = &roles[i].ID
			break
		}
	}
	m, err := s.Orgs.GetMember(ctx, orgID, userID)
	if err != nil {
		return false, internal(err)
	}
	if m == nil {
		now := s.now()
		member := &models.OrganizationMember{
			ID: uuid.New(), OrganizationID: orgID, UserID: userID, Role: string(mapped.role),
			Permissions: models.GetRolePermissions(mapped.role), InvitedBy: &p.ServiceUserID, InvitedAt: now, AcceptedAt: &now,
			AccessScope: models.AccessScopeWorkspace,
		}
		if roleID != nil {
			err = s.Orgs.AddMemberWithRoles(ctx, member, []uuid.UUID{*roleID})
		} else {
			err = s.Orgs.AddMember(ctx, member)
		}
		if err != nil {
			return false, internal(err)
		}
		s.auditMember(ctx, p, orgID, models.AuditActionCreate, partnerRole)
		return true, nil
	}
	if m.IsOwner() {
		return false, nil
	}
	if roleID != nil {
		current, err := s.Orgs.GetMemberRoles(ctx, orgID, userID)
		if err != nil {
			return false, internal(err)
		}
		if len(current) == 1 && current[0].ID == *roleID {
			return false, nil
		}
		if err := s.Orgs.SetMemberRoles(ctx, orgID, userID, []uuid.UUID{*roleID}); err != nil {
			return false, internal(err)
		}
	} else {
		perms := models.GetRolePermissions(mapped.role)
		if m.Role == string(mapped.role) && m.Permissions == perms {
			return false, nil
		}
		m.Role, m.RoleID, m.Permissions = string(mapped.role), nil, perms
		if err := s.Orgs.UpdateMember(ctx, m); err != nil {
			return false, internal(err)
		}
	}
	s.auditMember(ctx, p, orgID, models.AuditActionUpdate, partnerRole)
	return true, nil
}

func (s *Service) auditMember(ctx context.Context, p *models.Partner, orgID uuid.UUID, action models.AuditAction, role string) {
	s.audit(ctx, p, orgID, action, models.AuditEntityOrganizationMember, map[string]string{"role": role})
}

// LoginInput is the POST /organizations/{external_id}/login-links body.
type LoginInput struct {
	Person   PersonInput `json:"person"`
	ReturnTo string      `json:"return_to"`
}

// LoginLink is a one-time sign-in URL.
type LoginLink struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}

type loginRecord struct {
	UserID    uuid.UUID `json:"user_id"`
	OrgID     uuid.UUID `json:"org_id"`
	PartnerID uuid.UUID `json:"partner_id"`
	Nonce     string    `json:"nonce"`
	ReturnTo  string    `json:"return_to"`
}

// CleanReturnTo accepts a path inside the app, or an absolute URL on one of the partner's allowed hosts.
func CleanReturnTo(raw string, allowedHosts []string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", true
	}
	if len(raw) > 2048 || strings.ContainsAny(raw, "\\\r\n\t") {
		return "", false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	if u.Scheme == "" && u.Host == "" && u.User == nil {
		if !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") {
			return "", false
		}
		return u.String(), true
	}
	if u.Scheme != "https" && !(config.DevEnv() && u.Scheme == "http") {
		return "", false
	}
	if u.User != nil || u.Host == "" || !slices.Contains(allowedHosts, strings.ToLower(u.Hostname())) {
		return "", false
	}
	return u.String(), true
}

// IssueLoginLink brings the person's account and membership up to date and returns a one-time link that signs them in.
func (s *Service) IssueLoginLink(ctx context.Context, c *Caller, externalID string, in LoginInput) (*LoginLink, *errx.Error) {
	if s.Sessions == nil || s.Codes == nil {
		return nil, errx.NewPublic(errx.ServiceUnavailable, "Sign-in links are not available on this instance.")
	}
	po, xerr := s.ResolveOrganization(ctx, c, externalID)
	if xerr != nil {
		return nil, xerr
	}
	if po.State == models.PartnerOrgCancelled {
		return nil, ErrOrgCancelled
	}
	if xerr := validatePerson(&in.Person); xerr != nil {
		return nil, xerr
	}
	returnTo, ok := CleanReturnTo(in.ReturnTo, c.Partner.AllowedHosts)
	if !ok {
		return nil, invalid("return_to is a path inside the app or a URL on one of the partner's allowed hosts.")
	}
	base := config.AppBaseURL()
	if base == "" {
		return nil, errx.NewPublic(errx.ServiceUnavailable, "APP_URL is not configured on this instance.")
	}
	orgID := *po.OrganizationID
	id, _, xerr := s.ensurePerson(ctx, c, orgID, &in.Person)
	if xerr != nil {
		return nil, xerr
	}
	linkID, nonce := uuid.New(), randomHex(16)
	now := s.now()
	exp := now.Add(LoginTTL)
	code, err := s.Sessions.GenerateTokenFor(token.PurposePartnerLogin, id.UserID, linkID, "", nonce, now, exp)
	if err != nil {
		return nil, internal(err)
	}
	payload, err := json.Marshal(loginRecord{UserID: id.UserID, OrgID: orgID, PartnerID: c.Partner.ID, Nonce: nonce, ReturnTo: returnTo})
	if err != nil {
		return nil, internal(err)
	}
	if err := s.Codes.Put(ctx, loginKeyPrefix+linkID.String(), payload, LoginTTL); err != nil {
		return nil, internal(err)
	}
	return &LoginLink{URL: base + "/auth/partner?code=" + url.QueryEscape(code), ExpiresAt: exp.UTC()}, nil
}

// LoginResult is a signed-in session and where to send the browser.
type LoginResult struct {
	*models.Token
	ReturnTo string `json:"return_to"`
}

// ExchangeLogin spends a login link once for a session in its workspace.
func (s *Service) ExchangeLogin(ctx context.Context, code, ipaddr, userAgent string) (*LoginResult, *errx.Error) {
	if s.Sessions == nil || s.Codes == nil || code == "" || len(code) > 2048 {
		return nil, ErrLoginInvalid
	}
	claims, xerr := s.Sessions.VerifyTokenFor(token.PurposePartnerLogin, code)
	if xerr != nil {
		return nil, ErrLoginInvalid
	}
	raw, err := s.Codes.Take(ctx, loginKeyPrefix+claims.SessionID.String())
	if err != nil {
		return nil, internal(err)
	}
	if len(raw) == 0 {
		return nil, ErrLoginInvalid
	}
	var rec loginRecord
	if err := json.Unmarshal(raw, &rec); err != nil || rec.UserID != claims.UserID || rec.Nonce == "" || rec.Nonce != claims.Nonce {
		return nil, ErrLoginInvalid
	}
	p, err := s.Repo.GetPartner(ctx, rec.PartnerID)
	if err != nil {
		return nil, internal(err)
	}
	if p == nil || p.Status != models.PartnerStatusActive {
		return nil, ErrLoginInvalid
	}
	po, _, err := s.Repo.GetPartnerOrgByOrganization(ctx, rec.OrgID)
	if err != nil {
		return nil, internal(err)
	}
	if po == nil || po.PartnerID != p.ID || po.State == models.PartnerOrgCancelled {
		return nil, ErrLoginInvalid
	}
	if s.Orgs != nil {
		m, err := s.Orgs.GetMember(ctx, rec.OrgID, rec.UserID)
		if err != nil {
			return nil, internal(err)
		}
		if m == nil {
			return nil, ErrLoginInvalid
		}
	}
	if s.Bans != nil {
		scope, err := s.Bans.GetBanState(ctx, rec.UserID)
		if err != nil {
			return nil, internal(err)
		}
		if models.BanScope(scope).Has(models.BanScopeLogin) {
			return nil, errx.ErrAccountSuspended
		}
	}
	orgID := rec.OrgID
	tok, xerr := s.Sessions.GenerateSessionWithOrg(ctx, rec.UserID, "", ipaddr, userAgent, token.AuthProviderPartner, &orgID)
	if xerr != nil {
		return nil, xerr
	}
	return &LoginResult{Token: tok, ReturnTo: rec.ReturnTo}, nil
}

// RemovePerson ends a person's membership in the workspace and every one of their sessions.
func (s *Service) RemovePerson(ctx context.Context, c *Caller, externalID, personExternalID string) *errx.Error {
	po, xerr := s.ResolveOrganization(ctx, c, externalID)
	if xerr != nil {
		return xerr
	}
	id, err := s.Repo.GetPartnerIdentity(ctx, c.Partner.ID, c.Mode, strings.TrimSpace(personExternalID))
	if err != nil {
		return internal(err)
	}
	if id == nil || s.Orgs == nil {
		return nil
	}
	orgID := *po.OrganizationID
	m, err := s.Orgs.GetMember(ctx, orgID, id.UserID)
	if err != nil {
		return internal(err)
	}
	if m != nil && !m.IsOwner() {
		if err := s.Orgs.RemoveMember(ctx, orgID, id.UserID); err != nil {
			return internal(err)
		}
		s.auditMember(ctx, c.Partner, orgID, models.AuditActionRemove, "")
	}
	if s.Sessions != nil {
		if xerr := s.Sessions.RevokeOtherSessions(ctx, id.UserID, uuid.Nil); xerr != nil {
			errs.CaptureException(xerr)
			return xerr
		}
	}
	return nil
}

// IsPartnerBound reports whether an account belongs to a partner (a person it signs in, or its service account).
// Such an account signs in only through the partner, so it cannot set a password or add another sign-in method.
func (s *Service) IsPartnerBound(ctx context.Context, userID uuid.UUID) (bool, error) {
	return s.Repo.IsPartnerBoundUser(ctx, userID)
}
