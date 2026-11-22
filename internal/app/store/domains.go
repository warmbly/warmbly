package store

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/pubsub"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/graphkit"
)

// domainDetails is the display state store_domains.details keeps. Conflicts
// hold GraphKit's record ids, which only ever leave as Warmbly record ids.
type domainDetails struct {
	Nameservers       []string               `json:"nameservers"`
	ForwardingStatus  string                 `json:"forwarding_status"`
	DNSHost           string                 `json:"dns_host"`
	Connection        *DomainConnection      `json:"connection"`
	Conflicts         []graphkit.DNSConflict `json:"conflicts"`
	OwnerVerification *OwnerVerification     `json:"owner_verification"`
	Transfer          *TransferRequest       `json:"transfer"`
}

func detailsOf(d *graphkit.Domain) domainDetails {
	out := domainDetails{Nameservers: nonNil(d.Nameservers), ForwardingStatus: d.ForwardingStatus, DNSHost: d.DNSHost, Conflicts: d.DNSConflicts}
	if out.ForwardingStatus == "" {
		out.ForwardingStatus = "none"
	}
	if !d.Registered() {
		method := d.Connection
		if method != graphkit.ConnectionNameservers {
			// The store connects by nameservers or by records; a customer's own DNS reads as records.
			method = graphkit.ConnectionManual
		}
		out.Connection = &DomainConnection{Method: method, Status: d.ConnectionStatus, ExpiresAt: d.ConnectionExpiresAt,
			CheckedAt: d.ConnectionCheckedAt, CurrentNameservers: nonNil(d.CurrentNameservers)}
	}
	if v := d.RegistrantVerification; v != nil {
		out.OwnerVerification = &OwnerVerification{Status: v.Status, Email: v.Email, Deadline: v.Deadline}
	}
	if t := d.Transfer; t != nil {
		out.Transfer = &TransferRequest{Status: t.Status, RequestedAt: t.RequestedAt, AutoApproveAt: t.AutoApproveAt}
	}
	return out
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func sourceOf(d *graphkit.Domain) string {
	if d.Registered() {
		return models.StoreDomainPurchased
	}
	return models.StoreDomainConnected
}

// recordID is a GraphKit record's Warmbly id: stable, opaque, and scoped to the domain.
func recordID(domainID uuid.UUID, graphkitID string) uuid.UUID {
	return uuid.NewSHA1(domainID, []byte(graphkitID))
}

// upsertDomain stores GraphKit's current domain, with its renewal price at retail as a display cache.
// A removed domain is forgotten instead.
func (s *Service) upsertDomain(ctx context.Context, orgID uuid.UUID, orderID *uuid.UUID, d *graphkit.Domain) (*models.StoreDomain, error) {
	if d.Status == graphkit.DomainRemoved {
		row, err := s.Repo.GetDomainByGraphKitID(ctx, orgID, d.ID)
		if err != nil || row == nil {
			return nil, err
		}
		if _, err := s.Repo.DeleteConnectedDomain(ctx, orgID, row.ID); err != nil {
			return nil, err
		}
		s.publish(ctx, orgID, pubsub.StoreEvent{BaseEvent: pubsub.BaseEvent{EventType: pubsub.EventStoreDomainUpdated}, ID: row.ID.String(), Status: d.Status})
		return nil, nil
	}
	mailAuth, _ := json.Marshal(MailAuth{MX: d.MailAuth.MX, SPF: d.MailAuth.SPF, DKIM: d.MailAuth.DKIM, DMARC: d.MailAuth.DMARC})
	details, _ := json.Marshal(detailsOf(d))
	fwd := ""
	if d.ForwardingURL != nil {
		fwd = *d.ForwardingURL
	}
	row, err := s.Repo.UpsertDomain(ctx, &models.StoreDomain{ID: uuid.New(), OrganizationID: orgID, OrderID: orderID,
		GraphKitDomainID: d.ID, Name: d.Domain, Status: d.Status, Source: sourceOf(d), Provider: d.Provider, ExpiresAt: d.ExpiresAt,
		RenewRetailMicros: models.Micro(retailDomain(s.retail(ctx), d.RenewMicros)), ForwardingURL: fwd, MailAuth: mailAuth, Details: details})
	if err != nil || row == nil {
		return row, err
	}
	s.publish(ctx, orgID, pubsub.StoreEvent{BaseEvent: pubsub.BaseEvent{EventType: pubsub.EventStoreDomainUpdated}, ID: row.ID.String(), Status: row.Status})
	return row, nil
}

func renewable(d *models.StoreDomain) bool {
	return d.Source != models.StoreDomainConnected && (d.Status == graphkit.DomainActive || d.Status == graphkit.DomainExpired)
}

func domainView(d *models.StoreDomain) Domain {
	out := Domain{ID: d.ID, Name: d.Name, Status: d.Status, Source: d.Source, Provider: d.Provider, Renewable: renewable(d), ExpiresAt: d.ExpiresAt,
		AutoRenew: d.AutoRenew, RenewMicros: int64(d.RenewRetailMicros), ForwardingURL: d.ForwardingURL, MailboxCount: d.MailboxCount,
		TrackingHost: d.TrackingHost, CreatedAt: d.CreatedAt, Nameservers: []string{}, DNSConflicts: []DNSConflict{}, ForwardingStatus: "none"}
	if out.Source == "" {
		out.Source = models.StoreDomainPurchased
	}
	if out.Source == models.StoreDomainConnected {
		out.AutoRenew, out.RenewMicros = false, 0
	}
	_ = json.Unmarshal(d.MailAuth, &out.MailAuth)
	var det domainDetails
	if len(d.Details) > 0 && json.Unmarshal(d.Details, &det) == nil {
		out.Nameservers = nonNil(det.Nameservers)
		if det.ForwardingStatus != "" {
			out.ForwardingStatus = det.ForwardingStatus
		}
		out.Connection, out.OwnerVerification, out.Transfer = det.Connection, det.OwnerVerification, det.Transfer
		for _, c := range det.Conflicts {
			v := DNSConflict{Type: c.Type, Name: c.Name, Content: c.Content}
			if c.RecordID != nil && *c.RecordID != "" {
				id := recordID(d.ID, *c.RecordID)
				v.RecordID = &id
			}
			out.DNSConflicts = append(out.DNSConflicts, v)
		}
	}
	return out
}

// dnsWritable reports a zone GraphKit writes: a purchased domain, or a connected one not on the customer's own DNS.
func dnsWritable(d *models.StoreDomain) bool {
	if d.Source != models.StoreDomainConnected {
		return true
	}
	var det domainDetails
	_ = json.Unmarshal(d.Details, &det)
	return det.DNSHost != graphkit.DNSHostCustomer
}

func recordView(domainID uuid.UUID, r graphkit.DNSRecord) DNSRecord {
	purpose := r.Purpose
	if purpose == "partner" {
		// GraphKit's name for a record the workspace added itself.
		purpose = "custom"
	}
	return DNSRecord{ID: recordID(domainID, r.ID), Type: r.Type, Name: r.Name, Content: r.Content, TTL: r.TTL, Priority: r.Priority,
		Managed: r.Managed, Purpose: purpose, Status: r.Status}
}

// ---- domains ------------------------------------------------------------

// ListDomains is the workspace's purchased and connected domains, newest first.
func (s *Service) ListDomains(ctx context.Context, orgID uuid.UUID, cursor string, limit int) (*Page[Domain], error) {
	if !s.readable() {
		return nil, errDisabled
	}
	after, limit, err := pageArgs(cursor, limit)
	if err != nil {
		return nil, err
	}
	rows, err := s.Repo.ListDomains(ctx, orgID, after, limit+1)
	if err != nil {
		return nil, err
	}
	out := &Page[Domain]{Data: []Domain{}}
	for i := range rows {
		if i == limit {
			break
		}
		out.Data = append(out.Data, domainView(&rows[i]))
	}
	out.Pagination = nextPage(len(rows) > limit, func() models.StorePage {
		return models.StorePage{CreatedAt: rows[limit-1].CreatedAt, ID: rows[limit-1].ID}
	})
	return out, nil
}

func (s *Service) domain(ctx context.Context, orgID, id uuid.UUID) (*models.StoreDomain, error) {
	if !s.Enabled() {
		return nil, errDisabled
	}
	d, err := s.Repo.GetDomain(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	if d == nil {
		return nil, errx.ErrNotFound
	}
	return d, nil
}

// GetDomain is one domain with its DNS records.
func (s *Service) GetDomain(ctx context.Context, orgID, id uuid.UUID) (*Domain, error) {
	d, err := s.domain(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	return s.withDNS(ctx, d), nil
}

// withDNS is the domain's view with its records; a records read that fails leaves them empty.
func (s *Service) withDNS(ctx context.Context, d *models.StoreDomain) *Domain {
	out := domainView(d)
	out.DNS = []DNSRecord{}
	recs, gerr := s.api().ListDNS(ctx, d.GraphKitDomainID)
	if gerr != nil {
		logUpstream("dns", gerr)
		return &out
	}
	for _, r := range recs {
		out.DNS = append(out.DNS, recordView(d.ID, r))
	}
	return &out
}

// refreshDomain reads the domain from GraphKit again and stores it.
func (s *Service) refreshDomain(ctx context.Context, d *models.StoreDomain) *models.StoreDomain {
	g, gerr := s.api().GetDomain(ctx, d.GraphKitDomainID)
	if gerr != nil {
		logUpstream("domain.refresh", gerr)
		return d
	}
	row, err := s.upsertDomain(ctx, d.OrganizationID, nil, g)
	if err != nil || row == nil {
		return d
	}
	return row
}

// PatchDomain changes auto-renew (here) and forwarding (at GraphKit).
func (s *Service) PatchDomain(ctx context.Context, orgID, id uuid.UUID, p DomainPatch) (*Domain, error) {
	d, err := s.domain(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	if p.AutoRenew != nil && d.Source == models.StoreDomainConnected {
		return nil, registeredElsewhere()
	}
	if p.ForwardingURL != nil {
		fwd, xerr := cleanForwarding(*p.ForwardingURL)
		if xerr != nil {
			return nil, xerr
		}
		patch := graphkit.DomainPatch{ClearForwarding: fwd == ""}
		if fwd != "" {
			patch.ForwardingURL = &fwd
		}
		g, gerr := s.api().UpdateDomain(ctx, d.GraphKitDomainID, patch, opKey("forward", d.ID))
		if gerr != nil {
			return nil, s.upstream(ctx, "domain.forward", gerr)
		}
		if _, err := s.upsertDomain(ctx, orgID, nil, g); err != nil {
			return nil, err
		}
	}
	if p.AutoRenew != nil {
		if err := s.Repo.SetDomainAutoRenew(ctx, orgID, id, *p.AutoRenew); err != nil {
			return nil, err
		}
	}
	if d, err = s.domain(ctx, orgID, id); err != nil {
		return nil, err
	}
	s.publish(ctx, orgID, pubsub.StoreEvent{BaseEvent: pubsub.BaseEvent{EventType: pubsub.EventStoreDomainUpdated}, ID: d.ID.String(), Status: d.Status})
	out := domainView(d)
	return &out, nil
}

// TransferOut unlocks a domain for another registrar and returns its auth code.
// Auto-renew goes off: the domain is leaving.
func (s *Service) TransferOut(ctx context.Context, orgID, id uuid.UUID) (string, error) {
	d, err := s.domain(ctx, orgID, id)
	if err != nil {
		return "", err
	}
	if d.Source == models.StoreDomainConnected {
		return "", registeredElsewhere()
	}
	t, gerr := s.api().TransferOut(ctx, d.GraphKitDomainID, opKey("transfer", d.ID))
	if gerr != nil {
		return "", s.upstream(ctx, "domain.transfer", gerr)
	}
	if err := s.Repo.SetDomainAutoRenew(ctx, orgID, id, false); err != nil {
		return "", err
	}
	if t.Domain != nil {
		_, _ = s.upsertDomain(ctx, orgID, nil, t.Domain)
	} else {
		s.refreshDomain(ctx, d)
	}
	return t.AuthCode, nil
}

// ---- existing domains ---------------------------------------------------

// ConnectDomain adds a domain the workspace registered elsewhere: by moving
// its nameservers here, or by records added at its DNS provider. It is free,
// but its only use is buying mailboxes on it, so it needs a buyer.
func (s *Service) ConnectDomain(ctx context.Context, orgID uuid.UUID, in ConnectRequest) (*Domain, error) {
	if err := s.requireBuyer(ctx, orgID); err != nil {
		return nil, err
	}
	name := strings.ToLower(strings.TrimSpace(strings.TrimSuffix(in.Domain, ".")))
	name = strings.TrimPrefix(name, "www.")
	if !domainRe.MatchString(name) {
		return nil, itemsErr("Enter a domain name, like acme.com.")
	}
	method := in.Method
	if method == "" {
		method = graphkit.ConnectionNameservers
	}
	if method != graphkit.ConnectionNameservers && method != graphkit.ConnectionManual {
		return nil, itemsErr("Connect by changing nameservers or by adding records.")
	}
	if err := s.ensureAccount(ctx, orgID); err != nil {
		return nil, err
	}
	g, gerr := s.api().ConnectDomain(ctx, externalID(orgID), name, method, opKey("connect", orgID))
	if gerr != nil {
		return nil, s.upstream(ctx, "domain.connect", gerr)
	}
	row, err := s.upsertDomain(ctx, orgID, nil, g)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, unavailable()
	}
	return s.withDNS(ctx, row), nil
}

func (s *Service) connectedDomain(ctx context.Context, orgID, id uuid.UUID) (*models.StoreDomain, error) {
	d, err := s.domain(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	if d.Source != models.StoreDomainConnected {
		return nil, errx.NewWithIdentifier(errx.Conflict, CodeRegisteredHere, "This domain was bought here, so it is already connected. To move it, transfer it out.")
	}
	return d, nil
}

// CheckConnection looks for a connected domain's nameservers or records now.
func (s *Service) CheckConnection(ctx context.Context, orgID, id uuid.UUID) (*Domain, error) {
	d, err := s.connectedDomain(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	g, gerr := s.api().CheckConnection(ctx, d.GraphKitDomainID, opKey("connection.check", d.ID))
	if gerr != nil {
		return nil, s.upstream(ctx, "domain.check", gerr)
	}
	row, err := s.upsertDomain(ctx, orgID, nil, g)
	if err != nil || row == nil {
		return nil, err
	}
	out := domainView(row)
	return &out, nil
}

// RegenerateConnection starts an expired connection again: new nameservers, or a new ownership record.
func (s *Service) RegenerateConnection(ctx context.Context, orgID, id uuid.UUID) (*Domain, error) {
	d, err := s.connectedDomain(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	g, gerr := s.api().RegenerateConnection(ctx, d.GraphKitDomainID, opKey("connection.regenerate", d.ID))
	if gerr != nil {
		return nil, s.upstream(ctx, "domain.regenerate", gerr)
	}
	row, err := s.upsertDomain(ctx, orgID, nil, g)
	if err != nil || row == nil {
		return nil, err
	}
	return s.withDNS(ctx, row), nil
}

// RemoveDomain removes a connected domain with no mailboxes; its records leave with it.
func (s *Service) RemoveDomain(ctx context.Context, orgID, id uuid.UUID) error {
	d, err := s.connectedDomain(ctx, orgID, id)
	if err != nil {
		return err
	}
	if d.MailboxCount > 0 {
		return stateConflict("domain.remove")
	}
	if gerr := s.api().RemoveDomain(ctx, d.GraphKitDomainID, opKey("remove", d.ID)); gerr != nil && graphkit.CodeOf(gerr) != graphkit.CodeNotFound {
		return s.upstream(ctx, "domain.remove", gerr)
	}
	gone, err := s.Repo.DeleteConnectedDomain(ctx, orgID, id)
	if err != nil {
		return err
	}
	if !gone {
		return stateConflict("domain.remove")
	}
	s.publish(ctx, orgID, pubsub.StoreEvent{BaseEvent: pubsub.BaseEvent{EventType: pubsub.EventStoreDomainUpdated}, ID: id.String(), Status: graphkit.DomainRemoved})
	return nil
}

// ---- DNS records ----------------------------------------------------------

var dnsTypes = map[string]bool{"A": true, "AAAA": true, "CNAME": true, "MX": true, "TXT": true, "SRV": true, "CAA": true}

func dnsInvalid(msg string) *errx.Error {
	return errx.NewWithIdentifier(errx.BadRequest, CodeDNSRecordInvalid, msg)
}

// cleanRecordName takes a relative name, "@", or a full name under the domain, and returns the relative one.
func cleanRecordName(raw, domain string) (string, *errx.Error) {
	n := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(raw), "."))
	switch {
	case n == "" || n == "@" || n == domain:
		return "@", nil
	case strings.HasSuffix(n, "."+domain):
		n = strings.TrimSuffix(n, "."+domain)
	}
	if len(n) > 253 || !recordNameRe.MatchString(n) {
		return "", dnsInvalid("Use a name like www, mail or _dmarc, or @ for the domain itself.")
	}
	return n, nil
}

func checkTTL(ttl int) *errx.Error {
	if ttl != 0 && (ttl < 60 || ttl > 86400) {
		return dnsInvalid("TTL is between 60 and 86400 seconds.")
	}
	return nil
}

func checkPriority(p *int) *errx.Error {
	if p != nil && (*p < 0 || *p > 65535) {
		return dnsInvalid("Priority is between 0 and 65535.")
	}
	return nil
}

func checkContent(c string) (string, *errx.Error) {
	c = strings.TrimSpace(c)
	if c == "" || len(c) > 2048 {
		return "", dnsInvalid("Enter the record's value.")
	}
	return c, nil
}

// zone is a domain whose records the store can change.
func (s *Service) zone(ctx context.Context, orgID, id uuid.UUID) (*models.StoreDomain, error) {
	d, err := s.domain(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	if !dnsWritable(d) {
		return nil, stateConflict("dns.write")
	}
	return d, nil
}

// AddDNSRecord adds a record of the workspace's own, a tracking CNAME or a verification TXT.
func (s *Service) AddDNSRecord(ctx context.Context, orgID, id uuid.UUID, in DNSRecordInput) (*DNSRecord, error) {
	d, err := s.zone(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	typ := strings.ToUpper(strings.TrimSpace(in.Type))
	if !dnsTypes[typ] {
		return nil, dnsInvalid("Pick a record type: A, AAAA, CNAME, MX, TXT, SRV or CAA.")
	}
	name, xerr := cleanRecordName(in.Name, d.Name)
	if xerr != nil {
		return nil, xerr
	}
	content, xerr := checkContent(in.Content)
	if xerr != nil {
		return nil, xerr
	}
	if xerr := checkTTL(in.TTL); xerr != nil {
		return nil, xerr
	}
	if xerr := checkPriority(in.Priority); xerr != nil {
		return nil, xerr
	}
	r, gerr := s.api().AddDNS(ctx, d.GraphKitDomainID, graphkit.DNSRecordInput{Type: typ, Name: name, Content: content, TTL: in.TTL, Priority: in.Priority}, opKey("dns.add", d.ID))
	if gerr != nil {
		return nil, s.upstream(ctx, "dns.add", gerr)
	}
	out := recordView(d.ID, *r)
	s.publish(ctx, orgID, pubsub.StoreEvent{BaseEvent: pubsub.BaseEvent{EventType: pubsub.EventStoreDomainUpdated}, ID: d.ID.String(), Status: d.Status})
	return &out, nil
}

// record finds the GraphKit record behind a Warmbly record id.
func (s *Service) record(ctx context.Context, d *models.StoreDomain, id uuid.UUID) (*graphkit.DNSRecord, error) {
	recs, gerr := s.api().ListDNS(ctx, d.GraphKitDomainID)
	if gerr != nil {
		return nil, s.upstream(ctx, "dns.read", gerr)
	}
	for i := range recs {
		if recordID(d.ID, recs[i].ID) == id {
			return &recs[i], nil
		}
	}
	return nil, errx.ErrNotFound
}

// UpdateDNSRecord changes one of the workspace's records in place; its type stays.
func (s *Service) UpdateDNSRecord(ctx context.Context, orgID, id, rid uuid.UUID, in DNSRecordPatch) (*DNSRecord, error) {
	d, err := s.zone(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	r, err := s.record(ctx, d, rid)
	if err != nil {
		return nil, err
	}
	if r.Managed {
		return nil, errx.NewWithIdentifier(errx.Conflict, CodeDNSRecordManaged, "Warmbly keeps this record for mail delivery, so it can't be changed here.")
	}
	var patch graphkit.DNSRecordPatch
	if in.Name != nil {
		name, xerr := cleanRecordName(*in.Name, d.Name)
		if xerr != nil {
			return nil, xerr
		}
		patch.Name = &name
	}
	if in.Content != nil {
		content, xerr := checkContent(*in.Content)
		if xerr != nil {
			return nil, xerr
		}
		patch.Content = &content
	}
	if in.TTL != nil {
		if xerr := checkTTL(*in.TTL); xerr != nil || *in.TTL == 0 {
			return nil, dnsInvalid("TTL is between 60 and 86400 seconds.")
		}
		patch.TTL = in.TTL
	}
	if xerr := checkPriority(in.Priority); xerr != nil {
		return nil, xerr
	}
	patch.Priority = in.Priority
	got, gerr := s.api().UpdateDNS(ctx, d.GraphKitDomainID, r.ID, patch, opKey("dns.update", rid))
	if gerr != nil {
		return nil, s.upstream(ctx, "dns.update", gerr)
	}
	out := recordView(d.ID, *got)
	s.publish(ctx, orgID, pubsub.StoreEvent{BaseEvent: pubsub.BaseEvent{EventType: pubsub.EventStoreDomainUpdated}, ID: d.ID.String(), Status: d.Status})
	return &out, nil
}

// DeleteDNSRecord deletes one of the workspace's records, or an imported one that blocks mail.
func (s *Service) DeleteDNSRecord(ctx context.Context, orgID, id, rid uuid.UUID) error {
	d, err := s.zone(ctx, orgID, id)
	if err != nil {
		return err
	}
	r, err := s.record(ctx, d, rid)
	if err != nil {
		return err
	}
	if r.Managed {
		return errx.NewWithIdentifier(errx.Conflict, CodeDNSRecordManaged, "Warmbly keeps this record for mail delivery, so it can't be deleted.")
	}
	if gerr := s.api().DeleteDNS(ctx, d.GraphKitDomainID, r.ID, opKey("dns.delete", rid)); gerr != nil && graphkit.CodeOf(gerr) != graphkit.CodeNotFound {
		return s.upstream(ctx, "dns.delete", gerr)
	}
	// Deleting a conflicting record clears the conflict; show that without waiting for the event.
	s.refreshDomain(ctx, d)
	return nil
}

// CheckDNS looks every record up in public DNS now.
func (s *Service) CheckDNS(ctx context.Context, orgID, id uuid.UUID) (*DNSCheck, error) {
	d, err := s.domain(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	c, gerr := s.api().CheckDNS(ctx, d.GraphKitDomainID, opKey("dns.check", d.ID))
	if gerr != nil {
		return nil, s.upstream(ctx, "dns.check", gerr)
	}
	out := &DNSCheck{Nameservers: nonNil(c.Nameservers), Records: []DNSRecord{}}
	for _, r := range c.Data {
		out.Records = append(out.Records, recordView(d.ID, r))
	}
	return out, nil
}

// ---- tracking host ------------------------------------------------------

// trackingRetry is how long a tracking setup that failed waits before the reconciler tries it again.
const trackingRetry = time.Hour

// setupTracking points the domain's mailboxes at a tracking host on the domain
// itself, writing its CNAME through the store, so the domain never needs the
// sending-domains setup a domain bought elsewhere does. Once per domain.
func (s *Service) setupTracking(ctx context.Context, d *models.StoreDomain) {
	if s.domains == nil || d.TrackingHost != "" || d.Status != graphkit.DomainActive || !dnsWritable(d) {
		return
	}
	s.mu.Lock()
	if s.trackingTried == nil {
		s.trackingTried = map[uuid.UUID]time.Time{}
	}
	if at, ok := s.trackingTried[d.ID]; ok && s.now().Sub(at) < trackingRetry {
		s.mu.Unlock()
		return
	}
	s.trackingTried[d.ID] = s.now()
	s.mu.Unlock()
	host := config.DefaultTrackingHost(d.Name)
	if _, _, xerr := s.domains.VendorTracking(ctx, d.OrganizationID, d.Name, host); xerr != nil {
		log.Info().Str("store_domain_id", d.ID.String()).Str("reason", xerr.ResponseCode()).Msg("store: tracking host not set up yet")
		return
	}
	if err := s.Repo.SetDomainTrackingHost(ctx, d.OrganizationID, d.ID, host); err != nil {
		log.Warn().Err(err).Str("store_domain_id", d.ID.String()).Msg("store: recording the tracking host failed")
		return
	}
	s.publish(ctx, d.OrganizationID, pubsub.StoreEvent{BaseEvent: pubsub.BaseEvent{EventType: pubsub.EventStoreDomainUpdated}, ID: d.ID.String(), Status: d.Status})
}

// trackMailbox gives a mailbox that just connected its domain's tracking host:
// the first one sets the host up for every mailbox on the domain, and a later one
// joins the host already there, leaving the others as they are.
func (s *Service) trackMailbox(ctx context.Context, d *models.StoreDomain, emailAccountID uuid.UUID) {
	if d.Status != graphkit.DomainActive {
		return
	}
	if d.TrackingHost == "" {
		s.setupTracking(ctx, d)
		return
	}
	if s.Mailboxes == nil {
		return
	}
	if _, xerr := s.Mailboxes.UpdateTrackingDomain(ctx, d.OrganizationID.String(), emailAccountID.String(), d.TrackingHost); xerr != nil {
		log.Info().Str("store_domain_id", d.ID.String()).Str("reason", xerr.ResponseCode()).Msg("store: tracking host not applied to a new mailbox")
	}
}

func (s *Service) setupTrackingDue(ctx context.Context) error {
	if s.domains == nil {
		return nil
	}
	rows, err := s.Repo.DomainsNeedingTracking(ctx, 20)
	if err != nil {
		return err
	}
	for i := range rows {
		s.setupTracking(ctx, &rows[i])
	}
	return nil
}
