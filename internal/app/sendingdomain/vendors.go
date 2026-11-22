package sendingdomain

import (
	"context"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// Vendors is several VendorDomains as one: the inbox vendor accounts and the
// store each hold their own domains. A domain goes to the first that holds it.
type Vendors []VendorDomains

// DomainLinks merges every source's links; the first source wins a domain both name.
func (vs Vendors) DomainLinks(ctx context.Context, orgID uuid.UUID) (map[string]models.VendorDomainLink, *errx.Error) {
	out := map[string]models.VendorDomainLink{}
	for _, v := range vs {
		links, xerr := v.DomainLinks(ctx, orgID)
		if xerr != nil {
			return nil, xerr
		}
		for d, l := range links {
			if _, ok := out[d]; !ok {
				out[d] = l
			}
		}
	}
	return out, nil
}

func (vs Vendors) holder(ctx context.Context, orgID uuid.UUID, domain string) (VendorDomains, *errx.Error) {
	for _, v := range vs {
		links, xerr := v.DomainLinks(ctx, orgID)
		if xerr != nil {
			return nil, xerr
		}
		if _, ok := links[normalizeDomain(domain)]; ok {
			return v, nil
		}
	}
	return nil, errx.ErrNotFound
}

func (vs Vendors) SetForwarding(ctx context.Context, orgID uuid.UUID, domain, target string) (*models.VendorDomainLink, *errx.Error) {
	v, xerr := vs.holder(ctx, orgID, domain)
	if xerr != nil {
		return nil, xerr
	}
	return v.SetForwarding(ctx, orgID, domain, target)
}

func (vs Vendors) UpsertDNS(ctx context.Context, orgID uuid.UUID, domain, typ, name, value string) *errx.Error {
	v, xerr := vs.holder(ctx, orgID, domain)
	if xerr != nil {
		return xerr
	}
	return v.UpsertDNS(ctx, orgID, domain, typ, name, value)
}
