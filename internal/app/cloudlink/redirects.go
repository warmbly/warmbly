package cloudlink

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// offerTTL keeps the dashboard's redirect choice from calling Cloud on every render.
const offerTTL = time.Minute

type cachedOffer struct {
	offer *models.PoolLinkRedirectOffer
	at    time.Time
}

// rememberOffer keeps Cloud's answer; a Cloud that sends none does not serve redirects.
func (s *service) rememberOffer(instanceID uuid.UUID, o *models.PoolLinkRedirectOffer) {
	if o == nil {
		o = &models.PoolLinkRedirectOffer{}
	}
	s.mu.Lock()
	if s.offers == nil {
		s.offers = map[uuid.UUID]cachedOffer{}
	}
	s.offers[instanceID] = cachedOffer{offer: o, at: time.Now()}
	s.mu.Unlock()
}

func (s *service) forgetOffer(instanceID uuid.UUID) {
	s.mu.Lock()
	delete(s.offers, instanceID)
	s.mu.Unlock()
}

func (s *service) OnDisconnect(fn func(context.Context, uuid.UUID)) {
	s.disconnected = append(s.disconnected, fn)
}

// RedirectOffer is Cloud's offer and whether the instance is linked; an unreachable Cloud keeps the last answer, nil before any.
func (s *service) RedirectOffer(ctx context.Context, orgID uuid.UUID) (*models.PoolLinkRedirectOffer, bool) {
	l, err := s.repo.Get(ctx, &orgID)
	if err != nil {
		return nil, true // unknown reads as unreachable, never as "not linked"
	}
	if l == nil {
		return nil, false
	}
	fresh := func() (*models.PoolLinkRedirectOffer, bool) {
		s.mu.Lock()
		defer s.mu.Unlock()
		offer := s.offers[l.InstanceID]
		return offer.offer, !offer.at.IsZero() && time.Since(offer.at) < offerTTL
	}
	if o, ok := fresh(); ok {
		return o, true
	}
	s.offerFetch.Lock()
	defer s.offerFetch.Unlock()
	if o, ok := fresh(); ok {
		return o, true
	}
	cached, _ := fresh()
	var info models.PoolLinkInstanceInfo
	if xerr := s.clientFor(l).do(ctx, http.MethodGet, "/instance", nil, &info); xerr != nil {
		return cached, true
	}
	s.rememberOffer(l.InstanceID, info.Redirects)
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.offers[l.InstanceID].offer, true
}

func (s *service) ListRedirects(ctx context.Context) ([]models.DomainRedirect, *errx.Error) {
	links, err := s.repo.ListLinks(ctx)
	if err != nil {
		return nil, errx.InternalError()
	}
	var rows []models.DomainRedirect
	for _, l := range links {
		var out struct {
			Data []models.DomainRedirect `json:"data"`
		}
		if xerr := s.clientFor(&l).do(ctx, http.MethodGet, "/instance/redirects", nil, &out); xerr != nil {
			log.Warn().Str("instance_id", l.InstanceID.String()).Str("code", xerr.ResponseCode()).Msg("cloud link: redirect listing failed")
			continue
		}
		for i := range out.Data {
			out.Data[i].CloudLinkInstanceID = &l.InstanceID
		}
		rows = append(rows, out.Data...)
	}
	return rows, nil
}

func (s *service) redirectLink(ctx context.Context, orgID uuid.UUID, domain string) (*models.CloudLink, *errx.Error) {
	l, err := s.repo.GetForRedirect(ctx, orgID, domain)
	if err != nil {
		return nil, errx.InternalError()
	}
	if l == nil {
		return nil, ErrNotConnected
	}
	return l, nil
}

func redirectPath(domain string) string { return "/instance/redirects/" + url.PathEscape(domain) }

func (s *service) redirectCall(ctx context.Context, orgID uuid.UUID, domain, method, path string, body any) (*models.DomainRedirect, *errx.Error) {
	l, xerr := s.redirectLink(ctx, orgID, domain)
	if xerr != nil {
		return nil, xerr
	}
	var out models.DomainRedirect
	if xerr := s.clientFor(l).do(ctx, method, path, body, &out); xerr != nil {
		return nil, xerr
	}
	return &out, nil
}

// PutRedirect leaves the offer cached: Cloud enforces its own limit, and a bulk move must not re-read the offer per domain.
func (s *service) PutRedirect(ctx context.Context, orgID uuid.UUID, domain string, in models.DomainRedirectRequest) (*models.DomainRedirect, *errx.Error) {
	// Cloud serves it itself; the instance's own choice of server means nothing there.
	l, xerr := s.redirectLink(ctx, orgID, domain)
	if xerr != nil {
		return nil, xerr
	}
	if err := s.repo.BindRedirect(ctx, orgID, domain, l.InstanceID); err != nil {
		return nil, errx.InternalError()
	}
	in.ServedBy = ""
	return s.redirectCall(ctx, orgID, domain, http.MethodPut, redirectPath(domain), in)
}

func (s *service) GetRedirect(ctx context.Context, orgID uuid.UUID, domain string) (*models.DomainRedirect, *errx.Error) {
	return s.redirectCall(ctx, orgID, domain, http.MethodGet, redirectPath(domain), nil)
}

func (s *service) VerifyRedirect(ctx context.Context, orgID uuid.UUID, domain string) (*models.DomainRedirect, *errx.Error) {
	return s.redirectCall(ctx, orgID, domain, http.MethodPost, redirectPath(domain)+"/verify", nil)
}

func (s *service) DeleteRedirect(ctx context.Context, orgID uuid.UUID, domain string) *errx.Error {
	l, xerr := s.redirectLink(ctx, orgID, domain)
	if xerr != nil {
		return xerr
	}
	return s.clientFor(l).do(ctx, http.MethodDelete, redirectPath(domain), nil, nil)
}

func (s *service) ReleaseRedirect(ctx context.Context, instanceID uuid.UUID, domain string) *errx.Error {
	l, xerr := s.mailboxLink(ctx, &models.CloudLinkMailbox{InstanceID: instanceID})
	if xerr != nil {
		return xerr
	}
	return s.clientFor(l).do(ctx, http.MethodDelete, redirectPath(domain), nil, nil)
}
