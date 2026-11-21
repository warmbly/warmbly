package graphkit

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

// ---- quotes and orders --------------------------------------------------

func (f *Fake) accountDomainLocked(external, name string) *fakeDomain {
	if d := f.liveDomainLocked(name); d != nil && d.external == external {
		return d
	}
	return nil
}

func (f *Fake) mailboxCountLocked(domainID string) int {
	n := 0
	for _, m := range f.mailboxes {
		if m.m.DomainID == domainID && m.m.Status != MailboxCancelled && m.m.Status != MailboxFailed {
			n++
		}
	}
	return n
}

// hasAdminLocked reports a domain that already has a live admin mailbox.
func (f *Fake) hasAdminLocked(domainID string) bool {
	for _, m := range f.mailboxes {
		if m.m.DomainID == domainID && m.m.IsAdmin && m.m.Status != MailboxCancelled && m.m.Status != MailboxFailed {
			return true
		}
	}
	return false
}

// adminOf is the mailbox that becomes the admin: the flagged one, else the first.
func adminOf(boxes []NewMailbox) int {
	for i, m := range boxes {
		if m.Admin {
			return i
		}
	}
	return 0
}

func (f *Fake) CreateQuote(_ context.Context, externalID string, items []QuoteItem, key string) (*Quote, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return replay(f, key, "quote:"+externalID, items, func() (*Quote, error) {
		acct := f.accounts[externalID]
		if acct == nil {
			return nil, fakeErr(http.StatusNotFound, CodeAccountNotFound, "no such account")
		}
		if len(items) == 0 {
			return nil, fakeErr(http.StatusBadRequest, CodeInvalidRequest, "items are required")
		}
		now := f.now().UTC()
		q := Quote{ID: f.id("qt"), AccountID: acct.ID, CreatedAt: now, ExpiresAt: now.Add(15 * time.Minute), Currency: "USD"}
		newDomains := map[string]bool{}
		for _, it := range items {
			if it.Type != ItemDomain {
				continue
			}
			if bad := contactProblems(acct.Registrant); len(bad) > 0 {
				return nil, fakeErrDetails(http.StatusUnprocessableEntity, CodeContactInvalid, "registrant is incomplete", map[string]any{"fields": bad})
			}
			name := strings.ToLower(it.Domain)
			o := f.offerLocked(name, true)
			switch {
			case o.Reason != nil && *o.Reason == "tld_unsupported":
				return nil, fakeErr(http.StatusUnprocessableEntity, CodeTLDUnsupported, name+" is not offered")
			case o.Premium:
				return nil, fakeErr(http.StatusUnprocessableEntity, CodeDomainPremium, name+" is a premium name")
			case !o.Available || newDomains[name]:
				return nil, fakeErr(http.StatusConflict, CodeDomainUnavailable, name+" is not available")
			}
			years := it.Years
			if years == 0 {
				years = fakeMinYearsFor(o.TLD)
			}
			if years < fakeMinYearsFor(o.TLD) || years > fakeMaxYearsFor(o.TLD) {
				return nil, fakeErr(http.StatusBadRequest, CodeInvalidRequest, fmt.Sprintf(".%s is registered for %d to %d years", o.TLD, fakeMinYearsFor(o.TLD), fakeMaxYearsFor(o.TLD)))
			}
			newDomains[name] = true
			q.Lines = append(q.Lines, QuoteLine{Type: ItemDomain, Domain: name, Years: years, AmountMicros: *o.RegisterMicros * int64(years), RenewMicros: *o.RenewMicros})
			q.DueNowMicros += *o.RegisterMicros * int64(years)
		}
		taken := map[string]bool{}
		perDomain := map[string]int{}
		providers := map[string]string{}
		for i, it := range items {
			if it.Type != ItemMailboxes {
				continue
			}
			name := strings.ToLower(it.Domain)
			if it.Provider != ProviderGoogle && it.Provider != ProviderMicrosoft {
				return nil, fakeErr(http.StatusBadRequest, CodeInvalidRequest, "provider must be google or microsoft")
			}
			flags := 0
			for _, m := range it.Mailboxes {
				if m.Admin {
					flags++
				}
			}
			if flags > 1 {
				return nil, fakeErr(http.StatusBadRequest, CodeInvalidRequest, "at most one mailbox per domain is the admin")
			}
			existing := 0
			line := QuoteLine{Type: ItemMailboxes, Domain: name, Provider: it.Provider, Count: len(it.Mailboxes), MonthlyMicros: fakeMailboxMonthly}
			hasAdmin := false
			if !newDomains[name] {
				d := f.accountDomainLocked(externalID, name)
				if d == nil {
					return nil, fakeErr(http.StatusNotFound, CodeNotFound, name+" is not on this account")
				}
				if d.d.Status != DomainActive || (!d.d.Registered() && d.d.ConnectionStatus != ConnectionActive) {
					return nil, fakeErr(http.StatusConflict, CodeDomainNotReady, name+" is not active")
				}
				if len(d.d.DNSConflicts) > 0 {
					return nil, fakeErrDetails(http.StatusConflict, CodeDNSRecordConflict, name+" has records that clash with the mail records", map[string]any{"conflicts": d.d.DNSConflicts})
				}
				if d.d.Provider != "" && d.d.Provider != it.Provider {
					return nil, fakeErr(http.StatusUnprocessableEntity, CodeProviderMixed, "one mail provider per domain")
				}
				existing = f.mailboxCountLocked(d.d.ID)
				hasAdmin = f.hasAdminLocked(d.d.ID)
				line.NameserversChange = !d.d.Registered() && strings.Contains(name, "providerdns")
				for _, m := range f.mailboxes {
					if m.m.DomainID == d.d.ID && m.m.Status != MailboxCancelled {
						taken[m.m.Email] = true
					}
				}
			}
			if hasAdmin && flags > 0 {
				return nil, fakeErr(http.StatusUnprocessableEntity, CodeInvalidRequest, name+" already has an admin mailbox")
			}
			if p, ok := providers[name]; ok && p != it.Provider {
				return nil, fakeErr(http.StatusUnprocessableEntity, CodeProviderMixed, "one mail provider per domain")
			}
			providers[name] = it.Provider
			for j, m := range it.Mailboxes {
				// As the sandbox does: both names are required.
				for field, v := range map[string]string{"first_name": m.FirstName, "last_name": m.LastName} {
					if n := len([]rune(strings.TrimSpace(v))); n < 1 || n > 64 {
						return nil, fakeErrDetails(http.StatusBadRequest, CodeInvalidRequest, field+" must be 1-64 characters.",
							map[string]any{"field": fmt.Sprintf("items[%d].mailboxes[%d].%s", i, j, field)})
					}
				}
				lp := strings.ToLower(m.LocalPart)
				if !fakeLocal.MatchString(lp) {
					return nil, fakeErrDetails(http.StatusBadRequest, CodeLocalPartInvalid, lp+" is not a valid address", map[string]any{"local_part": lp})
				}
				if taken[lp+"@"+name] {
					return nil, fakeErrDetails(http.StatusConflict, CodeMailboxExists, lp+"@"+name+" already exists", map[string]any{"local_part": lp})
				}
				taken[lp+"@"+name] = true
				line.Emails = append(line.Emails, lp+"@"+name)
			}
			if !hasAdmin && len(line.Emails) > 0 {
				line.AdminEmail = line.Emails[adminOf(it.Mailboxes)]
			}
			perDomain[name] += len(it.Mailboxes)
			if existing+perDomain[name] > 5 {
				return nil, fakeErr(http.StatusUnprocessableEntity, CodeMailboxLimit, "at most 5 mailboxes per domain")
			}
			line.AmountMicros = int64(len(it.Mailboxes)) * fakeMailboxMonthly
			q.Lines = append(q.Lines, line)
			q.DueNowMicros += line.AmountMicros
			q.RecurringMonthlyMicros += line.AmountMicros
		}
		f.quotes[q.ID] = &fakeQuote{external: externalID, quote: q, items: items}
		return cloneJSON(q), nil
	})
}

func (f *Fake) GetQuote(_ context.Context, externalID, quoteID string) (*Quote, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.refusedLocked(); err != nil {
		return nil, err
	}
	fq := f.quotes[quoteID]
	if fq == nil || fq.external != externalID {
		return nil, fakeErr(http.StatusNotFound, CodeNotFound, "no such quote")
	}
	return cloneJSON(fq.quote), nil
}

func (f *Fake) CreateOrder(_ context.Context, externalID, quoteID, forwardingURL, key string) (*Order, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return replay(f, key, "order:"+externalID, map[string]string{"quote_id": quoteID, "forwarding_url": forwardingURL}, func() (*Order, error) {
		fq := f.quotes[quoteID]
		if fq == nil {
			return nil, fakeErr(http.StatusNotFound, CodeNotFound, "no such quote")
		}
		if fq.external != externalID {
			return nil, fakeErr(http.StatusConflict, CodeQuoteMismatch, "the quote belongs to another account")
		}
		if fq.quote.OrderID != nil {
			return nil, fakeErrDetails(http.StatusConflict, CodeQuoteMismatch, "the quote was already ordered", map[string]any{"order_id": *fq.quote.OrderID})
		}
		if f.now().After(fq.quote.ExpiresAt) {
			return nil, fakeErr(http.StatusUnprocessableEntity, CodeQuoteExpired, "the quote expired")
		}
		if f.balance < fq.quote.DueNowMicros {
			return nil, fakeErr(http.StatusPaymentRequired, CodePartnerBalanceInsufficient, "partner balance is too low")
		}
		f.balance -= fq.quote.DueNowMicros
		now := f.now().UTC()
		o := &fakeOrder{external: externalID, last: -1}
		o.order = Order{ID: f.id("ord"), AccountID: f.accounts[externalID].ID, QuoteID: quoteID, Status: OrderProcessing, DueNowMicros: fq.quote.DueNowMicros, CreatedAt: now}
		fq.quote.OrderID = &o.order.ID
		var fwd *string
		if forwardingURL != "" {
			fwd = &forwardingURL
			o.order.ForwardingURL = fwd
		}
		domainIDs := map[string]string{}
		for _, line := range fq.quote.Lines {
			if line.Type != ItemDomain {
				continue
			}
			d := &fakeDomain{external: externalID, d: Domain{ID: f.id("dom"), AccountID: o.order.AccountID, Domain: line.Domain, DisplayName: line.Domain,
				Status: DomainRegistering, RenewMicros: fakeTLDPrices[tldOf(line.Domain)], Privacy: true, Locked: true, ForwardingURL: fwd, DMARCPolicy: f.settings.DMARCPolicyDefault,
				CreatedAt: now, MailAuth: MailAuth{MX: "pending", SPF: "pending", DKIM: "pending", DMARC: "pending"}, Nameservers: []string{}, ForwardingStatus: "none",
				Connection: ConnectionRegistered, ConnectionStatus: ConnectionActive, CurrentNameservers: []string{}, DNSHost: DNSHostGraphKit, DNSConflicts: []DNSConflict{}}}
			f.domains[d.d.ID] = d
			domainIDs[line.Domain] = d.d.ID
			o.domains = append(o.domains, d.d.ID)
			o.order.Items = append(o.order.Items, OrderItem{Type: ItemDomain, Domain: line.Domain, Status: "registering", DomainID: d.d.ID, Years: line.Years, AmountMicros: line.AmountMicros})
		}
		for _, line := range fq.quote.Lines {
			if line.Type != ItemMailboxes {
				continue
			}
			domainID := domainIDs[line.Domain]
			if domainID == "" {
				domainID = f.accountDomainLocked(externalID, line.Domain).d.ID
			}
			f.domains[domainID].d.Provider = line.Provider
			item := OrderItem{Type: ItemMailboxes, Domain: line.Domain, Status: "waiting_for_domain", Provider: line.Provider, AmountMicros: line.AmountMicros, DomainID: domainID}
			var names []NewMailbox
			for _, it := range fq.items {
				if it.Type == ItemMailboxes && strings.EqualFold(it.Domain, line.Domain) {
					names = append(names, it.Mailboxes...)
				}
			}
			for i, email := range line.Emails {
				var nm NewMailbox
				if i < len(names) {
					nm = names[i]
				}
				m := &fakeMailbox{external: externalID, password: "sandbox-" + f.id("pw"), m: Mailbox{ID: f.id("mbx"), AccountID: o.order.AccountID, DomainID: domainID,
					Email: email, Provider: line.Provider, Status: MailboxProvisioning, FirstName: nm.FirstName, LastName: nm.LastName,
					IsAdmin: email == line.AdminEmail, MonthlyMicros: fakeMailboxMonthly, CreatedAt: now,
					Connection: Connection{Method: ConnectDelegation}}}
				f.mailboxes[m.m.ID] = m
				item.MailboxIDs = append(item.MailboxIDs, m.m.ID)
				o.boxes = append(o.boxes, m.m.ID)
			}
			o.order.Items = append(o.order.Items, item)
		}
		for i, k := range StepKeys {
			st := OrderStep{Key: k, Status: StepPending}
			switch {
			case i == 0 && len(o.domains) == 0:
				st.Status = StepSkipped
			case i > 0 && len(o.boxes) == 0 && k != "dns" && k != "ready":
				st.Status = StepSkipped
			}
			o.order.Steps = append(o.order.Steps, st)
		}
		f.startNextLocked(o, now)
		f.orders[o.order.ID] = o
		return cloneJSON(o.order), nil
	})
}

func tldOf(name string) string {
	_, tld := splitDomain(name)
	return tld
}

// connectionLocked is how a mailbox connects once it is set up: the account's
// identity for the provider, else the partner's, with the sandbox's magic labels on top.
func (f *Fake) connectionLocked(external, provider, domain string) Connection {
	if strings.Contains(domain, "pwfallback") {
		return Connection{Method: ConnectPassword, Ready: true}
	}
	acct, partner := f.accountDel[external], f.delegation
	if provider == ProviderGoogle {
		if strings.Contains(domain, "tokenmode") {
			return Connection{Method: ConnectToken, Ready: true}
		}
		if acct != nil && acct.Google != nil {
			if acct.Google.ClientID == "" {
				return Connection{Method: ConnectToken, Ready: true}
			}
			return Connection{Method: ConnectDelegation, Ready: true}
		}
		return Connection{Method: ConnectDelegation, Ready: partner != nil && partner.Google != nil}
	}
	if acct != nil && acct.Microsoft != nil {
		return Connection{Method: ConnectDelegation, Ready: true}
	}
	return Connection{Method: ConnectDelegation, Ready: partner != nil && partner.Microsoft != nil}
}

// reconnectLocked applies changed identities to every live mailbox, as GraphKit's background job does.
func (f *Fake) reconnectLocked(external string) {
	for _, m := range f.mailboxes {
		if (external != "" && m.external != external) || (m.m.Status != MailboxActive && m.m.Status != MailboxCancelling && m.m.Status != MailboxSuspended) {
			continue
		}
		d := f.domains[m.m.DomainID]
		if d == nil {
			continue
		}
		c := f.connectionLocked(m.external, m.m.Provider, d.d.Domain)
		if c != m.m.Connection {
			m.m.Connection = c
			f.emitLocked(EventMailboxUpdated, m.external, m.m)
		}
	}
}

// startNextLocked marks the first pending step running.
func (f *Fake) startNextLocked(o *fakeOrder, at time.Time) {
	for i := o.last + 1; i < len(o.order.Steps); i++ {
		if o.order.Steps[i].Status == StepPending {
			o.order.Steps[i].Status, o.order.Steps[i].At = StepRunning, &at
			return
		}
	}
}

// advanceLocked moves everything time has reached and records the events.
func (f *Fake) advanceLocked() []Event {
	now := f.now().UTC()
	var out []Event
	ids := make([]string, 0, len(f.orders))
	for id := range f.orders {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		out = append(out, f.advanceOrderLocked(f.orders[id], now)...)
	}
	for _, m := range f.mailboxes {
		if m.m.Status == MailboxCancelling && m.m.CancelsAt != nil && !now.Before(*m.m.CancelsAt) {
			m.m.Status = MailboxCancelled
			m.m.NextPeriodMonthlyMicros = nil
			out = append(out, f.emitLocked(EventMailboxCancelled, m.external, m.m))
		}
		if m.m.Status == MailboxActive && m.m.CurrentPeriodEnd != nil && !now.Before(*m.m.CurrentPeriodEnd) {
			// A new period starts at the price announced for it.
			next := m.m.CurrentPeriodEnd.AddDate(0, 1, 0)
			m.m.CurrentPeriodEnd = &next
			if m.m.NextPeriodMonthlyMicros != nil {
				m.m.MonthlyMicros = *m.m.NextPeriodMonthlyMicros
			}
			f.balance -= m.m.MonthlyMicros
		}
	}
	out = append(out, f.advanceDomainsLocked(now)...)
	out = append(out, f.advanceSocialLocked(now)...)
	return out
}

func (f *Fake) advanceOrderLocked(o *fakeOrder, now time.Time) []Event {
	if o.order.Status != OrderProcessing {
		return nil
	}
	var out []Event
	elapsed := now.Sub(o.order.CreatedAt)
	for o.last+1 < len(fakeStepAt) && elapsed >= fakeStepAt[o.last+1] {
		o.last++
		at := o.order.CreatedAt.Add(fakeStepAt[o.last])
		out = append(out, f.completeStepLocked(o, o.last, at)...)
		if o.order.Status != OrderProcessing {
			break
		}
		f.startNextLocked(o, at)
		out = append(out, f.emitLocked(EventOrderUpdated, o.external, o.order))
	}
	return out
}

// orderDomainsLocked is every domain an order works on: the ones it registers and the existing ones it adds mailboxes to.
func (f *Fake) orderDomainsLocked(o *fakeOrder) []*fakeDomain {
	seen := map[string]bool{}
	var out []*fakeDomain
	for _, id := range o.domains {
		seen[id] = true
		out = append(out, f.domains[id])
	}
	for _, it := range o.order.Items {
		if it.Type == ItemMailboxes && it.DomainID != "" && !seen[it.DomainID] {
			seen[it.DomainID] = true
			out = append(out, f.domains[it.DomainID])
		}
	}
	return out
}

func (f *Fake) completeStepLocked(o *fakeOrder, step int, at time.Time) []Event {
	var out []Event
	st := &o.order.Steps[step]
	if st.Status == StepSkipped {
		return nil
	}
	st.Status, st.At = StepDone, &at
	key := StepKeys[step]
	failing := func(magic string) bool {
		for _, d := range f.orderDomainsLocked(o) {
			if strings.Contains(d.d.Domain, magic) {
				return true
			}
		}
		return false
	}
	switch key {
	case "register":
		for _, id := range o.domains {
			d := f.domains[id]
			if strings.Contains(d.d.Domain, "fail-register") {
				d.d.Status = DomainFailed
				f.failItemsLocked(o, d.d.Domain, true)
				out = append(out, f.emitLocked(EventDomainFailed, o.external, d.d))
				continue
			}
			reg := at
			d.d.Status, d.d.RegisteredAt = DomainActive, &reg
			f.setExpiryLocked(d, at.AddDate(1, 0, 0))
			f.setDomainItemLocked(o, id, "configuring")
			out = append(out, f.emitLocked(EventDomainRegistered, o.external, d.d))
			out = append(out, f.verifyRegistrantLocked(d, at)...)
		}
		if failing("fail-register") && f.allItemsFailedLocked(o) {
			st.Status = StepFailed
		}
	case "dns":
		for _, d := range f.orderDomainsLocked(o) {
			if d.d.Status != DomainActive {
				continue
			}
			if d.d.Registered() && len(d.d.Nameservers) == 0 {
				f.assignZoneLocked(d)
				d.d.CurrentNameservers = append([]string{}, d.d.Nameservers...)
			}
			if d.d.ForwardingURL != nil {
				d.d.ForwardingStatus = "active"
			}
			f.publishManagedLocked(d, at)
			f.setDomainItemLocked(o, d.d.ID, "active")
			out = append(out, f.emitLocked(EventDomainUpdated, o.external, d.d))
		}
		if failing("fail-dns") {
			f.failMailboxesLocked(o, "fail-dns")
			st.Status = StepFailed
		}
	case "mail_auth":
		for _, d := range f.orderDomainsLocked(o) {
			if d.d.Status == DomainActive && !strings.Contains(d.d.Domain, "fail-mailauth") {
				d.d.MailAuth = MailAuth{MX: "ok", SPF: "ok", DKIM: "ok", DMARC: "ok", CheckedAt: &at}
				out = append(out, f.emitLocked(EventDomainUpdated, o.external, d.d))
			}
		}
		if failing("fail-mailauth") {
			f.failMailboxesLocked(o, "fail-mailauth")
			st.Status = StepFailed
		}
	case "mailboxes":
		if failing("fail-mailboxes") {
			f.failMailboxesLocked(o, "fail-mailboxes")
		}
		for i := range o.order.Items {
			if o.order.Items[i].Type == ItemMailboxes && o.order.Items[i].Status != "failed" {
				o.order.Items[i].Status = "connecting"
			}
		}
		live := 0
		for _, id := range o.boxes {
			if f.mailboxes[id].m.Status == MailboxProvisioning {
				live++
				out = append(out, f.emitLocked(EventMailboxProvisioning, o.external, f.mailboxes[id].m))
			}
		}
		if live == 0 && len(o.boxes) > 0 {
			st.Status = StepFailed
		}
	case "delegation":
		end := at.AddDate(0, 1, 0)
		live := 0
		for _, id := range o.boxes {
			m := f.mailboxes[id]
			if m.m.Status != MailboxProvisioning {
				continue
			}
			live++
			monthly := m.m.MonthlyMicros
			m.m.Status, m.m.CurrentPeriodEnd, m.m.NextPeriodMonthlyMicros = MailboxActive, &end, &monthly
			m.m.Connection = f.connectionLocked(m.external, m.m.Provider, f.domains[m.m.DomainID].d.Domain)
			out = append(out, f.emitLocked(EventMailboxActive, o.external, m.m))
		}
		if live == 0 && len(o.boxes) > 0 {
			st.Status = StepFailed
		}
		for _, d := range f.orderDomainsLocked(o) {
			if d.d.Status != DomainActive {
				continue
			}
			d.d.MailboxCount = f.mailboxCountLocked(d.d.ID)
			if !d.d.Registered() && strings.Contains(d.d.Domain, "providerdns") && d.d.DNSHost != DNSHostMailboxProvider {
				// The mailbox provider hosts this domain's DNS: the customer moves it to its nameservers.
				seen := at.Add(fakeSeenAfter)
				d.d.DNSHost, d.d.ConnectionStatus, d.seenAt = DNSHostMailboxProvider, ConnectionPending, &seen
				d.d.Nameservers = []string{"ns-a.provider-dns.example", "ns-b.provider-dns.example"}
				out = append(out, f.emitLocked(EventDomainUpdated, o.external, d.d))
			}
		}
		for i := range o.order.Items {
			if o.order.Items[i].Type == ItemMailboxes && o.order.Items[i].Status != "failed" {
				o.order.Items[i].Status = "active"
			}
		}
	case "ready":
		o.order.Status = OrderCompleted
		failed := 0
		for _, it := range o.order.Items {
			if it.Status == "failed" {
				failed++
			}
		}
		switch {
		case failed == len(o.order.Items):
			o.order.Status = OrderFailed
		case failed > 0:
			o.order.Status = OrderPartiallyFailed
		}
		o.order.CompletedAt = &at
		out = append(out, f.emitLocked(EventOrderCompleted, o.external, o.order))
	}
	if key == "register" && st.Status == StepFailed {
		o.order.Status, o.order.CompletedAt = OrderFailed, &at
		for i := step + 1; i < len(o.order.Steps); i++ {
			o.order.Steps[i].Status = StepSkipped
		}
		out = append(out, f.emitLocked(EventOrderCompleted, o.external, o.order))
	}
	return out
}

// verifyRegistrantLocked asks the first registration for a registrant email to verify it, done a minute later.
func (f *Fake) verifyRegistrantLocked(d *fakeDomain, at time.Time) []Event {
	acct := f.accounts[d.external]
	if acct == nil || acct.Registrant == nil {
		return nil
	}
	email := strings.ToLower(acct.Registrant.Email)
	if f.verified[email] {
		d.d.RegistrantVerification = &RegistrantVerification{Status: "verified", Email: email}
		return nil
	}
	deadline, done := at.Add(15*24*time.Hour), at.Add(time.Minute)
	d.d.RegistrantVerification = &RegistrantVerification{Status: "pending", Email: email, Deadline: &deadline}
	d.verifyAt = &done
	return []Event{f.emitLocked(EventDomainVerificationRequired, d.external, d.d)}
}

func (f *Fake) allItemsFailedLocked(o *fakeOrder) bool {
	for _, it := range o.order.Items {
		if it.Status != "failed" {
			return false
		}
	}
	return true
}

func (f *Fake) setDomainItemLocked(o *fakeOrder, domainID, status string) {
	for i := range o.order.Items {
		if o.order.Items[i].Type == ItemDomain && o.order.Items[i].DomainID == domainID && o.order.Items[i].Status != "failed" {
			o.order.Items[i].Status = status
		}
	}
}

// failItemsLocked fails and refunds a domain's mailboxes item, and its domain item when withDomain.
func (f *Fake) failItemsLocked(o *fakeOrder, name string, withDomain bool) {
	for i := range o.order.Items {
		it := &o.order.Items[i]
		if it.Domain != name || it.Status == "failed" || (it.Type == ItemDomain && !withDomain) {
			continue
		}
		it.Status, it.ErrorCode, it.RefundedMicros = "failed", CodeProvisioningFailed, it.AmountMicros
		f.balance += it.AmountMicros
		for _, id := range it.MailboxIDs {
			f.mailboxes[id].m.Status = MailboxFailed
			f.emitLocked(EventMailboxFailed, o.external, f.mailboxes[id].m)
		}
	}
}

func (f *Fake) failMailboxesLocked(o *fakeOrder, magic string) {
	for _, it := range o.order.Items {
		if it.Type == ItemMailboxes && strings.Contains(it.Domain, magic) {
			f.failItemsLocked(o, it.Domain, false)
		}
	}
}

func (f *Fake) GetOrder(_ context.Context, externalID, orderID string) (*Order, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.refusedLocked(); err != nil {
		return nil, err
	}
	f.advanceLocked()
	o := f.orders[orderID]
	if o == nil || o.external != externalID {
		return nil, fakeErr(http.StatusNotFound, CodeNotFound, "no such order")
	}
	return cloneJSON(o.order), nil
}

func (f *Fake) ListOrders(_ context.Context, externalID, _ string, _ int) (*Page[Order], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.refusedLocked(); err != nil {
		return nil, err
	}
	f.advanceLocked()
	out := &Page[Order]{Data: []Order{}}
	for _, o := range f.orders {
		if o.external == externalID {
			out.Data = append(out.Data, *cloneJSON(o.order))
		}
	}
	sort.Slice(out.Data, func(i, j int) bool { return out.Data[i].ID > out.Data[j].ID })
	return out, nil
}
