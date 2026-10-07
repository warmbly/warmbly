package sendingdomain

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/mailhost"
	"github.com/warmbly/warmbly/internal/repository"
	"github.com/warmbly/warmbly/internal/utils/validate"
)

// CloudRedirects is Warmbly Cloud serving root redirects for this linked instance.
type CloudRedirects interface {
	// RedirectOffer is Cloud's offer and whether the instance is linked; a linked instance gets nil while Cloud is unreachable.
	RedirectOffer(ctx context.Context, orgID uuid.UUID) (*models.PoolLinkRedirectOffer, bool)
	ReleaseRedirect(ctx context.Context, instanceID uuid.UUID, domain string) *errx.Error
	ListRedirects(ctx context.Context) ([]models.DomainRedirect, *errx.Error)
	PutRedirect(ctx context.Context, orgID uuid.UUID, domain string, in models.DomainRedirectRequest) (*models.DomainRedirect, *errx.Error)
	GetRedirect(ctx context.Context, orgID uuid.UUID, domain string) (*models.DomainRedirect, *errx.Error)
	VerifyRedirect(ctx context.Context, orgID uuid.UUID, domain string) (*models.DomainRedirect, *errx.Error)
	DeleteRedirect(ctx context.Context, orgID uuid.UUID, domain string) *errx.Error
}

// WireCloud attaches the instance's link to Warmbly Cloud; optional.
func (s *Service) WireCloud(c CloudRedirects) { s.cloud = c }

const unlinkedMessage = "This instance is no longer connected to Warmbly Cloud. Reconnect it in Settings, or serve the redirect from this server."

const pendingCloudMessage = "Warmbly Cloud has not confirmed this redirect yet. Warmbly keeps trying by itself."

// cloudOrphanGrace is how old a Cloud row with no row here must be before the sweep releases it, so a save in flight is never undone.
const cloudOrphanGrace = 15 * time.Minute

// cloudOutage is an answer that says nothing about the redirect: Cloud was down, slow, or busy.
func cloudOutage(xerr *errx.Error) bool {
	return xerr.Code >= 500 || xerr.Code == errx.TooManyRequests
}

// cloudRefused is a refusal Cloud gives for the redirect itself, which stands until someone changes it.
func cloudRefused(xerr *errx.Error) bool {
	switch xerr.Identifier {
	case ErrIDLimit, ErrIDTaken, ErrIDTarget, ErrIDConsumerDomain, ErrIDNotYours:
		return true
	}
	return false
}

// cloudServable says whether Cloud may take the domain. One Cloud already serves is only edited, so Cloud's own answer decides.
func (s *Service) cloudServable(ctx context.Context, orgID uuid.UUID, domain string, alreadyCloud bool) *errx.Error {
	if s.cloud == nil {
		return errx.NewWithIdentifier(errx.Conflict, ErrIDCloudUnavailable, "Connect this instance to Warmbly Cloud to serve redirects from there.")
	}
	if !alreadyCloud {
		offer, linked := s.cloud.RedirectOffer(ctx, orgID)
		switch {
		case !linked:
			return errx.NewWithIdentifier(errx.Conflict, ErrIDCloudUnavailable, "Connect this instance to Warmbly Cloud to serve redirects from there.")
		case offer == nil:
			// Linked, but Cloud has not answered since this process started.
			return errx.NewWithIdentifier(errx.ServiceUnavailable, ErrIDCloudUnreachable, "Warmbly Cloud could not be reached. Try again in a moment.")
		case !offer.Available:
			return errx.NewWithIdentifier(errx.Conflict, ErrIDCloudUnavailable, "Warmbly Cloud does not serve redirects for this instance right now.")
		}
	}
	taken, err := s.redirects.CloudServedElsewhere(ctx, orgID, domain)
	if err != nil {
		return errx.InternalError()
	}
	if taken {
		return errx.NewWithIdentifier(errx.Conflict, ErrIDTaken, "Another workspace on this instance already redirects this domain.")
	}
	return nil
}

// cloudRefusal keeps Cloud's refusal but never its 401, which is about the link token, not the caller's session.
func cloudRefusal(xerr *errx.Error) *errx.Error {
	switch {
	case cloudGone(xerr):
		return errx.NewWithIdentifier(errx.Conflict, ErrIDCloudUnavailable, "This instance is no longer linked to Warmbly Cloud. Reconnect it in Settings, or serve the redirect from this server.")
	case xerr.Code >= 500 || xerr.Code == errx.TooManyRequests:
		return errx.NewWithIdentifier(errx.ServiceUnavailable, ErrIDCloudUnreachable, "Warmbly Cloud could not be reached. Try again in a moment.")
	}
	return xerr
}

// cloudGone is a Cloud answer meaning the redirect is no longer served there: none there, or no link at all.
func cloudGone(xerr *errx.Error) bool {
	if xerr.Code == errx.Unauthorized || xerr.Code == errx.Forbidden {
		return true // a revoked link: Cloud dropped its redirects with it
	}
	switch xerr.Identifier {
	case ErrIDRemoteNotFound, "cloud_link_not_connected", "pool_link_revoked", "pool_link_instance_not_found", "unauthorized":
		return true
	}
	return false
}

// releaseCloud stops Cloud serving a domain before this instance forgets it did.
func (s *Service) releaseCloud(ctx context.Context, orgID uuid.UUID, domain string) *errx.Error {
	if s.cloud == nil {
		return nil
	}
	if xerr := s.cloud.DeleteRedirect(ctx, orgID, domain); xerr != nil && !cloudGone(xerr) {
		return errx.NewWithIdentifier(errx.ServiceUnavailable, ErrIDCloudUnreachable,
			"Warmbly Cloud could not be reached, so the redirect is still served there. Try again in a moment.")
	}
	return nil
}

// mirror records Cloud's verdict on a cloud-served row as this row's own.
func (s *Service) mirror(ctx context.Context, r *models.DomainRedirect, remote *models.DomainRedirect) (*models.DomainRedirect, *errx.Error) {
	if err := s.redirects.SetCheck(ctx, r.ID, remote.Verified, remote.LastError); err != nil {
		if errors.Is(err, repository.ErrRedirectTaken) {
			return nil, errx.NewWithIdentifier(errx.Conflict, ErrIDTaken, "Another workspace on this instance already redirects this domain.")
		}
		return nil, errx.InternalError()
	}
	if err := s.redirects.SetRemote(ctx, r.ID, remote.ServeHost, remote.Records); err != nil {
		return nil, errx.InternalError()
	}
	if err := s.redirects.SetReach(ctx, r.ID, remote.Reach); err != nil {
		return nil, errx.InternalError()
	}
	return s.finish(ctx, r, nil)
}

// checkCloud mirrors Cloud's verdict: a lost row is sent again, an ended link stops it, an outage changes nothing.
func (s *Service) checkCloud(ctx context.Context, r *models.DomainRedirect, force bool) (*models.DomainRedirect, *errx.Error) {
	if s.cloud == nil {
		return s.unlink(ctx, r)
	}
	var remote *models.DomainRedirect
	var xerr *errx.Error
	if force {
		remote, xerr = s.cloud.VerifyRedirect(ctx, r.OrganizationID, r.Domain)
	} else {
		remote, xerr = s.cloud.GetRedirect(ctx, r.OrganizationID, r.Domain)
	}
	// A row Cloud lost, or holds with an older target (a save whose answer was lost), is sent again.
	if (xerr != nil && xerr.Identifier == ErrIDRemoteNotFound) || (xerr == nil && (remote.TargetURL != r.TargetURL || remote.IncludeWWW != r.IncludeWWW)) {
		www := r.IncludeWWW
		remote, xerr = s.cloud.PutRedirect(ctx, r.OrganizationID, r.Domain, models.DomainRedirectRequest{TargetURL: r.TargetURL, IncludeWWW: &www})
	}
	if xerr != nil {
		if cloudGone(xerr) {
			return s.unlink(ctx, r)
		}
		if cloudRefused(xerr) {
			return s.stop(ctx, r, xerr.Message)
		}
		if err := s.redirects.SetCheck(ctx, r.ID, r.Verified, r.LastError); err != nil && !errors.Is(err, repository.ErrRedirectTaken) {
			return nil, errx.InternalError()
		}
		if force {
			return nil, cloudRefusal(xerr)
		}
		return s.finish(ctx, r, nil)
	}
	return s.mirror(ctx, r, remote)
}

func (s *Service) unlink(ctx context.Context, r *models.DomainRedirect) (*models.DomainRedirect, *errx.Error) {
	return s.stop(ctx, r, unlinkedMessage)
}

func (s *Service) stop(ctx context.Context, r *models.DomainRedirect, why string) (*models.DomainRedirect, *errx.Error) {
	if err := s.redirects.SetCheck(ctx, r.ID, false, why); err != nil {
		return nil, errx.InternalError()
	}
	if err := s.redirects.SetReach(ctx, r.ID, nil); err != nil {
		return nil, errx.InternalError()
	}
	return s.finish(ctx, r, nil)
}

// reconcileCloud releases what Cloud serves for this instance that no workspace here has Cloud serve any more.
func (s *Service) reconcileCloud(ctx context.Context) {
	if s.cloud == nil {
		return
	}
	remote, xerr := s.cloud.ListRedirects(ctx)
	if xerr != nil {
		return
	}
	local, err := s.redirects.CloudServedDomains(ctx)
	if err != nil {
		return
	}
	for _, r := range remote {
		owner, exists := local[r.Domain]
		if r.CloudLinkInstanceID != nil && (!exists || owner != *r.CloudLinkInstanceID) && time.Since(r.CreatedAt) > cloudOrphanGrace {
			if xerr := s.cloud.ReleaseRedirect(ctx, *r.CloudLinkInstanceID, r.Domain); xerr != nil && !cloudGone(xerr) {
				log.Warn().Str("domain", r.Domain).Str("code", xerr.ResponseCode()).Msg("domain redirect sweep: could not release a Cloud redirect")
			}
		}
	}
}

// MarkCloudUnlinked stops every cloud-served redirect claiming to be live, once the link to Cloud ends.
func (s *Service) MarkCloudUnlinked(ctx context.Context, instanceID uuid.UUID) {
	_ = s.redirects.UnverifyCloudServed(ctx, instanceID, unlinkedMessage)
}

// The Cloud side: rows served for a linked instance, proven against this deployment's own TXT value and tracking host.

// LinkedOffer is what this deployment offers a linked instance; unavailable without a tracking host.
func (s *Service) LinkedOffer(ctx context.Context, instanceID uuid.UUID) *models.PoolLinkRedirectOffer {
	offer := &models.PoolLinkRedirectOffer{Limit: config.PoolLinkRedirectLimit}
	if s.target() == "" {
		return offer
	}
	offer.Available, offer.Host = true, s.target()
	if n, err := s.redirects.CountLinked(ctx, instanceID); err == nil {
		offer.Used = n
	}
	return offer
}

func linkedNotFound() *errx.Error {
	return errx.NewWithIdentifier(errx.NotFound, ErrIDRemoteNotFound, "Warmbly Cloud does not serve a redirect for this domain.")
}

func (s *Service) LinkedList(ctx context.Context, inst *models.PoolLinkInstance) ([]models.DomainRedirect, *errx.Error) {
	list, err := s.redirects.ListLinked(ctx, inst.ID)
	if err != nil {
		return nil, errx.InternalError()
	}
	for i := range list {
		s.decorate(&list[i], nil)
	}
	return list, nil
}

func (s *Service) LinkedGet(ctx context.Context, inst *models.PoolLinkInstance, domain string) (*models.DomainRedirect, *errx.Error) {
	r, err := s.redirects.GetLinked(ctx, inst.ID, normalizeDomain(domain))
	if err != nil {
		return nil, errx.InternalError()
	}
	if r == nil {
		return nil, linkedNotFound()
	}
	s.decorate(r, nil)
	return r, nil
}

// LinkedSet serves (or updates) a redirect for a linked instance; ownership is proven here, never taken on its word.
func (s *Service) LinkedSet(ctx context.Context, inst *models.PoolLinkInstance, domain string, in models.DomainRedirectRequest) (*models.DomainRedirect, *errx.Error) {
	domain = normalizeDomain(domain)
	if !validate.TrackingHostname(domain) {
		return nil, errx.NewWithIdentifier(errx.BadRequest, ErrIDNotYours, "Enter a domain.")
	}
	if mailhost.SharedProvider(domain) {
		return nil, errx.NewWithIdentifier(errx.BadRequest, ErrIDConsumerDomain, "A shared email provider's domain cannot be redirected.")
	}
	target, xerr := cleanTarget(in.TargetURL, domain)
	if xerr != nil {
		return nil, xerr
	}
	if s.target() == "" {
		return nil, errx.NewWithIdentifier(errx.Conflict, ErrIDCloudUnavailable, "Warmbly Cloud does not serve redirects right now.")
	}
	www := true
	if in.IncludeWWW != nil {
		www = *in.IncludeWWW
	}
	existing, err := s.redirects.GetLinked(ctx, inst.ID, domain)
	if err != nil {
		return nil, errx.InternalError()
	}
	if existing == nil {
		other, err := s.redirects.Get(ctx, inst.OrganizationID, domain)
		if err != nil {
			return nil, errx.InternalError()
		}
		if other != nil {
			return nil, errx.NewWithIdentifier(errx.Conflict, ErrIDTaken, "Your Warmbly Cloud workspace already has a redirect for this domain. Remove it there first.")
		}
	}
	if existing == nil && inst.RemoteOrganizationID == nil {
		return nil, errx.NewWithIdentifier(errx.Conflict, "pool_link_workspace_required", "Connect per workspace to add redirects. Existing legacy redirects keep working.")
	}
	instanceID := inst.ID
	r := &models.DomainRedirect{ID: uuid.New(), OrganizationID: inst.OrganizationID, Domain: domain, TargetURL: target, IncludeWWW: www,
		VerifyToken: s.proof.Value(inst.OrganizationID, domain), ServedBy: models.RedirectServedByInstance, LinkedInstanceID: &instanceID}
	ok, err := s.redirects.UpsertLinked(ctx, r, inst.CreatedBy, config.PoolLinkRedirectLimit)
	if errors.Is(err, repository.ErrRedirectOwned) {
		return nil, errx.NewWithIdentifier(errx.Conflict, ErrIDTaken, "Your Warmbly Cloud workspace already has a redirect for this domain. Remove it there first.")
	}
	if err != nil {
		return nil, errx.InternalError()
	}
	if !ok {
		return nil, errx.NewWithIdentifier(errx.Conflict, ErrIDLimit, "This instance has reached the number of redirects Warmbly Cloud serves for it.")
	}
	out, xerr := s.check(ctx, r, true)
	// A save the instance is told was refused leaves nothing behind: a new row goes, an existing one is put back.
	if xerr != nil {
		if existing == nil {
			_, _ = s.redirects.DeleteLinked(ctx, inst.ID, domain)
		} else {
			s.restoreLinked(ctx, inst, existing)
		}
	}
	return out, xerr
}

func (s *Service) restoreLinked(ctx context.Context, inst *models.PoolLinkInstance, prev *models.DomainRedirect) {
	back := *prev
	if _, err := s.redirects.UpsertLinked(ctx, &back, inst.CreatedBy, config.PoolLinkRedirectLimit); err != nil {
		return
	}
	_ = s.redirects.SetCheck(ctx, prev.ID, prev.Verified, prev.LastError)
	_ = s.redirects.SetReach(ctx, prev.ID, prev.Reach)
}

func (s *Service) LinkedVerify(ctx context.Context, inst *models.PoolLinkInstance, domain string) (*models.DomainRedirect, *errx.Error) {
	r, err := s.redirects.GetLinked(ctx, inst.ID, normalizeDomain(domain))
	if err != nil {
		return nil, errx.InternalError()
	}
	if r == nil {
		return nil, linkedNotFound()
	}
	return s.check(ctx, r, true)
}

func (s *Service) LinkedDelete(ctx context.Context, inst *models.PoolLinkInstance, domain string) *errx.Error {
	ok, err := s.redirects.DeleteLinked(ctx, inst.ID, normalizeDomain(domain))
	if err != nil {
		return errx.InternalError()
	}
	if !ok {
		return linkedNotFound()
	}
	return nil
}
