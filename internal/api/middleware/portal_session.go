package middleware

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
)

// A session signed in on an agency's portal domain reaches that agency's client workspaces
// and its agency routes, and none of the account-level routes listed below.

const (
	// PortalAgencyKey holds the agency a portal session is bound to.
	PortalAgencyKey  = "portal_agency_id"
	portalClientsKey = "portal_clients"

	// CodePortalSessionRestricted answers anything a portal session may not reach.
	CodePortalSessionRestricted = "portal_session_restricted"
)

// ErrPortalSessionRestricted names the main address, where the refused change can be made.
func ErrPortalSessionRestricted() *errx.Error {
	where := "the main dashboard address"
	if origin := config.PrimaryDashboardOrigin(); origin != "" {
		where = origin
	}
	return errx.NewWithIdentifier(errx.Forbidden, CodePortalSessionRestricted,
		"You signed in on an agency portal, which reaches only that agency's client workspaces. Sign in at "+where+" to do this.")
}

// portalRoute is a route a portal session may not call: an empty method is every method, a trailing "/*" a prefix.
type portalRoute struct{ method, path string }

// portalRefusedRoutes is every account-level route: sign-in methods, sessions, deletion, credentials and the operator surface.
var portalRefusedRoutes = []portalRoute{
	{"POST", "/v1/auth/logout-all"},
	{"GET", "/v1/auth/sessions"},
	{"DELETE", "/v1/auth/sessions"},
	{"DELETE", "/v1/auth/sessions/:id"},
	{"POST", "/v1/auth/me/password"},
	{"POST", "/v1/auth/me/password/link"},
	{"GET", "/v1/auth/me/identities"},
	{"DELETE", "/v1/auth/me/identities/:provider"},
	{"GET", "/v1/auth/2fa/status"},
	{"POST", "/v1/auth/2fa/enroll/start"},
	{"POST", "/v1/auth/2fa/enroll/confirm"},
	{"DELETE", "/v1/auth/2fa"},
	{"POST", "/v1/auth/2fa/recovery-codes"},
	{"POST", "/v1/auth/passkey/register/begin"},
	{"POST", "/v1/auth/passkey/register/finish"},
	{"GET", "/v1/auth/passkey/credentials"},
	{"PATCH", "/v1/auth/passkey/credentials/:id"},
	{"DELETE", "/v1/auth/passkey/credentials/:id"},
	{"POST", "/v1/auth/reauth/passkey/begin"},
	{"POST", "/v1/auth/reauth/passkey/finish"},
	{"POST", "/v1/auth/reauth/sso/begin"},
	{"POST", "/v1/auth/reauth/sso/finish"},
	{"POST", "/v1/auth/reauth/native/begin"},
	{"POST", "/v1/auth/reauth/native"},
	{"POST", "/v1/auth/portal/handoff"},
	{"", "/v1/me/danger-zone/*"},
	{"POST", "/v1/organization"},
	{"POST", "/v1/agency"},
	{"POST", "/v1/api-keys"},
	{"PATCH", "/v1/api-keys/:id"},
	{"POST", "/v1/agency/:agencyId/api-keys"},
	{"PATCH", "/v1/agency/:agencyId/api-keys/:keyId"},
	{"", "/v1/oauth/applications/*"},
	{"", "/v1/oauth/application-logo/*"},
	{"", "/v1/oauth/authorize/*"},
	{"", "/v1/auth/cli/codes/*"},
	{"", "/v1/pool-link/codes/*"},
	{"POST", "/v1/cloud-link/connect"},
	{"POST", "/v1/cloud-link/connect/poll"},
	{"DELETE", "/v1/cloud-link"},
	{"GET", "/v1/cloud-link/workspace-mailboxes"},
	{"POST", "/v1/cloud-link/workspace-mailboxes/:id/adopt"},
	{"", "/admin/*"},
}

// portalWorkspaceParams are routes outside the agency group whose path names a workspace.
var portalWorkspaceParams = map[string]string{
	"/v1/organization/switch/:id":            "id",
	"/v1/organization/:orgId/limit-requests": "orgId",
}

// PortalRefusedRoutes lists the refused routes as method and path pairs, for the test that holds them to the router.
func PortalRefusedRoutes() [][2]string {
	out := make([][2]string, 0, len(portalRefusedRoutes))
	for _, r := range portalRefusedRoutes {
		out = append(out, [2]string{r.method, r.path})
	}
	return out
}

// PortalWorkspaceRoutes lists the routes whose path names a workspace, for the same test.
func PortalWorkspaceRoutes() []string {
	out := make([]string, 0, len(portalWorkspaceParams))
	for path := range portalWorkspaceParams {
		out = append(out, path)
	}
	return out
}

func portalRefuses(method, path string) bool {
	for _, r := range portalRefusedRoutes {
		if r.method != "" && r.method != method {
			continue
		}
		if prefix, ok := strings.CutSuffix(r.path, "/*"); ok {
			if path == prefix || strings.HasPrefix(path, prefix+"/") {
				return true
			}
			continue
		}
		if path == r.path {
			return true
		}
	}
	return false
}

// enforcePortalSession holds a portal session to its agency before any handler runs; false means it answered.
func (h *Handler) enforcePortalSession(c *gin.Context, session *models.Session) bool {
	if session == nil || session.PortalAgencyID == nil {
		return true
	}
	agency := *session.PortalAgencyID
	c.Set(PortalAgencyKey, agency)
	if h.PortalClients != nil {
		c.Set(portalClientsKey, h.PortalClients)
	}
	refuse := func() bool {
		errx.JSON(c, ErrPortalSessionRestricted())
		c.Abort()
		return false
	}
	path := c.FullPath()
	if portalRefuses(c.Request.Method, path) {
		return refuse()
	}
	if strings.HasPrefix(path, "/v1/agency/:agencyId") {
		if id, err := uuid.Parse(c.Param("agencyId")); err != nil || id != agency {
			return refuse()
		}
	}
	if name, ok := portalWorkspaceParams[path]; ok {
		id, err := uuid.Parse(c.Param(name))
		if err != nil {
			return refuse()
		}
		allowed, xerr := PortalAllowsWorkspace(c, id)
		if xerr != nil {
			errx.JSON(c, xerr)
			c.Abort()
			return false
		}
		if !allowed {
			return refuse()
		}
	}
	return true
}

// GetPortalAgencyID is the agency the request's session is bound to; nil for any other caller.
func GetPortalAgencyID(c *gin.Context) *uuid.UUID {
	if v, ok := c.Get(PortalAgencyKey); ok {
		if id, ok := v.(uuid.UUID); ok {
			return &id
		}
	}
	return nil
}

// PortalAllowsWorkspace reports whether the caller may reach orgID: always for a normal caller,
// only a current client of the bound agency for a portal session.
func PortalAllowsWorkspace(c *gin.Context, orgID uuid.UUID) (bool, *errx.Error) {
	agency := GetPortalAgencyID(c)
	if agency == nil {
		return true, nil
	}
	clients, _ := c.Get(portalClientsKey)
	lookup, ok := clients.(PortalClients)
	if !ok || lookup == nil {
		return false, nil
	}
	holder, err := lookup.ClientAgency(c.Request.Context(), orgID)
	if err != nil {
		errs.CaptureException(err)
		return false, errx.InternalError()
	}
	return holder != nil && *holder == *agency, nil
}

// RefuseOutsidePortal answers portal_session_restricted when a portal session names any workspace outside its agency.
func RefuseOutsidePortal(c *gin.Context, orgIDs ...uuid.UUID) *errx.Error {
	for _, id := range orgIDs {
		ok, xerr := PortalAllowsWorkspace(c, id)
		if xerr != nil {
			return xerr
		}
		if !ok {
			return ErrPortalSessionRestricted()
		}
	}
	return nil
}

// PortalClients answers which agency holds a workspace as a client; nil when none.
type PortalClients interface {
	ClientAgency(ctx context.Context, orgID uuid.UUID) (*uuid.UUID, error)
}

// AgencyClientReader reads one client row; satisfied by repository.AgencyRepository.
type AgencyClientReader interface {
	GetClient(ctx context.Context, orgID uuid.UUID) (*models.AgencyClient, error)
}

// portalClientTTL bounds how long a detached client stays reachable from a portal session.
const portalClientTTL = 10 * time.Second

type portalClientEntry struct {
	agency  *uuid.UUID
	expires time.Time
}

// PortalClientCache is a briefly cached PortalClients over the agency client table.
type PortalClientCache struct {
	src AgencyClientReader
	now func() time.Time

	mu      sync.Mutex
	entries map[uuid.UUID]portalClientEntry
}

func NewPortalClientCache(src AgencyClientReader) *PortalClientCache {
	return &PortalClientCache{src: src, now: time.Now, entries: map[uuid.UUID]portalClientEntry{}}
}

func (p *PortalClientCache) ClientAgency(ctx context.Context, orgID uuid.UUID) (*uuid.UUID, error) {
	now := p.now()
	p.mu.Lock()
	if e, ok := p.entries[orgID]; ok && now.Before(e.expires) {
		p.mu.Unlock()
		return e.agency, nil
	}
	p.mu.Unlock()
	client, err := p.src.GetClient(ctx, orgID)
	if err != nil {
		return nil, err
	}
	var agency *uuid.UUID
	if client != nil {
		id := client.AgencyID
		agency = &id
	}
	p.mu.Lock()
	if len(p.entries) >= 4096 {
		p.entries = map[uuid.UUID]portalClientEntry{}
	}
	p.entries[orgID] = portalClientEntry{agency: agency, expires: now.Add(portalClientTTL)}
	p.mu.Unlock()
	return agency, nil
}
