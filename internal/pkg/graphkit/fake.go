package graphkit

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

// Fake is an in-process GraphKit for tests, `make dev`, the sandbox and qa
// proof. It follows the contract's sandbox: the same magic domain names
// decide edge cases, orders walk their six steps over about twenty seconds,
// existing domains connect ten seconds after they are added, and every state
// change lands in the events feed and, while Run is going, in the OnEvent
// hook, which the store wires to the handler its webhook uses.
type Fake struct {
	mu  sync.Mutex
	now func() time.Time
	seq int
	// run makes ids unique across restarts, as GraphKit's are; the store keeps them.
	run  string
	hook func(Event)
	q    chan Event

	accounts   map[string]*Account
	quotes     map[string]*fakeQuote
	orders     map[string]*fakeOrder
	domains    map[string]*fakeDomain
	mailboxes  map[string]*fakeMailbox
	dns        map[string][]DNSRecord
	events     []Event
	idem       map[string]idemEntry
	delegation *Delegation
	accountDel map[string]*Delegation
	settings   PartnerSettings
	webhook    *WebhookEndpoint
	secret     string
	balance    int64
	renewing   map[string]bool
	// verified holds the registrant emails already verified, so only the first registration asks.
	verified  map[string]bool
	suspended bool

	social *fakeSocial
	// linkPage builds a link session URL when a stand-in consent page is served.
	linkPage func(sessionID string) string
}

type fakeQuote struct {
	external string
	quote    Quote
	items    []QuoteItem
}

type fakeOrder struct {
	external string
	order    Order
	domains  []string
	boxes    []string
	last     int
}

type fakeDomain struct {
	external string
	d        Domain
	transfer *time.Time
	// seenAt is when a pending existing domain's delegation or records are seen; movesAt when they move away.
	seenAt  *time.Time
	movesAt *time.Time
	// verifyAt is when a pending registrant verification completes.
	verifyAt *time.Time
	zone     int
}

type fakeMailbox struct {
	external string
	m        Mailbox
	password string
	// readyAt is when a delegation connection becomes ready once its identity is registered.
	readyAt *time.Time
}

type idemEntry struct {
	fingerprint string
	raw         []byte
	err         *Error
}

// fakeStepAt is when each order step completes, from the order's creation.
var fakeStepAt = []time.Duration{3 * time.Second, 6 * time.Second, 9 * time.Second, 13 * time.Second, 16 * time.Second, 19 * time.Second}

var fakeTLDPrices = map[string]int64{
	"com": 11_170_000, "net": 13_490_000, "org": 10_990_000, "co": 24_500_000, "io": 39_000_000, "ai": 79_000_000,
}

var fakeDefaultTLDs = []string{"com", "co", "io", "net", "org", "ai"}

// fakeMinYears are the TLDs registered for more than a year at least.
var fakeMinYears = map[string]int{"ai": 2}

// fakeBrokerScopes are the scopes the broker can mint.
var fakeBrokerScopes = []string{
	"https://mail.google.com/", "https://www.googleapis.com/auth/gmail.modify", "https://www.googleapis.com/auth/gmail.readonly",
	"https://www.googleapis.com/auth/gmail.send", "https://www.googleapis.com/auth/gmail.compose",
	"https://www.googleapis.com/auth/gmail.labels", "https://www.googleapis.com/auth/gmail.settings.basic",
}

const fakeMailboxMonthly = 2_000_000

// fakeSeenAfter is how long after it is added an existing domain's delegation or records are seen.
const fakeSeenAfter = 10 * time.Second

// fakeConnectionTTL is how long a pending existing domain waits before it expires.
const fakeConnectionTTL = 48 * time.Hour

// fakeNameserverPairs are the Cloudflare-looking pairs the sandbox assigns, one per zone.
var fakeNameserverPairs = [][2]string{{"ada", "bob"}, {"kim", "lou"}, {"nia", "otto"}, {"pam", "rex"}}

// fakeRedirectIPs are the documentation addresses forwarded domains point at.
var fakeRedirectIPs = []string{"192.0.2.80", "2001:db8::80"}

// NewFake builds an empty simulator with a $250 partner balance.
func NewFake() *Fake {
	run := make([]byte, 4)
	_, _ = rand.Read(run)
	return &Fake{
		run:        hex.EncodeToString(run),
		now:        time.Now,
		q:          make(chan Event, 1024),
		accounts:   map[string]*Account{},
		quotes:     map[string]*fakeQuote{},
		orders:     map[string]*fakeOrder{},
		domains:    map[string]*fakeDomain{},
		mailboxes:  map[string]*fakeMailbox{},
		dns:        map[string][]DNSRecord{},
		idem:       map[string]idemEntry{},
		renewing:   map[string]bool{},
		accountDel: map[string]*Delegation{},
		verified:   map[string]bool{},
		settings:   PartnerSettings{RegistrantNotices: "registrar", DMARCPolicyDefault: "none"},
		balance:    250_000_000,
		social:     newFakeSocial(),
	}
}

// Suspend makes every call answer 403 partner_suspended, as for a partner GraphKit suspended.
func (f *Fake) Suspend(on bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.suspended = on
}

func (f *Fake) refusedLocked() error {
	if f.suspended {
		return fakeErr(http.StatusForbidden, CodePartnerSuspended, "This partner is suspended.")
	}
	return nil
}

// SetClock replaces the clock, for tests that step time.
func (f *Fake) SetClock(now func() time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = now
}

// OnEvent sets the hook state changes are delivered to while Run is going.
func (f *Fake) OnEvent(fn func(Event)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hook = fn
}

// Run advances the simulation every second and delivers events to the hook until ctx ends.
func (f *Fake) Run(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			f.Tick()
		case ev := <-f.q:
			f.mu.Lock()
			hook := f.hook
			f.mu.Unlock()
			if hook != nil {
				hook(ev)
			}
		}
	}
}

// Tick advances every order, domain and mailbox to now and returns the new events.
func (f *Fake) Tick() []Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.advanceLocked()
}

// SetBalance sets the partner balance, for tests of partner_balance_insufficient.
func (f *Fake) SetBalance(micros int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.balance = micros
}

// SetRenewPrice changes a domain's renewal price, as GraphKit's price list moves.
func (f *Fake) SetRenewPrice(domainID string, micros int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if d := f.domains[domainID]; d != nil && d.d.RenewMicros != micros {
		d.d.RenewMicros = micros
		f.emitLocked(EventDomainUpdated, d.external, d.d)
	}
}

// SetNextMonthlyPrice announces a mailbox's price for its next period, as a GraphKit price change does.
func (f *Fake) SetNextMonthlyPrice(mailboxID string, micros int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if m := f.mailboxes[mailboxID]; m != nil {
		m.m.NextPeriodMonthlyMicros = &micros
		f.emitLocked(EventMailboxUpdated, m.external, m.m)
	}
}

// ExpireDomain moves a domain's expiry and its renewal window, for renewal tests.
func (f *Fake) ExpireDomain(domainID string, at time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if d := f.domains[domainID]; d != nil {
		f.setExpiryLocked(d, at.UTC())
	}
}

func (f *Fake) setExpiryLocked(d *fakeDomain, exp time.Time) {
	from, until := exp.AddDate(0, 0, -365), exp.AddDate(0, 0, 30)
	if strings.Contains(d.d.Domain, "notdue") {
		from = exp.AddDate(0, 0, -30)
	}
	d.d.ExpiresAt, d.d.RenewableFrom, d.d.RenewableUntil = &exp, &from, &until
}

func (f *Fake) id(prefix string) string {
	f.seq++
	return fmt.Sprintf("%s_%s%06d", prefix, f.run, f.seq)
}

func fakeErr(status int, code, msg string) *Error {
	return &Error{Status: status, Code: code, Message: msg, RequestID: "req_fake"}
}

func fakeErrDetails(status int, code, msg string, details any) *Error {
	e := fakeErr(status, code, msg)
	e.Details, _ = json.Marshal(details)
	return e
}

func (f *Fake) emitLocked(typ, external string, data any) Event {
	raw, _ := json.Marshal(data)
	ext := external
	ev := Event{ID: f.id("evt"), Type: typ, CreatedAt: f.now().UTC(), AccountExternalID: &ext, Data: raw}
	f.events = append(f.events, ev)
	select {
	case f.q <- ev:
	default:
	}
	return ev
}

// replay runs fn once per key: the same key and body replay the first answer,
// a different body is idempotency_conflict. Retryable failures are not stored.
func replay[T any](f *Fake, key, op string, body any, fn func() (*T, error)) (*T, error) {
	if err := f.refusedLocked(); err != nil {
		return nil, err
	}
	if key == "" {
		return nil, fakeErr(http.StatusBadRequest, CodeInvalidRequest, "Idempotency-Key is required")
	}
	b, _ := json.Marshal(body)
	sum := sha256.Sum256(append([]byte(op+"\x00"), b...))
	fp := hex.EncodeToString(sum[:])
	if e, ok := f.idem[key]; ok {
		if e.fingerprint != fp {
			return nil, fakeErr(http.StatusConflict, CodeIdempotencyConflict, "key reused with a different body")
		}
		if e.err != nil {
			return nil, e.err
		}
		var out T
		if err := json.Unmarshal(e.raw, &out); err != nil {
			return nil, err
		}
		return &out, nil
	}
	v, err := fn()
	entry := idemEntry{fingerprint: fp}
	if err != nil {
		gerr, ok := AsError(err)
		if !ok || gerr.Retryable() {
			return nil, err
		}
		entry.err = gerr
	} else {
		entry.raw, _ = json.Marshal(v)
	}
	f.idem[key] = entry
	return v, err
}

func cloneJSON[T any](v T) *T {
	b, _ := json.Marshal(v)
	var out T
	_ = json.Unmarshal(b, &out)
	return &out
}

func price(v int64) *int64 { return &v }

// ---- accounts -----------------------------------------------------------

var (
	fakePhone   = regexp.MustCompile(`^\+[0-9]{1,3}\.[0-9]{4,14}$`)
	fakeCountry = regexp.MustCompile(`^[A-Z]{2}$`)
	fakeLocal   = regexp.MustCompile(`^[a-z0-9]([a-z0-9._-]{0,62}[a-z0-9])?$`)
)

func contactProblems(c *Contact) []string {
	if c == nil {
		return []string{"registrant"}
	}
	var bad []string
	req := map[string]string{"first_name": c.FirstName, "last_name": c.LastName, "email": c.Email, "address1": c.Address1, "city": c.City, "postal_code": c.PostalCode}
	for k, v := range req {
		if strings.TrimSpace(v) == "" {
			bad = append(bad, k)
		}
	}
	if !fakePhone.MatchString(c.Phone) {
		bad = append(bad, "phone")
	}
	if !fakeCountry.MatchString(c.Country) {
		bad = append(bad, "country")
	}
	if !strings.Contains(c.Email, "@") && !slices.Contains(bad, "email") {
		bad = append(bad, "email")
	}
	sort.Strings(bad)
	return bad
}

func (f *Fake) UpsertAccount(_ context.Context, externalID string, in AccountInput, key string) (*Account, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return replay(f, key, "account:"+externalID, in, func() (*Account, error) {
		if in.Registrant != nil {
			if bad := contactProblems(in.Registrant); len(bad) > 0 {
				return nil, fakeErrDetails(http.StatusBadRequest, CodeContactInvalid, "registrant is incomplete", map[string]any{"fields": bad})
			}
		}
		now := f.now().UTC()
		a := f.accounts[externalID]
		if a == nil {
			a = &Account{ID: f.id("acct"), ExternalID: externalID, CreatedAt: now}
			f.accounts[externalID] = a
		}
		a.Name, a.UpdatedAt = in.Name, now
		if in.Registrant != nil {
			a.Registrant = cloneJSON(*in.Registrant)
		}
		return cloneJSON(*a), nil
	})
}

func (f *Fake) GetAccount(_ context.Context, externalID string) (*Account, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.refusedLocked(); err != nil {
		return nil, err
	}
	a := f.accounts[externalID]
	if a == nil {
		return nil, fakeErr(http.StatusNotFound, CodeAccountNotFound, "no such account")
	}
	return cloneJSON(*a), nil
}

func (f *Fake) ListAccounts(context.Context, string, int) (*Page[Account], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.refusedLocked(); err != nil {
		return nil, err
	}
	out := &Page[Account]{Data: []Account{}}
	for _, a := range f.accounts {
		out.Data = append(out.Data, *cloneJSON(*a))
	}
	sort.Slice(out.Data, func(i, j int) bool { return out.Data[i].ID > out.Data[j].ID })
	return out, nil
}

// ---- pricing and search -------------------------------------------------

func fakeMinYearsFor(tld string) int {
	if n := fakeMinYears[tld]; n > 0 {
		return n
	}
	return 1
}

func fakeMaxYearsFor(tld string) int {
	if tld == "co" {
		return 5
	}
	return 10
}

func (f *Fake) Pricing(context.Context) (*Pricing, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.refusedLocked(); err != nil {
		return nil, err
	}
	p := &Pricing{Currency: "USD", UpdatedAt: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
		Mailboxes: []MailboxPrice{{Provider: ProviderGoogle, MonthlyMicros: fakeMailboxMonthly}, {Provider: ProviderMicrosoft, MonthlyMicros: fakeMailboxMonthly}},
		Limits:    Limits{MailboxesPerDomainMax: 5, MailboxesPerDomainRecommended: 3},
		Sending: &SendingPrices{AddonMonthlyMicros: 30_000_000, BoughtMonthlyMicros: 2_000_000, OutsideMonthlyMicros: 3_000_000,
			Modes: []string{"sending_warmup", "warmup_only"}, SendsPerMailboxPerDay: 50},
		Social: &SocialPrices{Channels: fakeSocialCatalog()}}
	for _, tld := range fakeDefaultTLDs {
		v := fakeTLDPrices[tld]
		p.Domains = append(p.Domains, TLDPrice{TLD: tld, RegisterMicros: v, RenewMicros: v, TransferMicros: v, PrivacyIncluded: true,
			MinYears: fakeMinYearsFor(tld), MaxYears: fakeMaxYearsFor(tld)})
	}
	return p, nil
}

func splitDomain(name string) (label, tld string) {
	name = strings.ToLower(strings.TrimSpace(name))
	i := strings.LastIndex(name, ".")
	if i <= 0 {
		return name, ""
	}
	return name[:i], name[i+1:]
}

// offerLocked is one domain's availability under the sandbox rules.
func (f *Fake) offerLocked(name string, exact bool) DomainOffer {
	name = strings.ToLower(strings.TrimSpace(name))
	label, tld := splitDomain(name)
	o := DomainOffer{Domain: name, DisplayName: name, TLD: tld, Exact: exact}
	reason := func(r string) { o.Reason = &r }
	p, ok := fakeTLDPrices[tld]
	if !ok || label == "" {
		reason("tld_unsupported")
		return o
	}
	o.RegisterMicros, o.RenewMicros = price(p), price(p)
	switch {
	case len(label) <= 4 || strings.Contains(label, "taken") || f.liveDomainLocked(name) != nil:
		reason("registered")
	case strings.Contains(label, "premium"):
		o.Premium = true
		reason("premium")
	case strings.Contains(label, "reserved"):
		reason("reserved")
	default:
		o.Available = true
	}
	return o
}

// liveDomainLocked is the domain of that name on any account, registered or connected.
func (f *Fake) liveDomainLocked(name string) *fakeDomain {
	for _, d := range f.domains {
		if d.d.Domain != name {
			continue
		}
		switch d.d.Status {
		case DomainFailed, DomainTransferredOut, DomainRemoved:
			continue
		}
		if !d.d.Registered() && d.d.ConnectionStatus == ConnectionExpired {
			continue
		}
		return d
	}
	return nil
}

var fakeKeep = regexp.MustCompile(`[^a-z0-9-]+`)

func (f *Fake) Search(_ context.Context, query string, tlds []string, limit int, exactOnly bool) (*SearchResult, error) {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil, fakeErr(http.StatusBadRequest, CodeInvalidRequest, "q is required")
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		return nil, fakeErr(http.StatusBadRequest, CodeInvalidRequest, "limit is at most 100")
	}
	if len(tlds) == 0 {
		tlds = fakeDefaultTLDs
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.refusedLocked(); err != nil {
		return nil, err
	}
	out := &SearchResult{Query: query, Data: []DomainOffer{}}
	seen := map[string]bool{}
	add := func(name string, exact bool) bool {
		if seen[name] {
			return false
		}
		seen[name] = true
		o := f.offerLocked(name, exact)
		if exact || o.Available {
			out.Data = append(out.Data, o)
			return true
		}
		return false
	}
	var keyword string
	if strings.Contains(q, ".") && !strings.Contains(q, " ") {
		add(q, true)
		keyword, _ = splitDomain(q)
	} else {
		keyword = fakeKeep.ReplaceAllString(strings.ReplaceAll(q, " ", ""), "")
		for _, tld := range tlds {
			add(keyword+"."+tld, true)
		}
	}
	keyword = fakeKeep.ReplaceAllString(keyword, "")
	if keyword == "" || exactOnly {
		return out, nil
	}
	suggestions := 0
	try := func(name string) {
		if suggestions < limit && add(name, false) {
			suggestions++
		}
	}
	for _, tld := range tlds {
		try(keyword + "." + tld)
	}
	for _, p := range []string{"get", "try", "hey", "use"} {
		try(p + keyword + "." + tlds[0])
	}
	for _, s := range []string{"hq", "mail", "team"} {
		try(keyword + s + "." + tlds[0])
	}
	for _, tld := range tlds[1:] {
		for _, p := range []string{"get", "try"} {
			try(p + keyword + "." + tld)
		}
	}
	return out, nil
}

func (f *Fake) Check(_ context.Context, domains []string, key string) ([]DomainOffer, error) {
	if len(domains) > 50 {
		return nil, fakeErr(http.StatusBadRequest, CodeInvalidRequest, "at most 50 domains")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.refusedLocked(); err != nil {
		return nil, err
	}
	out := make([]DomainOffer, 0, len(domains))
	for _, d := range domains {
		out = append(out, f.offerLocked(d, true))
	}
	return out, nil
}
