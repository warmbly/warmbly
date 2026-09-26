package mailhost

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type fakeResolver struct {
	mx      map[string][]string
	srv     map[string][]*net.SRV
	mxErr   map[string]error
	mu      sync.Mutex
	mxCalls int
}

func (f *fakeResolver) LookupMX(_ context.Context, name string) ([]*net.MX, error) {
	f.mu.Lock()
	f.mxCalls++
	f.mu.Unlock()
	if err := f.mxErr[name]; err != nil {
		return nil, err
	}
	hosts, ok := f.mx[name]
	if !ok {
		return nil, &net.DNSError{Err: "no such host", Name: name, IsNotFound: true}
	}
	out := make([]*net.MX, 0, len(hosts))
	for i, h := range hosts {
		out = append(out, &net.MX{Host: h + ".", Pref: uint16(10 * (i + 1))})
	}
	return out, nil
}

func (f *fakeResolver) LookupSRV(_ context.Context, service, proto, name string) (string, []*net.SRV, error) {
	key := "_" + service + "._" + proto + "." + name
	recs, ok := f.srv[key]
	if !ok {
		return "", nil, &net.DNSError{Err: "no such host", Name: key, IsNotFound: true}
	}
	return key, recs, nil
}

type mapCache struct {
	mu sync.Mutex
	m  map[string]Detection
}

func (c *mapCache) Get(_ context.Context, d string) (Detection, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.m[d]
	return v, ok
}

func (c *mapCache) Set(_ context.Context, d string, v Detection) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[d] = v
}

// testDetector serves autoconfig at /ac/<domain> and the ISPDB at /ispdb/<domain>.
func testDetector(t *testing.T, r *fakeResolver, docs map[string]string) (*Detector, *[]string) {
	t.Helper()
	var mu sync.Mutex
	var hits []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		mu.Lock()
		hits = append(hits, req.URL.Path)
		mu.Unlock()
		body, ok := docs[req.URL.Path]
		if !ok {
			http.NotFound(w, req)
			return
		}
		w.Header().Set("Content-Type", "text/xml")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	d := NewDetector(r, srv.Client(), nil)
	d.ispdbBase = srv.URL + "/ispdb/"
	d.autoconfigURLs = func(domain string) []string { return []string{srv.URL + "/ac/" + domain} }
	return d, &hits
}

func TestDetectMX(t *testing.T) {
	r := &fakeResolver{mx: map[string][]string{
		"acme.io":       {"aspmx.l.google.com", "alt1.aspmx.l.google.com"},
		"newgoogle.io":  {"smtp.google.com"},
		"legacygoog.io": {"aspmx2.googlemail.com"},
		"ms.io":         {"ms-io.mail.protection.outlook.com"},
		"msdnssec.io":   {"msdnssec-io.r-v1.mx.microsoft"},
		"olc.io":        {"outlook-com.olc.protection.outlook.com"},
		"zoho-us.io":    {"mx.zoho.com", "mx2.zoho.com"},
		"zoho-eu.io":    {"mx.zoho.eu"},
		"zoho-in.io":    {"mx.zoho.in"},
		"zoho-au.io":    {"mx.zoho.com.au"},
		"zoho-jp.io":    {"mx.zoho.jp"},
		"zoho-ca.io":    {"mx.zohocloud.ca"},
		"zoho-sa.io":    {"mx.zoho.sa"},
		"godaddy.io":    {"mailstore1.secureserver.net"},
		"nc.io":         {"mx1.privateemail.com"},
		"ionos-us.io":   {"mx00.ionos.com"},
		"ionos-de.io":   {"mx00.ionos.de"},
		"ionos-uk.io":   {"mx00.ionos.co.uk"},
		"oneandone.io":  {"mx00.1and1.co.uk"},
		"perfora.io":    {"mx01.perfora.net"},
		"kunden.io":     {"mx00.kundenserver.de"},
		"hst.io":        {"mx1.hostinger.com"},
		"fm.io":         {"in1-smtp.messagingengine.com"},
		"yh.io":         {"mta5.am0.yahoodns.net"},
		"ic.io":         {"mx01.mail.icloud.com"},
		"mg.io":         {"aspmx1.migadu.com"},
		"pm.io":         {"mailserver.purelymail.com"},
		"rs.io":         {"mx1.emailsrvr.com"},
		"ovh.io":        {"mx1.mail.ovh.net"},
		"yx.io":         {"mx.yandex.net"},
		"gmxmx.io":      {"mx00.gmx.net"},
		"pr.io":         {"mail.protonmail.ch"},
	}}
	d, _ := testDetector(t, r, nil)

	cases := []struct {
		domain string
		host   Host
		smtp   string
		imap   string
		pa     PasswordAuth
	}{
		{"acme.io", GoogleWorkspace, "smtp.gmail.com", "imap.gmail.com", AppPassword},
		{"newgoogle.io", GoogleWorkspace, "smtp.gmail.com", "imap.gmail.com", AppPassword},
		{"legacygoog.io", GoogleWorkspace, "smtp.gmail.com", "imap.gmail.com", AppPassword},
		{"ms.io", Microsoft365, "smtp.office365.com", "outlook.office365.com", OAuthOnly},
		{"msdnssec.io", Microsoft365, "smtp.office365.com", "outlook.office365.com", OAuthOnly},
		{"olc.io", Outlook, "smtp-mail.outlook.com", "outlook.office365.com", OAuthOnly},
		{"zoho-us.io", Zoho, "smtppro.zoho.com", "imappro.zoho.com", Password},
		{"zoho-eu.io", Zoho, "smtppro.zoho.eu", "imappro.zoho.eu", Password},
		{"zoho-in.io", Zoho, "smtppro.zoho.in", "imappro.zoho.in", Password},
		{"zoho-au.io", Zoho, "smtppro.zoho.com.au", "imappro.zoho.com.au", Password},
		{"zoho-jp.io", Zoho, "smtppro.zoho.jp", "imappro.zoho.jp", Password},
		{"zoho-ca.io", Zoho, "smtppro.zohocloud.ca", "imappro.zohocloud.ca", Password},
		{"zoho-sa.io", Zoho, "smtppro.zoho.sa", "imappro.zoho.sa", Password},
		{"godaddy.io", GoDaddy, "smtpout.secureserver.net", "imap.secureserver.net", Password},
		{"nc.io", Namecheap, "mail.privateemail.com", "mail.privateemail.com", Password},
		{"ionos-us.io", IONOS, "smtp.ionos.com", "imap.ionos.com", Password},
		{"ionos-de.io", IONOS, "smtp.ionos.de", "imap.ionos.de", Password},
		{"ionos-uk.io", IONOS, "smtp.ionos.co.uk", "imap.ionos.co.uk", Password},
		{"oneandone.io", IONOS, "smtp.ionos.co.uk", "imap.ionos.co.uk", Password},
		{"perfora.io", IONOS, "smtp.ionos.com", "imap.ionos.com", Password},
		{"kunden.io", IONOS, "smtp.ionos.de", "imap.ionos.de", Password},
		{"hst.io", Hostinger, "smtp.hostinger.com", "imap.hostinger.com", Password},
		{"fm.io", Fastmail, "smtp.fastmail.com", "imap.fastmail.com", AppPassword},
		{"yh.io", Yahoo, "smtp.mail.yahoo.com", "imap.mail.yahoo.com", AppPassword},
		{"ic.io", ICloud, "smtp.mail.me.com", "imap.mail.me.com", AppPassword},
		{"mg.io", Migadu, "smtp.migadu.com", "imap.migadu.com", Password},
		{"pm.io", Purelymail, "smtp.purelymail.com", "imap.purelymail.com", Password},
		{"rs.io", Rackspace, "secure.emailsrvr.com", "secure.emailsrvr.com", Password},
		{"ovh.io", OVH, "ssl0.ovh.net", "ssl0.ovh.net", Password},
		{"yx.io", Yandex, "smtp.yandex.com", "imap.yandex.com", AppPassword},
		{"gmxmx.io", GMX, "mail.gmx.net", "imap.gmx.net", Password},
		{"pr.io", Proton, "", "", Unsupported},
	}
	for _, tc := range cases {
		t.Run(tc.domain, func(t *testing.T) {
			got := d.Detect(context.Background(), tc.domain)
			if got.Host != tc.host || got.Source != SourceMX || got.PasswordAuth != tc.pa {
				t.Fatalf("got host=%q source=%q pa=%q, want %q mx %q", got.Host, got.Source, got.PasswordAuth, tc.host, tc.pa)
			}
			if len(got.MX) == 0 || strings.HasSuffix(got.MX[0], ".") {
				t.Fatalf("MX not normalized: %v", got.MX)
			}
			if tc.smtp == "" {
				if got.Settings != nil {
					t.Fatalf("want no settings, got %+v", got.Settings)
				}
				return
			}
			if got.Settings == nil || got.Settings.SMTP.Host != tc.smtp || got.Settings.IMAP.Host != tc.imap {
				t.Fatalf("settings %+v, want %s / %s", got.Settings, tc.smtp, tc.imap)
			}
			if got.Settings.IMAP.Port != 993 || got.Settings.IMAP.Security != SecurityTLS {
				t.Fatalf("imap endpoint %+v", got.Settings.IMAP)
			}
		})
	}
}

func TestDetectKnown(t *testing.T) {
	r := &fakeResolver{}
	d, hits := testDetector(t, r, nil)
	cases := map[string]Host{
		"gmail.com": Gmail, "GoogleMail.com.": Gmail, "outlook.com": Outlook, "hotmail.co.uk": Outlook,
		"live.fr": Outlook, "msn.com": Outlook, "yahoo.com": Yahoo, "yahoo.fr": Yahoo, "ymail.com": Yahoo,
		"rocketmail.com": Yahoo, "aol.com": AOL, "icloud.com": ICloud, "me.com": ICloud, "mac.com": ICloud,
		"zoho.com": Zoho, "zohomail.com": Zoho, "gmx.de": GMX, "gmx.com": GMX, "yandex.ru": Yandex,
		"ya.ru": Yandex, "proton.me": Proton, "protonmail.com": Proton, "fastmail.com": Fastmail, "fastmail.fm": Fastmail,
		"someone@Gmail.com": Gmail,
	}
	for domain, want := range cases {
		got := d.Detect(context.Background(), domain)
		if got.Host != want || got.Source != SourceKnown {
			t.Errorf("%s: got %q/%q, want %q/known", domain, got.Host, got.Source, want)
		}
	}
	if r.mxCalls != 0 || len(*hits) != 0 {
		t.Fatalf("known domains must not touch the network: mx=%d http=%v", r.mxCalls, *hits)
	}

	z := d.Detect(context.Background(), "zoho.com")
	if z.Settings.SMTP.Host != "smtp.zoho.com" || z.Settings.IMAP.Host != "imap.zoho.com" {
		t.Fatalf("personal zoho: %+v", z.Settings)
	}
	g := d.Detect(context.Background(), "gmx.de")
	if g.Settings.SMTP.Host != "mail.gmx.net" {
		t.Fatalf("gmx.de: %+v", g.Settings)
	}
	g = d.Detect(context.Background(), "gmx.com")
	if g.Settings.SMTP.Host != "mail.gmx.com" || g.Settings.SMTP.Security != SecurityStartTLS {
		t.Fatalf("gmx.com: %+v", g.Settings)
	}
	if gm := d.Detect(context.Background(), "gmail.com"); gm.AppPasswordURL != "https://myaccount.google.com/apppasswords" {
		t.Fatalf("gmail app password url %q", gm.AppPasswordURL)
	}
	if yj := d.Detect(context.Background(), "yahoo.co.jp"); yj.Host == Yahoo {
		t.Fatalf("yahoo.co.jp is not Yahoo Mail")
	}
}

const acXML = `<?xml version="1.0" encoding="UTF-8"?>
<clientConfig version="1.1">
  <emailProvider id="%EMAILDOMAIN%">
    <domain>%EMAILDOMAIN%</domain>
    <incomingServer type="pop3">
      <hostname>pop.%EMAILDOMAIN%</hostname><port>995</port><socketType>SSL</socketType>
    </incomingServer>
    <incomingServer type="imap">
      <hostname>imap.%EMAILDOMAIN%</hostname><port>143</port><socketType>plain</socketType>
    </incomingServer>
    <incomingServer type="imap">
      <hostname>imap.%EMAILDOMAIN%</hostname><port>143</port><socketType>STARTTLS</socketType>
    </incomingServer>
    <incomingServer type="imap">
      <hostname>imap.%EMAILDOMAIN%</hostname><port>993</port><socketType>SSL</socketType>
      <authentication>password-cleartext</authentication>
    </incomingServer>
    <outgoingServer type="smtp">
      <hostname>smtp.%EMAILDOMAIN%</hostname><port>465</port><socketType>SSL</socketType>
    </outgoingServer>
    <outgoingServer type="smtp">
      <hostname>smtp.%EMAILDOMAIN%</hostname><port>587</port><socketType>STARTTLS</socketType>
      <authentication>password-cleartext</authentication>
    </outgoingServer>
    <outgoingServer type="smtp">
      <hostname>smtp.%EMAILDOMAIN%</hostname><port>25</port><socketType>plain</socketType>
    </outgoingServer>
  </emailProvider>
</clientConfig>`

func TestParseClientConfig(t *testing.T) {
	s := parseClientConfig([]byte(acXML), "shop.example")
	if s == nil {
		t.Fatal("no settings")
	}
	want := Settings{
		SMTP: Endpoint{Host: "smtp.shop.example", Port: 587, Security: SecurityStartTLS},
		IMAP: Endpoint{Host: "imap.shop.example", Port: 993, Security: SecurityTLS},
	}
	if *s != want {
		t.Fatalf("got %+v, want %+v", *s, want)
	}

	onlyPlain := strings.ReplaceAll(acXML, "SSL", "plain")
	onlyPlain = strings.ReplaceAll(onlyPlain, "STARTTLS", "plain")
	if parseClientConfig([]byte(onlyPlain), "shop.example") != nil {
		t.Fatal("plain-only config must not produce settings")
	}

	oauthOnly := `<clientConfig><emailProvider>
	  <incomingServer type="imap"><hostname>imap.x.example</hostname><port>993</port><socketType>SSL</socketType><authentication>OAuth2</authentication></incomingServer>
	  <outgoingServer type="smtp"><hostname>smtp.x.example</hostname><port>465</port><socketType>SSL</socketType></outgoingServer>
	</emailProvider></clientConfig>`
	if parseClientConfig([]byte(oauthOnly), "x.example") != nil {
		t.Fatal("an OAuth-only IMAP server cannot take a password")
	}

	tls465 := `<clientConfig><emailProvider>
	  <incomingServer type="imap"><hostname>mail.host.example</hostname><port>993</port><socketType>SSL</socketType></incomingServer>
	  <outgoingServer type="smtp"><hostname>mail.host.example</hostname><port>465</port><socketType>SSL</socketType></outgoingServer>
	</emailProvider></clientConfig>`
	s = parseClientConfig([]byte(tls465), "x.example")
	if s == nil || s.SMTP.Port != 465 || s.SMTP.Security != SecurityTLS {
		t.Fatalf("465 tls: %+v", s)
	}

	if parseClientConfig([]byte("<html>not xml"), "x.example") != nil {
		t.Fatal("garbage must not parse")
	}
	bad := strings.ReplaceAll(tls465, "mail.host.example", "bad host")
	if parseClientConfig([]byte(bad), "x.example") != nil {
		t.Fatal("an invalid hostname must be refused")
	}
}

func TestDetectAutoconfig(t *testing.T) {
	r := &fakeResolver{mx: map[string][]string{"shop.example": {"mx.shop.example"}}}
	d, _ := testDetector(t, r, map[string]string{"/ac/shop.example": acXML})
	got := d.Detect(context.Background(), "shop.example")
	if got.Source != SourceAutoconfig || got.Host != Other || got.PasswordAuth != Password {
		t.Fatalf("got %+v", got)
	}
	if got.Settings.SMTP.Host != "smtp.shop.example" {
		t.Fatalf("settings %+v", got.Settings)
	}
}

func TestDetectAutoconfigKnownServer(t *testing.T) {
	doc := `<clientConfig><emailProvider>
	  <incomingServer type="imap"><hostname>imap.gmail.com</hostname><port>993</port><socketType>SSL</socketType></incomingServer>
	  <outgoingServer type="smtp"><hostname>smtp.gmail.com</hostname><port>465</port><socketType>SSL</socketType></outgoingServer>
	</emailProvider></clientConfig>`
	r := &fakeResolver{mx: map[string][]string{"relay.example": {"in.filter.example"}}}
	d, _ := testDetector(t, r, map[string]string{"/ac/relay.example": doc})
	got := d.Detect(context.Background(), "relay.example")
	if got.Host != GoogleWorkspace || got.PasswordAuth != AppPassword || got.Settings.SMTP.Port != 465 {
		t.Fatalf("got %+v", got)
	}
}

func TestDetectISPDBViaMX(t *testing.T) {
	doc := `<clientConfig><emailProvider>
	  <incomingServer type="imap"><hostname>imap.hosting.example</hostname><port>993</port><socketType>SSL</socketType></incomingServer>
	  <outgoingServer type="smtp"><hostname>smtp.hosting.example</hostname><port>587</port><socketType>STARTTLS</socketType></outgoingServer>
	</emailProvider></clientConfig>`
	r := &fakeResolver{mx: map[string][]string{"client.example": {"mx3.eu.hosting.example"}}}
	d, hits := testDetector(t, r, map[string]string{"/ispdb/hosting.example": doc})
	got := d.Detect(context.Background(), "client.example")
	if got.Source != SourceISPDB || got.Settings == nil || got.Settings.IMAP.Host != "imap.hosting.example" {
		t.Fatalf("got %+v", got)
	}
	want := []string{"/ac/client.example", "/ispdb/client.example", "/ispdb/hosting.example"}
	if fmt.Sprint(*hits) != fmt.Sprint(want) {
		t.Fatalf("lookup order %v, want %v", *hits, want)
	}
}

func TestDetectSRV(t *testing.T) {
	r := &fakeResolver{
		mx: map[string][]string{"srv.example": {"mx.srv.example"}},
		srv: map[string][]*net.SRV{
			"_submission._tcp.srv.example": {{Target: "mail.srv.example.", Port: 587, Priority: 0}},
			"_imaps._tcp.srv.example":      {{Target: "mail.srv.example.", Port: 993, Priority: 0}},
		},
	}
	d, _ := testDetector(t, r, nil)
	got := d.Detect(context.Background(), "srv.example")
	want := Settings{
		SMTP: Endpoint{Host: "mail.srv.example", Port: 587, Security: SecurityStartTLS},
		IMAP: Endpoint{Host: "mail.srv.example", Port: 993, Security: SecurityTLS},
	}
	if got.Source != SourceSRV || got.Settings == nil || *got.Settings != want {
		t.Fatalf("got %+v", got)
	}

	r.srv = map[string][]*net.SRV{
		"_submission._tcp.srv.example": {{Target: ".", Port: 0}},
		"_imaps._tcp.srv.example":      {{Target: "mail.srv.example.", Port: 993}},
	}
	if got := d.Detect(context.Background(), "srv.example"); got.Source != SourceNone {
		t.Fatalf("a '.' target means no service, got %+v", got)
	}
}

func TestDetectNone(t *testing.T) {
	r := &fakeResolver{mx: map[string][]string{"quiet.example": {"mx.quiet.example"}}}
	c := &mapCache{m: map[string]Detection{}}
	d, _ := testDetector(t, r, nil)
	d.cache = c
	got := d.Detect(context.Background(), "quiet.example")
	if got.Source != SourceNone || got.Host != Other || got.Settings != nil || got.PasswordAuth != Password {
		t.Fatalf("got %+v", got)
	}
	got = d.Detect(context.Background(), "nomx.example")
	if got.Source != SourceNone || got.Host != Unknown || got.PasswordAuth != Unsupported {
		t.Fatalf("got %+v", got)
	}
	if _, ok := c.m["quiet.example"]; !ok {
		t.Fatal("a definite answer should be cached")
	}

	r.mxErr = map[string]error{"flaky.example": &net.DNSError{Err: "server misbehaving", IsTemporary: true}}
	d.Detect(context.Background(), "flaky.example")
	if _, ok := c.m["flaky.example"]; ok {
		t.Fatal("a transient failure must not be cached")
	}

	if got := d.Detect(context.Background(), "not a domain"); got.Source != SourceNone {
		t.Fatalf("invalid input: %+v", got)
	}
}

func TestDetectCache(t *testing.T) {
	r := &fakeResolver{mx: map[string][]string{"acme.io": {"aspmx.l.google.com"}}}
	c := &mapCache{m: map[string]Detection{}}
	d, _ := testDetector(t, r, nil)
	d.cache = c
	d.Detect(context.Background(), "acme.io")
	d.Detect(context.Background(), "ACME.io")
	if r.mxCalls != 1 {
		t.Fatalf("cache miss: %d MX lookups", r.mxCalls)
	}
}

func TestDetectMany(t *testing.T) {
	r := &fakeResolver{mx: map[string][]string{
		"acme.io": {"aspmx.l.google.com"},
		"ms.io":   {"ms-io.mail.protection.outlook.com"},
	}}
	d, _ := testDetector(t, r, nil)
	got := d.DetectMany(context.Background(), []string{"acme.io", "ACME.io", "ms.io", "", "gmail.com", "acme.io"})
	if len(got) != 4 {
		t.Fatalf("got %d entries: %v", len(got), got)
	}
	if got["ACME.io"].Host != GoogleWorkspace || got["ms.io"].Host != Microsoft365 || got["gmail.com"].Host != Gmail {
		t.Fatalf("got %+v", got)
	}
	if r.mxCalls != 2 {
		t.Fatalf("duplicates must be looked up once, got %d", r.mxCalls)
	}
}

func TestNormalizeDomain(t *testing.T) {
	cases := map[string]string{
		" Example.COM. ":    "example.com",
		"bob@Example.com":   "example.com",
		"bücher.example":    "xn--bcher-kva.example",
		"localhost":         "",
		"exa mple.com":      "",
		"":                  "",
		"a/b.com":           "",
		"-bad.example":      "",
		"x@sub.example.org": "sub.example.org",
	}
	for in, want := range cases {
		if got := NormalizeDomain(in); got != want {
			t.Errorf("NormalizeDomain(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFromServer(t *testing.T) {
	cases := map[string]Host{
		"smtp.gmail.com":             Gmail,
		"IMAP.GMAIL.COM.":            Gmail,
		"smtp-relay.gmail.com":       Gmail,
		"outlook.office365.com":      Microsoft365,
		"smtp.office365.com":         Microsoft365,
		"smtp-mail.outlook.com":      Outlook,
		"smtppro.zoho.eu":            Zoho,
		"imap.zoho.com":              Zoho,
		"smtppro.zohocloud.ca":       Zoho,
		"smtp.mail.yahoo.com":        Yahoo,
		"smtp.aol.com":               AOL,
		"smtp.mail.me.com":           ICloud,
		"imap.fastmail.com":          Fastmail,
		"smtpout.secureserver.net":   GoDaddy,
		"mail.privateemail.com":      Namecheap,
		"smtp.ionos.co.uk":           IONOS,
		"smtp.1and1.com":             IONOS,
		"smtp.hostinger.com":         Hostinger,
		"ssl0.ovh.net":               OVH,
		"smtp.migadu.com":            Migadu,
		"smtp.purelymail.com":        Purelymail,
		"secure.emailsrvr.com":       Rackspace,
		"smtp.yandex.com":            Yandex,
		"mail.gmx.net":               GMX,
		"mail.protonmail.ch":         Proton,
		"mail.example.org":           Unknown,
		"":                           Unknown,
		"notgoogle.com":              Unknown,
		"gmail.com.attacker.example": Unknown,
	}
	for in, want := range cases {
		if got := FromServer(in); got != want {
			t.Errorf("FromServer(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRefine(t *testing.T) {
	cases := []struct {
		h      Host
		domain string
		want   Host
	}{
		{Gmail, "acme.io", GoogleWorkspace},
		{Gmail, "gmail.com", Gmail},
		{GoogleWorkspace, "googlemail.com", Gmail},
		{Microsoft365, "hotmail.com", Outlook},
		{Microsoft365, "outlook.de", Outlook},
		{Microsoft365, "acme.io", Microsoft365},
		{Outlook, "acme.io", Outlook},
		{Zoho, "acme.io", Zoho},
		{Gmail, "", Gmail},
	}
	for _, tc := range cases {
		if got := Refine(tc.h, tc.domain); got != tc.want {
			t.Errorf("Refine(%q, %q) = %q, want %q", tc.h, tc.domain, got, tc.want)
		}
	}
}

func TestAppPassword(t *testing.T) {
	yes := []string{"abcdefghijklmnop", "abcd efgh ijkl mnop", " ABCD EFGH IJKL MNOP "}
	no := []string{"", "abcd efgh ijkl mno", "abcd1fgh ijkl mnop", "hunter2", "abcdefghijklmnopq", "ábcdefghijklmnop"}
	for _, s := range yes {
		if !LooksLikeGoogleAppPassword(s) {
			t.Errorf("%q should look like an app password", s)
		}
	}
	for _, s := range no {
		if LooksLikeGoogleAppPassword(s) {
			t.Errorf("%q should not look like an app password", s)
		}
	}
	if got := NormalizeAppPassword(GoogleWorkspace, "abcd efgh ijkl mnop"); got != "abcdefghijklmnop" {
		t.Errorf("google normalize: %q", got)
	}
	if got := NormalizeAppPassword(Zoho, "abcd efgh ijkl mnop"); got != "abcd efgh ijkl mnop" {
		t.Errorf("non-google passwords stay untouched: %q", got)
	}
	if got := NormalizeAppPassword(Gmail, "my pass word"); got != "my pass word" {
		t.Errorf("a password that is not app-password shaped stays untouched: %q", got)
	}
	if AuthMethodFor(Gmail, "") != "app_password" || AuthMethodFor(GoDaddy, "") != "password" ||
		AuthMethodFor(Zoho, AppPassword) != "app_password" {
		t.Error("AuthMethodFor")
	}
}

func TestValidAndLabels(t *testing.T) {
	if !Valid("") || !Valid("google_workspace") || Valid("hetzner") || Valid("Gmail") {
		t.Fatal("Valid")
	}
	for _, h := range Hosts() {
		if !Valid(string(h)) || h.Label() == "" {
			t.Errorf("%q has no label", h)
		}
		if len(h) > 32 {
			t.Errorf("%q exceeds the mail_host length", h)
		}
	}
	if GoogleWorkspace.Label() != "Google Workspace" || Unknown.Label() != "" {
		t.Fatal("Label")
	}
}

func TestForMailbox(t *testing.T) {
	cases := []struct {
		stored, provider, address string
		want                      Host
	}{
		{"", "gmail", "a@gmail.com", Gmail},
		{"", "gmail", "a@acme.io", GoogleWorkspace},
		{"", "outlook", "a@hotmail.com", Outlook},
		{"", "outlook", "a@acme.io", Microsoft365},
		{"", "smtp_imap", "a@yahoo.com", Yahoo},
		{"", "smtp_imap", "a@acme.io", Other},
		{"zoho", "smtp_imap", "a@acme.io", Zoho},
		{"google_workspace", "smtp_imap", "a@gmail.com", Gmail},
		{"bogus", "smtp_imap", "a@acme.io", Other},
	}
	for _, c := range cases {
		if got := ForMailbox(c.stored, c.provider, c.address); got != c.want {
			t.Errorf("ForMailbox(%q, %q, %q) = %q, want %q", c.stored, c.provider, c.address, got, c.want)
		}
	}
}
