package graphkit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
)

// The simulator's domains: registered ones, existing ones connected by
// nameservers or by records, and the zone records of both.

func (f *Fake) domainLocked(id string) (*fakeDomain, error) {
	d := f.domains[id]
	if d == nil || d.d.Status == DomainRemoved {
		return nil, fakeErr(http.StatusNotFound, CodeNotFound, "no such domain")
	}
	return d, nil
}

func (f *Fake) ListDomains(_ context.Context, externalID, _ string, _ int) (*Page[Domain], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.refusedLocked(); err != nil {
		return nil, err
	}
	f.advanceLocked()
	out := &Page[Domain]{Data: []Domain{}}
	for _, d := range f.domains {
		if d.external == externalID && d.d.Status != DomainRemoved {
			out.Data = append(out.Data, *cloneJSON(d.d))
		}
	}
	sort.Slice(out.Data, func(i, j int) bool { return out.Data[i].ID < out.Data[j].ID })
	return out, nil
}

func (f *Fake) GetDomain(_ context.Context, domainID string) (*Domain, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.refusedLocked(); err != nil {
		return nil, err
	}
	f.advanceLocked()
	d, err := f.domainLocked(domainID)
	if err != nil {
		return nil, err
	}
	return cloneJSON(d.d), nil
}

func notRegisteredHere(d *fakeDomain) error {
	if d.d.Registered() {
		return nil
	}
	return fakeErr(http.StatusConflict, CodeNotRegisteredHere, d.d.Domain+" is registered elsewhere")
}

func (f *Fake) UpdateDomain(_ context.Context, domainID string, patch DomainPatch, key string) (*Domain, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return replay(f, key, "domain:"+domainID, patch, func() (*Domain, error) {
		d, err := f.domainLocked(domainID)
		if err != nil {
			return nil, err
		}
		if patch.Privacy != nil {
			if err := notRegisteredHere(d); err != nil {
				return nil, err
			}
			d.d.Privacy = *patch.Privacy
		}
		switch {
		case patch.ClearForwarding:
			d.d.ForwardingURL, d.d.ForwardingStatus = nil, "none"
		case patch.ForwardingURL != nil:
			v := *patch.ForwardingURL
			// The sandbox's redirect and its certificate are live at once.
			d.d.ForwardingURL, d.d.ForwardingStatus = &v, "active"
		}
		if patch.DMARCPolicy != nil {
			d.d.DMARCPolicy = *patch.DMARCPolicy
		}
		if d.d.Status == DomainActive {
			f.publishManagedLocked(d, f.now().UTC())
		}
		f.emitLocked(EventDomainUpdated, d.external, d.d)
		return cloneJSON(d.d), nil
	})
}

func (f *Fake) RenewDomain(_ context.Context, domainID string, years int, expected time.Time, key string) (*Domain, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	body := map[string]any{"years": years, "expected_expires_at": expected.UTC().Format(time.RFC3339)}
	return replay(f, key, "renew:"+domainID, body, func() (*Domain, error) {
		d, err := f.domainLocked(domainID)
		if err != nil {
			return nil, err
		}
		if err := notRegisteredHere(d); err != nil {
			return nil, err
		}
		switch d.d.Status {
		case DomainActive, DomainExpired:
		case DomainRedemption:
			return nil, fakeErr(http.StatusUnprocessableEntity, CodeRenewalWindowClosed, "the renewal window closed")
		default:
			return nil, fakeErr(http.StatusConflict, CodeDomainUnavailable, "the domain cannot be renewed")
		}
		if f.renewing[domainID] {
			return nil, fakeErr(http.StatusConflict, CodeRenewalInProgress, "a renewal is already running")
		}
		if d.d.ExpiresAt == nil || !d.d.ExpiresAt.Equal(expected) {
			return nil, fakeErrDetails(http.StatusConflict, CodeExpectedExpiryMismatch, "the domain's expiry has moved", map[string]any{"domain": d.d})
		}
		now := f.now()
		if d.d.RenewableFrom != nil && now.Before(*d.d.RenewableFrom) {
			return nil, fakeErr(http.StatusUnprocessableEntity, CodeRenewalNotDue, "the renewal window has not opened")
		}
		if d.d.RenewableUntil != nil && now.After(*d.d.RenewableUntil) {
			return nil, fakeErr(http.StatusUnprocessableEntity, CodeRenewalWindowClosed, "the renewal window closed")
		}
		years = max(years, 1)
		cost := d.d.RenewMicros * int64(years)
		if f.balance < cost {
			return nil, fakeErr(http.StatusPaymentRequired, CodePartnerBalanceInsufficient, "partner balance is too low")
		}
		f.balance -= cost
		d.d.Status = DomainActive
		f.setExpiryLocked(d, d.d.ExpiresAt.AddDate(years, 0, 0))
		f.emitLocked(EventDomainRenewed, d.external, d.d)
		return cloneJSON(d.d), nil
	})
}

func (f *Fake) TransferOut(_ context.Context, domainID, key string) (*TransferOut, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return replay(f, key, "transfer:"+domainID, nil, func() (*TransferOut, error) {
		d, err := f.domainLocked(domainID)
		if err != nil {
			return nil, err
		}
		if err := notRegisteredHere(d); err != nil {
			return nil, err
		}
		if strings.Contains(d.d.Domain, "translock") {
			return nil, fakeErr(http.StatusConflict, CodeTransferLocked, "the domain cannot be transferred now")
		}
		sum := sha256.Sum256([]byte(domainID))
		code := "GK-" + strings.ToUpper(hex.EncodeToString(sum[:6]))
		if d.d.Status == DomainTransferring {
			return &TransferOut{AuthCode: code, UnlockedUntil: d.d.UnlockedUntil, Domain: cloneJSON(d.d)}, nil
		}
		if d.d.Status != DomainActive && d.d.Status != DomainExpired {
			return nil, fakeErr(http.StatusConflict, CodeTransferLocked, "the domain cannot be transferred now")
		}
		now := f.now().UTC()
		until := now.Add(14 * 24 * time.Hour)
		approve := now.Add(5 * 24 * time.Hour)
		d.d.Status, d.d.Locked, d.d.UnlockedUntil = DomainTransferring, false, &until
		d.d.Transfer = &TransferRequest{Status: "requested", RequestedAt: &now, AutoApproveAt: &approve}
		if strings.Contains(d.d.Domain, "transferaway") {
			done := now.Add(time.Minute)
			d.transfer = &done
		}
		f.emitLocked(EventDomainUpdated, d.external, d.d)
		f.emitLocked(EventDomainTransferRequested, d.external, d.d)
		return &TransferOut{AuthCode: code, UnlockedUntil: &until, Domain: cloneJSON(d.d)}, nil
	})
}

// assignZoneLocked gives a domain its own zone and the nameserver pair it was assigned.
func (f *Fake) assignZoneLocked(d *fakeDomain) {
	f.seq++
	d.zone = f.seq
	pair := fakeNameserverPairs[d.zone%len(fakeNameserverPairs)]
	d.d.Nameservers = []string{pair[0] + ".ns.cloudflare.com", pair[1] + ".ns.cloudflare.com"}
}

// publishManagedLocked writes the records GraphKit keeps for mail and forwarding, replacing its previous ones.
func (f *Fake) publishManagedLocked(d *fakeDomain, at time.Time) {
	keep := []DNSRecord{}
	for _, r := range f.dns[d.d.ID] {
		if !r.Managed || r.Purpose == "ownership" {
			keep = append(keep, r)
		}
	}
	f.dns[d.d.ID] = append(keep, fakeManagedRecords(d.d, at)...)
}

func fakeManagedRecords(d Domain, at time.Time) []DNSRecord {
	var out []DNSRecord
	rec := func(purpose, typ, name, content string, prio *int) {
		out = append(out, DNSRecord{ID: "dns_" + purpose + "_" + name + "_" + d.ID, Type: typ, Name: name, Content: content, TTL: 3600,
			Priority: prio, Managed: true, Purpose: purpose, CreatedAt: &at})
	}
	if d.Provider != "" {
		mx, spf, dkim := "smtp.google.com", "v=spf1 include:_spf.google.com ~all", "google._domainkey"
		if d.Provider == ProviderMicrosoft {
			mx = strings.ReplaceAll(d.Domain, ".", "-") + ".mail.protection.outlook.com"
			spf, dkim = "v=spf1 include:spf.protection.outlook.com -all", "selector1._domainkey"
		}
		one := 1
		rec("mx", "MX", "@", mx, &one)
		rec("spf", "TXT", "@", spf, nil)
		rec("dkim", "TXT", dkim, "v=DKIM1; k=rsa; p=MIIBIjANBg", nil)
	}
	policy := d.DMARCPolicy
	if policy == "" {
		policy = "none"
	}
	rec("dmarc", "TXT", "_dmarc", "v=DMARC1; p="+policy, nil)
	if d.ForwardingURL != nil {
		rec("forwarding", "A", "@", fakeRedirectIPs[0], nil)
		rec("forwarding", "AAAA", "@", fakeRedirectIPs[1], nil)
		rec("forwarding", "CNAME", "www", "redirect.example.net", nil)
	}
	return out
}

// advanceDomainsLocked moves registrant verification, transfers and existing domains' connections on.
func (f *Fake) advanceDomainsLocked(now time.Time) []Event {
	var out []Event
	ids := make([]string, 0, len(f.domains))
	for id := range f.domains {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		d := f.domains[id]
		if d.verifyAt != nil && !now.Before(*d.verifyAt) && d.d.RegistrantVerification != nil {
			d.verifyAt = nil
			d.d.RegistrantVerification.Status, d.d.RegistrantVerification.Deadline = "verified", nil
			f.verified[strings.ToLower(d.d.RegistrantVerification.Email)] = true
			out = append(out, f.emitLocked(EventDomainUpdated, d.external, d.d))
		}
		if d.d.Status == DomainTransferring {
			switch {
			case d.transfer != nil && !now.Before(*d.transfer):
				d.d.Status, d.transfer = DomainTransferredOut, nil
				out = append(out, f.emitLocked(EventDomainTransferredOut, d.external, d.d))
			case d.d.UnlockedUntil != nil && !now.Before(*d.d.UnlockedUntil):
				d.d.Status, d.d.Locked, d.d.UnlockedUntil, d.d.Transfer = DomainActive, true, nil, nil
				out = append(out, f.emitLocked(EventDomainUpdated, d.external, d.d))
			}
		}
		if d.d.Registered() || d.d.Status == DomainRemoved {
			continue
		}
		out = append(out, f.advanceConnectionLocked(d, now)...)
	}
	return out
}

func (f *Fake) advanceConnectionLocked(d *fakeDomain, now time.Time) []Event {
	switch d.d.ConnectionStatus {
	case ConnectionPending:
		if d.seenAt != nil && !now.Before(*d.seenAt) {
			d.seenAt = nil
			d.d.Status, d.d.ConnectionStatus, d.d.ConnectionExpiresAt, d.d.ConnectionCheckedAt = DomainActive, ConnectionActive, nil, &now
			d.d.CurrentNameservers = append([]string{}, d.d.Nameservers...)
			if strings.Contains(d.d.Domain, "nsmoved") {
				moves := now.Add(2 * time.Minute)
				d.movesAt = &moves
			}
			return []Event{f.emitLocked(EventDomainConnected, d.external, d.d)}
		}
		if d.d.ConnectionExpiresAt != nil && !now.Before(*d.d.ConnectionExpiresAt) {
			d.seenAt = nil
			d.d.ConnectionStatus, d.d.ConnectionCheckedAt = ConnectionExpired, &now
			return []Event{f.emitLocked(EventDomainDisconnected, d.external, d.d)}
		}
	case ConnectionActive:
		if d.movesAt != nil && !now.Before(*d.movesAt) {
			d.movesAt = nil
			d.d.ConnectionStatus, d.d.ConnectionCheckedAt = ConnectionMoved, &now
			d.d.CurrentNameservers = []string{"ns1.elsewhere.example", "ns2.elsewhere.example"}
			return []Event{f.emitLocked(EventDomainDisconnected, d.external, d.d)}
		}
	}
	return nil
}

var fakeDomainRe = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,24}$`)

// ConnectDomain adds a domain registered elsewhere, connected by nameservers or by records.
func (f *Fake) ConnectDomain(_ context.Context, externalID, domain, method, key string) (*Domain, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return replay(f, key, "connect:"+externalID, map[string]string{"domain": domain, "method": method}, func() (*Domain, error) {
		if f.accounts[externalID] == nil {
			return nil, fakeErr(http.StatusNotFound, CodeAccountNotFound, "no such account")
		}
		name := strings.ToLower(strings.TrimSpace(domain))
		if !fakeDomainRe.MatchString(name) {
			return nil, fakeErr(http.StatusBadRequest, CodeInvalidRequest, "domain is not a domain name")
		}
		switch method {
		case ConnectionNameservers, ConnectionManual:
		case ConnectionCloudflare:
			return nil, fakeErr(http.StatusBadRequest, CodeInvalidRequest, "dns_connection_id is required for cloudflare")
		default:
			return nil, fakeErr(http.StatusBadRequest, CodeInvalidRequest, "method is nameservers, cloudflare or manual")
		}
		if have := f.liveDomainLocked(name); have != nil {
			reason := "connected_elsewhere"
			switch {
			case have.external == externalID:
				reason = "already_added"
			case have.d.Registered():
				reason = "registered"
			}
			return nil, fakeErrDetails(http.StatusConflict, CodeDomainUnavailable, name+" is already on GraphKit", map[string]any{"reason": reason})
		}
		now := f.now().UTC()
		d := &fakeDomain{external: externalID, d: Domain{ID: f.id("dom"), AccountID: f.accounts[externalID].ID, Domain: name, DisplayName: name,
			Status: DomainConnecting, DMARCPolicy: f.settings.DMARCPolicyDefault, ForwardingStatus: "none", CreatedAt: now,
			Nameservers: []string{}, CurrentNameservers: []string{}, DNSConflicts: []DNSConflict{}, Connection: method, ConnectionCheckedAt: &now}}
		f.domains[d.d.ID] = d
		f.startConnectionLocked(d, now)
		f.emitLocked(EventDomainUpdated, externalID, d.d)
		return cloneJSON(d.d), nil
	})
}

// startConnectionLocked opens a fresh connection: a new zone with imported records, or a new ownership record.
func (f *Fake) startConnectionLocked(d *fakeDomain, now time.Time) {
	expires := now.Add(fakeConnectionTTL)
	if strings.Contains(d.d.Domain, "nsstuck") {
		expires = now.Add(2 * time.Minute)
		d.seenAt = nil
	} else {
		seen := now.Add(fakeSeenAfter)
		d.seenAt = &seen
	}
	d.d.ConnectionStatus, d.d.ConnectionExpiresAt = ConnectionPending, &expires
	d.d.DNSConflicts = []DNSConflict{}
	if d.d.Connection == ConnectionManual {
		d.d.DNSHost, d.d.Nameservers = DNSHostCustomer, []string{}
		token := hex.EncodeToString([]byte(f.id("own")))[:32]
		f.dns[d.d.ID] = []DNSRecord{{ID: f.id("dns"), Type: "TXT", Name: "@", Content: "domain-verification=" + token, TTL: 3600, Managed: true, Purpose: "ownership", CreatedAt: &now}}
		return
	}
	d.d.DNSHost = DNSHostGraphKit
	f.assignZoneLocked(d)
	recs := []DNSRecord{
		{ID: f.id("dns"), Type: "A", Name: "@", Content: "192.0.2.10", TTL: 300, Purpose: "imported", CreatedAt: &now},
		{ID: f.id("dns"), Type: "CNAME", Name: "www", Content: d.d.Domain, TTL: 300, Purpose: "imported", CreatedAt: &now},
	}
	if strings.Contains(d.d.Domain, "hasmail") {
		ten := 10
		mail := []DNSRecord{
			{ID: f.id("dns"), Type: "MX", Name: "@", Content: "mx1.mailhost.example", TTL: 300, Priority: &ten, Purpose: "imported", CreatedAt: &now},
			{ID: f.id("dns"), Type: "TXT", Name: "@", Content: "v=spf1 include:mailhost.example ~all", TTL: 300, Purpose: "imported", CreatedAt: &now},
			{ID: f.id("dns"), Type: "TXT", Name: "_dmarc", Content: "v=DMARC1; p=quarantine;", TTL: 300, Purpose: "imported", CreatedAt: &now},
		}
		for _, r := range mail {
			id := r.ID
			d.d.DNSConflicts = append(d.d.DNSConflicts, DNSConflict{Type: r.Type, Name: r.Name, Content: r.Content, Reason: "clashes with the mail records GraphKit publishes", RecordID: &id})
		}
		recs = append(recs, mail...)
	}
	f.dns[d.d.ID] = recs
}

func (f *Fake) existingLocked(id string) (*fakeDomain, error) {
	d, err := f.domainLocked(id)
	if err != nil {
		return nil, err
	}
	if d.d.Registered() {
		return nil, fakeErr(http.StatusConflict, CodeStateConflict, "the domain is registered here")
	}
	return d, nil
}

func (f *Fake) CheckConnection(_ context.Context, domainID, key string) (*Domain, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.refusedLocked(); err != nil {
		return nil, err
	}
	if key == "" {
		return nil, fakeErr(http.StatusBadRequest, CodeInvalidRequest, "Idempotency-Key is required")
	}
	d, err := f.existingLocked(domainID)
	if err != nil {
		return nil, err
	}
	now := f.now().UTC()
	f.advanceConnectionLocked(d, now)
	d.d.ConnectionCheckedAt = &now
	return cloneJSON(d.d), nil
}

func (f *Fake) RegenerateConnection(_ context.Context, domainID, key string) (*Domain, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return replay(f, key, "regenerate:"+domainID, nil, func() (*Domain, error) {
		d, err := f.existingLocked(domainID)
		if err != nil {
			return nil, err
		}
		if d.d.ConnectionStatus == ConnectionActive {
			return nil, fakeErr(http.StatusConflict, CodeStateConflict, "the domain is connected")
		}
		now := f.now().UTC()
		d.d.Status = DomainConnecting
		f.startConnectionLocked(d, now)
		f.emitLocked(EventDomainUpdated, d.external, d.d)
		return cloneJSON(d.d), nil
	})
}

func (f *Fake) RemoveDomain(_ context.Context, domainID, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, err := replay(f, key, "remove:"+domainID, nil, func() (*struct{}, error) {
		d, err := f.existingLocked(domainID)
		if err != nil {
			return nil, err
		}
		if f.mailboxCountLocked(d.d.ID) > 0 {
			return nil, fakeErr(http.StatusConflict, CodeStateConflict, "the domain has mailboxes")
		}
		d.d.Status, d.d.ConnectionStatus = DomainRemoved, ConnectionExpired
		delete(f.dns, d.d.ID)
		f.emitLocked(EventDomainDisconnected, d.external, d.d)
		return &struct{}{}, nil
	})
	return err
}

// ---- records ------------------------------------------------------------

var fakeRecordTypes = []string{"A", "AAAA", "CNAME", "MX", "TXT", "SRV", "CAA"}

func (f *Fake) ListDNS(_ context.Context, domainID string) ([]DNSRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.refusedLocked(); err != nil {
		return nil, err
	}
	f.advanceLocked()
	if _, err := f.domainLocked(domainID); err != nil {
		return nil, err
	}
	return append([]DNSRecord{}, f.dns[domainID]...), nil
}

// clashLocked reports a record the managed mail records or CNAME rules forbid.
func (f *Fake) clashLocked(domainID, skipID, typ, name, content string) bool {
	for _, r := range f.dns[domainID] {
		if r.ID == skipID {
			continue
		}
		if r.Name == name && (typ == "CNAME" || r.Type == "CNAME") {
			return true
		}
		if !r.Managed {
			continue
		}
		switch {
		case typ == "MX" && r.Type == "MX":
			return true
		case typ == "TXT" && name == "@" && r.Purpose == "spf" && strings.HasPrefix(strings.ToLower(content), "v=spf1"):
			return true
		case r.Name == name && (r.Purpose == "dmarc" || r.Purpose == "dkim"):
			return true
		}
	}
	return false
}

func validTTL(ttl int) bool { return ttl == 0 || (ttl >= 60 && ttl <= 86400) }

func (f *Fake) zoneWritableLocked(domainID string) (*fakeDomain, error) {
	d, err := f.domainLocked(domainID)
	if err != nil {
		return nil, err
	}
	if d.d.DNSHost == DNSHostCustomer && d.d.Connection == ConnectionManual {
		return nil, fakeErr(http.StatusConflict, CodeStateConflict, "This domain's DNS is hosted elsewhere: add the record at its DNS provider.")
	}
	return d, nil
}

func (f *Fake) AddDNS(_ context.Context, domainID string, rec DNSRecordInput, key string) (*DNSRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return replay(f, key, "dns:"+domainID, rec, func() (*DNSRecord, error) {
		if _, err := f.zoneWritableLocked(domainID); err != nil {
			return nil, err
		}
		rec.Type = strings.ToUpper(rec.Type)
		if !slices.Contains(fakeRecordTypes, rec.Type) || strings.TrimSpace(rec.Name) == "" || strings.TrimSpace(rec.Content) == "" || !validTTL(rec.TTL) {
			return nil, fakeErr(http.StatusBadRequest, CodeInvalidRequest, "the record is not valid")
		}
		if len(f.dns[domainID]) >= 200 {
			return nil, fakeErr(http.StatusUnprocessableEntity, CodeInvalidRequest, "a domain holds at most 200 records")
		}
		if f.clashLocked(domainID, "", rec.Type, rec.Name, rec.Content) {
			return nil, fakeErr(http.StatusConflict, CodeDNSRecordConflict, "the record clashes with a managed one")
		}
		now := f.now().UTC()
		ttl := rec.TTL
		if ttl == 0 {
			ttl = 3600
		}
		r := DNSRecord{ID: f.id("dns"), Type: rec.Type, Name: rec.Name, Content: rec.Content, TTL: ttl, Priority: rec.Priority, CreatedAt: &now}
		f.dns[domainID] = append(f.dns[domainID], r)
		return &r, nil
	})
}

func (f *Fake) UpdateDNS(_ context.Context, domainID, recordID string, patch DNSRecordPatch, key string) (*DNSRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return replay(f, key, "dnsput:"+domainID+"/"+recordID, patch, func() (*DNSRecord, error) {
		if _, err := f.zoneWritableLocked(domainID); err != nil {
			return nil, err
		}
		list := f.dns[domainID]
		for i := range list {
			r := &list[i]
			if r.ID != recordID {
				continue
			}
			if r.Managed {
				return nil, fakeErr(http.StatusConflict, CodeDNSRecordManaged, "managed records cannot be changed")
			}
			next := *r
			if patch.Name != nil {
				next.Name = *patch.Name
			}
			if patch.Content != nil {
				next.Content = *patch.Content
			}
			if patch.TTL != nil {
				next.TTL = *patch.TTL
			}
			if patch.Priority != nil {
				next.Priority = patch.Priority
			}
			if next.Name == "" || next.Content == "" || !validTTL(next.TTL) {
				return nil, fakeErr(http.StatusBadRequest, CodeInvalidRequest, "the record is not valid")
			}
			if f.clashLocked(domainID, r.ID, next.Type, next.Name, next.Content) {
				return nil, fakeErr(http.StatusConflict, CodeDNSRecordConflict, "the record clashes with a managed one")
			}
			*r = next
			out := *r
			return &out, nil
		}
		return nil, fakeErr(http.StatusNotFound, CodeNotFound, "no such record")
	})
}

func (f *Fake) DeleteDNS(_ context.Context, domainID, recordID, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, err := replay(f, key, "dnsdel:"+domainID+"/"+recordID, nil, func() (*struct{}, error) {
		d, err := f.domainLocked(domainID)
		if err != nil {
			return nil, err
		}
		list := f.dns[domainID]
		for i, r := range list {
			if r.ID != recordID {
				continue
			}
			if r.Managed {
				return nil, fakeErr(http.StatusConflict, CodeDNSRecordManaged, "managed records cannot be deleted")
			}
			f.dns[domainID] = append(list[:i:i], list[i+1:]...)
			kept := d.d.DNSConflicts[:0]
			for _, c := range d.d.DNSConflicts {
				if c.RecordID == nil || *c.RecordID != recordID {
					kept = append(kept, c)
				}
			}
			if len(kept) != len(d.d.DNSConflicts) {
				d.d.DNSConflicts = kept
				f.emitLocked(EventDomainUpdated, d.external, d.d)
			}
			return &struct{}{}, nil
		}
		return nil, fakeErr(http.StatusNotFound, CodeNotFound, "no such record")
	})
	return err
}

// checkLocked looks every record up: in the sandbox a record is served once its domain is connected.
func (f *Fake) checkLocked(d *fakeDomain) *DNSCheck {
	now := f.now().UTC()
	served := d.d.Status == DomainActive && (d.d.Registered() || d.d.ConnectionStatus == ConnectionActive)
	out := &DNSCheck{Data: []DNSRecord{}, Nameservers: append([]string{}, d.d.Nameservers...), CheckedAt: &now}
	for _, r := range f.dns[d.d.ID] {
		r.Status = "missing"
		if served || (r.Purpose == "ownership" && d.d.ConnectionStatus == ConnectionActive) {
			r.Status = "ok"
		}
		out.Data = append(out.Data, r)
	}
	return out
}

func (f *Fake) CheckDNS(_ context.Context, domainID, key string) (*DNSCheck, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.refusedLocked(); err != nil {
		return nil, err
	}
	if key == "" {
		return nil, fakeErr(http.StatusBadRequest, CodeInvalidRequest, "Idempotency-Key is required")
	}
	f.advanceLocked()
	d, err := f.domainLocked(domainID)
	if err != nil {
		return nil, err
	}
	return f.checkLocked(d), nil
}

func (f *Fake) RepairDNS(_ context.Context, domainID, key string) (*DNSCheck, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return replay(f, key, "repair:"+domainID, nil, func() (*DNSCheck, error) {
		d, err := f.zoneWritableLocked(domainID)
		if err != nil {
			return nil, err
		}
		if d.d.Status == DomainActive {
			f.publishManagedLocked(d, f.now().UTC())
		}
		return f.checkLocked(d), nil
	})
}
