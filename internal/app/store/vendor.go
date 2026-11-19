package store

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/graphkit"
)

// VendorKey names the store among the vendors a sending domain can be held by.
const VendorKey = "purchased"

// vendorDNSTypes are the records the sending-domain setup writes through the store.
var vendorDNSTypes = []string{"A", "AAAA", "CNAME", "MX", "TXT"}

// DomainVendor lets the sending-domain service treat store domains like a
// vendor's: forwarding and the tracking CNAME are written through GraphKit,
// so a store domain never needs DNS added by hand.
type DomainVendor struct{ S *Service }

func toXerr(err error) *errx.Error {
	if err == nil {
		return nil
	}
	var xerr *errx.Error
	if errors.As(err, &xerr) {
		return xerr
	}
	return errx.InternalError()
}

// DomainLinks maps each active store domain to what the store can do for it.
func (v DomainVendor) DomainLinks(ctx context.Context, orgID uuid.UUID) (map[string]models.VendorDomainLink, *errx.Error) {
	out := map[string]models.VendorDomainLink{}
	if !v.S.Enabled() {
		return out, nil
	}
	rows, err := v.S.Repo.ListDomains(ctx, orgID, nil, maxPageLimit*5)
	if err != nil {
		return nil, toXerr(err)
	}
	for i := range rows {
		d := &rows[i]
		if d.Status != graphkit.DomainActive {
			continue
		}
		out[d.Name] = v.link(d)
	}
	return out, nil
}

func (v DomainVendor) link(d *models.StoreDomain) models.VendorDomainLink {
	l := models.VendorDomainLink{Vendor: VendorKey, ConnectionID: d.ID, Forwarding: d.ForwardingURL, CanForward: true, CanUnforward: true, DNSTypes: []string{}}
	if dnsWritable(d) {
		l.CanDNS, l.DNSTypes = true, vendorDNSTypes
	}
	return l
}

func (v DomainVendor) byName(ctx context.Context, orgID uuid.UUID, domain string) (*models.StoreDomain, *errx.Error) {
	d, err := v.S.Repo.GetDomainByName(ctx, orgID, strings.ToLower(domain))
	if err != nil {
		return nil, toXerr(err)
	}
	if d == nil {
		return nil, errx.ErrNotFound
	}
	return d, nil
}

// SetForwarding points the domain's root at target; empty stops forwarding.
func (v DomainVendor) SetForwarding(ctx context.Context, orgID uuid.UUID, domain, target string) (*models.VendorDomainLink, *errx.Error) {
	d, xerr := v.byName(ctx, orgID, domain)
	if xerr != nil {
		return nil, xerr
	}
	if _, err := v.S.PatchDomain(ctx, orgID, d.ID, DomainPatch{ForwardingURL: &target}); err != nil {
		return nil, toXerr(err)
	}
	if d, xerr = v.byName(ctx, orgID, domain); xerr != nil {
		return nil, xerr
	}
	l := v.link(d)
	return &l, nil
}

// UpsertDNS writes one record by name: the workspace's record of that type and name is changed, else one is added.
func (v DomainVendor) UpsertDNS(ctx context.Context, orgID uuid.UUID, domain, typ, name, value string) *errx.Error {
	d, xerr := v.byName(ctx, orgID, domain)
	if xerr != nil {
		return xerr
	}
	rel, xerr := cleanRecordName(name, d.Name)
	if xerr != nil {
		return xerr
	}
	recs, gerr := v.S.api().ListDNS(ctx, d.GraphKitDomainID)
	if gerr != nil {
		return v.S.upstream(ctx, "dns.read", gerr)
	}
	for _, r := range recs {
		if r.Managed || !strings.EqualFold(r.Type, typ) || r.Name != rel {
			continue
		}
		if r.Content == value {
			return nil
		}
		_, err := v.S.UpdateDNSRecord(ctx, orgID, d.ID, recordID(d.ID, r.ID), DNSRecordPatch{Content: &value})
		return toXerr(err)
	}
	_, err := v.S.AddDNSRecord(ctx, orgID, d.ID, DNSRecordInput{Type: typ, Name: rel, Content: value})
	return toXerr(err)
}

// ManagedBy adapts the partner core's check, which also names the partner, to ManagedOrgs.
type ManagedBy func(ctx context.Context, orgID uuid.UUID) (bool, string)

// PartnerManaged reports a workspace a partner manages.
func (f ManagedBy) PartnerManaged(ctx context.Context, orgID uuid.UUID) bool {
	ok, _ := f(ctx, orgID)
	return ok
}
