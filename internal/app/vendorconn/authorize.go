package vendorconn

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/app/mailboximport"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/mailvendor"
)

// DomainAuthorization is the import's view of one domain's authorization.
type DomainAuthorization = mailboximport.VendorAuthorization

// authState is one domain's authorization, shared by every row and replica through Redis.
type authState struct {
	RequestID string    `json:"request_id,omitempty"`
	Failed    string    `json:"failed,omitempty"`
	Started   time.Time `json:"started,omitempty"`
	Completed time.Time `json:"completed,omitempty"`
}

const (
	authStartTTL   = 2 * time.Minute
	authPendingTTL = 24 * time.Hour
	authFailedTTL  = time.Hour
	// authSettle is how long a completed consent may take to reach the provider's token service.
	authSettle = 15 * time.Minute
	// authMaxWait bounds one request, so no row waits on a vendor forever.
	authMaxWait = 2 * time.Hour
	listTTL     = time.Minute
)

func authKey(orgID, connectionID uuid.UUID, provider, domain string) string {
	return "vendor_app_auth:" + orgID.String() + ":" + connectionID.String() + ":" + provider + ":" + domain
}

// AuthorizeDomain has the vendor authorize this instance's app on a mailbox's
// domain and records the grant once it is done; call again while Pending.
func (s *Service) AuthorizeDomain(ctx context.Context, orgID, userID, connectionID uuid.UUID, email, provider string) DomainAuthorization {
	if s.grants == nil || s.cache == nil {
		return DomainAuthorization{}
	}
	at := strings.LastIndex(email, "@")
	if at < 0 || (provider != models.GrantProviderGoogle && provider != models.GrantProviderMicrosoft) {
		return DomainAuthorization{}
	}
	domain := strings.ToLower(email[at+1:])
	if g, err := s.grants.GrantFor(ctx, orgID, provider, domain); err == nil && g != nil {
		return DomainAuthorization{GrantID: &g.ID}
	}
	clientID, scopes, roles, ok := s.grants.AppIdentity(provider)
	if !ok {
		return DomainAuthorization{}
	}
	c, xerr := s.get(ctx, orgID, connectionID)
	if xerr != nil {
		return DomainAuthorization{Pending: xerr.Code == errx.Internal}
	}
	client, xerr := s.client(ctx, c)
	if xerr != nil {
		if xerr.Code == errx.Internal {
			return DomainAuthorization{Pending: true}
		}
		return DomainAuthorization{Message: xerr.Message}
	}
	az, ok := client.(mailvendor.AppAuthorizer)
	if !ok {
		return DomainAuthorization{}
	}
	label := labelOf(c.Vendor)
	key := authKey(orgID, connectionID, provider, domain)

	var st authState
	if err := s.cache.GetJSON(ctx, key, &st); err == nil {
		switch {
		case st.Failed != "":
			return DomainAuthorization{Message: st.Failed}
		case st.RequestID == "":
			return DomainAuthorization{Pending: true}
		}
		status, err := az.AuthorizationStatus(ctx, st.RequestID)
		switch {
		case err != nil && transient(err):
			status = mailvendor.AuthorizationStatus{State: mailvendor.AuthorizationPending}
		case errors.Is(err, mailvendor.ErrUnauthorized):
			return DomainAuthorization{Message: s.failed(ctx, c, err).Message}
		case err != nil:
			status = mailvendor.AuthorizationStatus{State: mailvendor.AuthorizationFailed, Reason: "the request is gone"}
		}
		if status.State == mailvendor.AuthorizationPending && time.Since(st.Started) > authMaxWait {
			status = mailvendor.AuthorizationStatus{State: mailvendor.AuthorizationFailed, Reason: "it did not finish within two hours"}
		}
		switch status.State {
		case mailvendor.AuthorizationPending:
			return DomainAuthorization{Pending: true}
		case mailvendor.AuthorizationFailed:
			msg := label + " could not authorize Warmbly on " + domain + reasonSuffix(status.Reason) + ". Sign in on each mailbox instead, or retry these rows once it is fixed."
			_ = s.cache.SetJSON(ctx, key, authState{Failed: msg}, authFailedTTL)
			return DomainAuthorization{Message: msg}
		}
		if st.Completed.IsZero() {
			st.Completed = time.Now()
			_ = s.cache.SetJSON(ctx, key, st, authPendingTTL)
		}
		return s.recordGrant(ctx, c, client, key, orgID, userID, provider, domain, time.Since(st.Completed) < authSettle)
	}

	started, err := s.cache.SetNX(ctx, key, "{}", authStartTTL).Result()
	if err != nil {
		return DomainAuthorization{}
	}
	if !started {
		return DomainAuthorization{Pending: true}
	}
	list, err := s.listed(ctx, c, client)
	if err != nil {
		_ = s.cache.Del(ctx, key)
		switch {
		case transient(err):
			return DomainAuthorization{Pending: true}
		case errors.Is(err, mailvendor.ErrUnauthorized):
			return DomainAuthorization{Message: s.failed(ctx, c, err).Message}
		}
		return DomainAuthorization{}
	}
	// The admin mailbox names the workspace InboxKit resolves the domain's admin in.
	var on *mailvendor.Mailbox
	admin := false
	for i := range list {
		if strings.EqualFold(list[i].Domain, domain) || strings.HasSuffix(strings.ToLower(list[i].Email), "@"+domain) {
			if on == nil || (list[i].Admin && !admin) {
				on = &list[i]
			}
			admin = admin || list[i].Admin
		}
	}
	// A Google grant is verified as the domain's administrator, so without one there is nothing to verify.
	if on == nil || (provider == models.GrantProviderGoogle && !admin) {
		_ = s.cache.Del(ctx, key)
		return DomainAuthorization{}
	}
	reqID, err := az.AuthorizeApp(ctx, mailvendor.AppAuthorization{
		Domain: domain, Mailbox: *on, Provider: provider, ClientID: clientID, Scopes: scopes, AppRoles: roles,
	})
	if err != nil {
		if transient(err) {
			_ = s.cache.Del(ctx, key)
			return DomainAuthorization{Pending: true}
		}
		if errors.Is(err, mailvendor.ErrUnauthorized) {
			_ = s.failed(ctx, c, err)
		}
		log.Info().Str("vendor", c.Vendor).Str("domain", domain).Err(err).Msg("vendor connection: app authorization refused")
		msg := label + " could not authorize Warmbly on " + domain + ". It needs an admin mailbox on the domain. Sign in on each mailbox instead."
		_ = s.cache.SetJSON(ctx, key, authState{Failed: msg}, authFailedTTL)
		return DomainAuthorization{Message: msg}
	}
	_ = s.cache.SetJSON(ctx, key, authState{RequestID: reqID, Started: time.Now()}, authPendingTTL)
	return DomainAuthorization{Pending: true}
}

// recordGrant turns a completed vendor authorization into the workspace's grant,
// covering only domains this vendor account holds.
// settling keeps a refusal pending while a fresh consent propagates.
func (s *Service) recordGrant(ctx context.Context, c *models.VendorConnection, client mailvendor.Client, key string, orgID, userID uuid.UUID, provider, domain string, settling bool) DomainAuthorization {
	list, err := s.listed(ctx, c, client)
	if err != nil {
		return DomainAuthorization{Pending: true}
	}
	owned := map[string]bool{}
	admin := ""
	for _, m := range list {
		d := strings.ToLower(m.Domain)
		if at := strings.LastIndex(m.Email, "@"); d == "" && at > 0 {
			d = strings.ToLower(m.Email[at+1:])
		}
		if d != "" {
			owned[d] = true
		}
		if m.Admin && d == domain && admin == "" {
			admin = m.Email
		}
	}
	domains := make([]string, 0, len(owned))
	for d := range owned {
		domains = append(domains, d)
	}
	g, xerr := s.grants.GrantFromVendor(ctx, orgID, userID, provider, domain, admin, domains)
	if xerr != nil {
		if settling || xerr.Code == errx.Internal || xerr.ResponseCode() == "mailbox_grant_unavailable" {
			return DomainAuthorization{Pending: true}
		}
		msg := labelOf(c.Vendor) + " authorized Warmbly on " + domain + ", but the grant did not verify: " + xerr.Message
		_ = s.cache.SetJSON(ctx, key, authState{Failed: msg}, authFailedTTL)
		return DomainAuthorization{Message: msg}
	}
	_ = s.cache.Del(ctx, key)
	return DomainAuthorization{GrantID: &g.ID}
}

// listed is the connection's mailbox list, shared a minute so every domain of an import reads it once.
func (s *Service) listed(ctx context.Context, c *models.VendorConnection, client mailvendor.Client) ([]mailvendor.Mailbox, error) {
	if v, ok := s.lists.Load(c.ID); ok {
		if l := v.(cachedList); time.Since(l.at) < listTTL {
			return l.boxes, nil
		}
	}
	boxes, err := client.List(ctx)
	if err != nil {
		return nil, err
	}
	s.lists.Store(c.ID, cachedList{boxes: boxes, at: time.Now()})
	return boxes, nil
}

type cachedList struct {
	boxes []mailvendor.Mailbox
	at    time.Time
}

// transient is a vendor failure worth trying again: nothing was decided.
func transient(err error) bool {
	if errors.Is(err, mailvendor.ErrRateLimited) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	var ve *mailvendor.Error
	return errors.As(err, &ve) && (ve.Status == 0 || ve.Status >= 500)
}

func reasonSuffix(reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return ""
	}
	if r := []rune(reason); len(r) > 200 {
		reason = string(r[:200])
	}
	return " (" + strings.TrimRight(reason, ".") + ")"
}
