package jobs

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/app/customhost"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/jobrun"
	"github.com/warmbly/warmbly/internal/pkg/trackdns"
	"github.com/warmbly/warmbly/internal/repository"
)

// FormsDomainSweep re-resolves every organization's custom forms domain on an
// interval, for the same reason the mailbox tracking-domain sweep exists: DNS
// is not a one-time fact. A record that finishes propagating after the
// customer pressed save starts being used without them coming back, and one
// that later stops pointing here stops being used instead of quietly serving
// form links on a host that no longer answers.
type FormsDomainSweep struct {
	orgs    repository.OrganizationRepository
	hosts   *customhost.Checker
	portals repository.BrandRepository
	checker PortalChecker
}

// PortalChecker is the DNS verdict on an agency portal host; satisfied by brandkit.Service.
type PortalChecker interface {
	CheckPortal(ctx context.Context, agencyID uuid.UUID, domain string, held bool) trackdns.Result
}

// WithPortals sweeps agency portal domains on the same interval.
func (j *FormsDomainSweep) WithPortals(portals repository.BrandRepository, checker PortalChecker) *FormsDomainSweep {
	j.portals, j.checker = portals, checker
	return j
}

func NewFormsDomainSweep(orgs repository.OrganizationRepository, hosts *customhost.Checker) *FormsDomainSweep {
	return &FormsDomainSweep{orgs: orgs, hosts: hosts}
}

func (j *FormsDomainSweep) Run(ctx context.Context) error {
	if err := j.sweepForms(ctx); err != nil {
		return err
	}
	return j.sweepPortals(ctx)
}

func (j *FormsDomainSweep) sweepForms(ctx context.Context) error {
	if j.orgs == nil {
		return nil
	}
	target := config.FormsHostname()
	if target == "" {
		// Nothing to point a record at; every lookup would fail the same way.
		return nil
	}

	rows, err := j.orgs.ListFormsDomains(ctx)
	if err != nil {
		return err
	}
	for _, row := range rows {
		// An unverified domain needs the workspace's TXT proof as well as the CNAME.
		res := j.hosts.Gate(ctx, trackdns.Verify(ctx, row.Domain, target), row.OrganizationID, row.Verified)
		// A resolver failure is transient: never let it un-verify a domain
		// that was working, or one DNS blip drops every customer back to the
		// shared host.
		if res.Code == trackdns.CodeLookupError {
			continue
		}
		if res.Verified == row.Verified {
			continue
		}
		var at *time.Time
		if res.Verified {
			now := time.Now().UTC()
			at = &now
		}
		if err := j.orgs.SetFormsDomainVerified(ctx, row.OrganizationID, res.Verified, at); err != nil {
			if errors.Is(err, repository.ErrFormsDomainTaken) {
				// Another workspace holds this domain; it stays unverified here.
				continue
			}
			log.Warn().Err(err).Str("organization_id", row.OrganizationID.String()).Msg("forms domain sweep: persist failed")
			continue
		}
		log.Info().
			Str("organization_id", row.OrganizationID.String()).
			Str("domain", row.Domain).
			Bool("verified", res.Verified).
			Str("code", res.Code).
			Msg("forms domain verification changed")
	}
	return nil
}

// sweepPortals re-resolves agency portal domains with the same rules as forms domains.
func (j *FormsDomainSweep) sweepPortals(ctx context.Context) error {
	if j.portals == nil || j.checker == nil || config.PortalCNAMETarget() == "" {
		return nil
	}
	rows, err := j.portals.ListPortalDomains(ctx)
	if err != nil {
		return err
	}
	for _, row := range rows {
		res := j.checker.CheckPortal(ctx, row.AgencyID, row.PortalDomain, row.PortalDomainVerified)
		if res.Code == trackdns.CodeLookupError || res.Verified == row.PortalDomainVerified {
			continue
		}
		if res.Verified {
			if held, err := j.portals.PortalHostHeldElsewhere(ctx, row.AgencyID, row.PortalDomain); err != nil || held {
				continue
			}
		}
		var at *time.Time
		if res.Verified {
			now := time.Now().UTC()
			at = &now
		}
		if err := j.portals.SetAgencyPortalDomainVerified(ctx, row.AgencyID, row.PortalDomain, res.Verified, at); err != nil {
			if !errors.Is(err, repository.ErrPortalDomainTaken) {
				log.Warn().Err(err).Str("agency_id", row.AgencyID.String()).Msg("portal domain sweep: persist failed")
			}
			continue
		}
		log.Info().Str("agency_id", row.AgencyID.String()).Str("domain", row.PortalDomain).
			Bool("verified", res.Verified).Str("code", res.Code).Msg("portal domain verification changed")
	}
	return nil
}

// Start runs the sweep once on boot and then on the interval until ctx ends.
func (j *FormsDomainSweep) Start(ctx context.Context, interval time.Duration) {
	jobrun.Loop(ctx, "forms_domain_sweep", interval, true, j.Run)
}
