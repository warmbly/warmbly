package mailboximport

import (
	"context"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/mailhost"
	"github.com/warmbly/warmbly/internal/repository"
)

func TestParsePaste(t *testing.T) {
	cases := []struct {
		name string
		text string
		want [][]string
	}{
		{"colon pairs", "a@x.com:pw1\nb@x.com:p:w2\n", [][]string{{"a@x.com", "pw1"}, {"b@x.com", "p:w2"}}},
		{"semicolon", "a@x.com;secret", [][]string{{"a@x.com", "secret"}}},
		{"spaces", "a@x.com abcd efgh ijkl mnop", [][]string{{"a@x.com", "abcd efgh ijkl mnop"}}},
		{"tabs", "email\tpassword\na@x.com\tpw", [][]string{{"email", "password"}, {"a@x.com", "pw"}}},
		{"csv", "email,password\na@x.com,pw", [][]string{{"email", "password"}, {"a@x.com", "pw"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := parsePaste(c.text)
			if len(got) != len(c.want) {
				t.Fatalf("rows = %v, want %v", got, c.want)
			}
			for i := range got {
				for j := range c.want[i] {
					if got[i][j] != c.want[i][j] {
						t.Fatalf("row %d = %v, want %v", i, got[i], c.want[i])
					}
				}
			}
		})
	}
}

func TestHasHeaderRow(t *testing.T) {
	if !hasHeaderRow([]string{"Email", "Password"}) {
		t.Fatal("a row of names is a header")
	}
	if hasHeaderRow([]string{"a@x.com", "pw"}) {
		t.Fatal("a row with an address is data")
	}
}

func fieldOf(s suggestion, header string) models.MailboxImportField {
	for _, c := range s.columns {
		if c.Header == header {
			return c.Field
		}
	}
	return ""
}

func TestSuggestAliasesAndVendor(t *testing.T) {
	svc := &Service{}
	headers := []string{"from_name", "from_email", "user_name", "password", "smtp_host", "smtp_port", "imap_host", "imap_port", "max_email_per_day"}
	body := [][]string{{"Alex", "alex@acme.io", "alex@acme.io", "pw", "smtp.acme.io", "587", "imap.acme.io", "993", "40"}}
	s := svc.suggest(context.Background(), headers, body, nil, nil)
	if s.vendor == nil || s.vendor.ID != "smartlead" {
		t.Fatalf("vendor = %+v, want smartlead", s.vendor)
	}
	want := map[string]models.MailboxImportField{
		"from_name": models.ImportFieldName, "from_email": models.ImportFieldEmail, "user_name": models.ImportFieldUsername,
		"password": models.ImportFieldPassword, "smtp_host": models.ImportFieldSMTPHost, "max_email_per_day": models.ImportFieldDailyLimit,
	}
	for h, f := range want {
		if got := fieldOf(s, h); got != f {
			t.Errorf("%s -> %s, want %s", h, got, f)
		}
	}
	for _, c := range s.columns {
		if c.Header == "password" && (!c.Secret || c.Samples[0] == "pw") {
			t.Fatalf("password column leaks its value: %+v", c)
		}
	}
}

func TestSuggestByShapeWithoutHeaders(t *testing.T) {
	svc := &Service{}
	headers := []string{"Column 1", "Column 2"}
	body := [][]string{{"a@acme.io", "abcd efgh ijkl mnop"}, {"b@acme.io", "qrst uvwx yzab cdef"}}
	s := svc.suggest(context.Background(), headers, body, nil, nil)
	if fieldOf(s, "Column 1") != models.ImportFieldEmail {
		t.Fatalf("first column = %s, want email", fieldOf(s, "Column 1"))
	}
	if fieldOf(s, "Column 2") != models.ImportFieldAppPassword {
		t.Fatalf("second column = %s, want app_password", fieldOf(s, "Column 2"))
	}
}

func TestSuggestGivenMappingWins(t *testing.T) {
	svc := &Service{}
	headers := []string{"email", "pw"}
	given := models.MailboxImportMapping{"0": models.ImportFieldEmail, "1": models.ImportFieldAppPassword}
	s := svc.suggest(context.Background(), headers, [][]string{{"a@x.com", "secret"}}, given, nil)
	if s.mapping["1"] != models.ImportFieldAppPassword {
		t.Fatalf("mapping = %v", s.mapping)
	}
}

func TestHeaderSignatureIgnoresSpelling(t *testing.T) {
	if headerSignature([]string{"SMTP Host", "Email"}) != headerSignature([]string{"smtp_host", "email"}) {
		t.Fatal("the same header set must share a signature")
	}
	if headerSignature([]string{"email", "smtp_host"}) == headerSignature([]string{"smtp_host", "email"}) {
		t.Fatal("column order is part of the signature")
	}
}

func build(t *testing.T, headers []string, rec []string, det map[string]mailhost.Detection, shared string, googleSignin bool) builtRow {
	t.Helper()
	svc := &Service{}
	s := svc.suggest(context.Background(), headers, [][]string{rec}, nil, nil)
	rows := buildRows(buildContext{
		headers: headers, mapping: s.mapping, detections: det, existing: map[string]models.EmailRef{},
		sharedPassword: shared, googleSignin: googleSignin,
	}, [][]string{rec}, 2)
	return rows[0]
}

func detection(domain string, h mailhost.Host) map[string]mailhost.Detection {
	return map[string]mailhost.Detection{domain: {
		Domain: domain, Host: h, Source: mailhost.SourceMX, Settings: mailhost.SettingsFor(h), PasswordAuth: mailhost.PasswordAuthFor(h),
	}}
}

func TestBuildRowGoogleAppPassword(t *testing.T) {
	r := build(t, []string{"email", "password"}, []string{"alex@acme.io", "abcd efgh ijkl mnop"},
		detection("acme.io", mailhost.GoogleWorkspace), "", false)
	if r.status != models.ImportPreviewReady {
		t.Fatalf("status = %s (%s)", r.status, r.problem)
	}
	if r.payload.SMTP.Host != "smtp.gmail.com" || r.payload.IMAP.Host != "imap.gmail.com" {
		t.Fatalf("servers = %+v %+v", r.payload.SMTP, r.payload.IMAP)
	}
	if r.payload.SMTP.Password != "abcdefghijklmnop" {
		t.Fatalf("app password kept its spaces: %q", r.payload.SMTP.Password)
	}
	if r.payload.AuthMethod != models.MailAuthAppPassword || r.payload.MailHost != string(mailhost.GoogleWorkspace) {
		t.Fatalf("auth = %s host = %s", r.payload.AuthMethod, r.payload.MailHost)
	}
}

func TestBuildRowGoogleAccountPassword(t *testing.T) {
	det := detection("acme.io", mailhost.GoogleWorkspace)
	r := build(t, []string{"email", "password"}, []string{"alex@acme.io", "Hunter2!pass"}, det, "", false)
	if r.status != models.ImportPreviewInvalid || r.cause != "google_app_password_required" {
		t.Fatalf("status = %s cause = %s", r.status, r.cause)
	}
	r = build(t, []string{"email", "password"}, []string{"alex@acme.io", "Hunter2!pass"}, det, "", true)
	if r.status != models.ImportPreviewNeedsSignin || r.cause != causeGoogleSignin {
		t.Fatalf("with Google sign-in on: status = %s cause = %s", r.status, r.cause)
	}
}

func TestBuildRowMicrosoftNeedsSignin(t *testing.T) {
	r := build(t, []string{"email", "password"}, []string{"sam@contoso.com", "pw"},
		detection("contoso.com", mailhost.Microsoft365), "", false)
	if r.status != models.ImportPreviewNeedsSignin || r.cause != causeMicrosoftSignin || !r.payload.Signin {
		t.Fatalf("status = %s cause = %s", r.status, r.cause)
	}
}

func TestBuildRowUnknownServers(t *testing.T) {
	det := map[string]mailhost.Detection{"odd.example": {Domain: "odd.example", Host: mailhost.Other, Source: mailhost.SourceNone, PasswordAuth: mailhost.Password}}
	r := build(t, []string{"email", "password"}, []string{"a@odd.example", "pw"}, det, "", false)
	if r.status != models.ImportPreviewInvalid || r.cause != causeUnknownServers {
		t.Fatalf("status = %s cause = %s", r.status, r.cause)
	}
}

func TestBuildRowFileServersAndSharedPassword(t *testing.T) {
	headers := []string{"email", "smtp_host", "smtp_port", "smtp_security", "imap_host"}
	rec := []string{"a@odd.example", "mail.odd.example", "465", "SSL", "mail.odd.example"}
	det := map[string]mailhost.Detection{"odd.example": {Domain: "odd.example", Host: mailhost.Other, PasswordAuth: mailhost.Password}}
	r := build(t, headers, rec, det, "shared-secret", false)
	if r.status != models.ImportPreviewReady {
		t.Fatalf("status = %s (%s)", r.status, r.problem)
	}
	if r.payload.SMTP.Port != 465 || r.payload.SMTP.Security != models.MailSecurityTLS {
		t.Fatalf("smtp = %+v", r.payload.SMTP)
	}
	if r.payload.IMAP.Port != 993 || r.payload.IMAP.Security != models.MailSecurityTLS {
		t.Fatalf("imap = %+v", r.payload.IMAP)
	}
	if r.payload.SMTP.Password != "shared-secret" || r.payload.IMAP.Username != "a@odd.example" {
		t.Fatalf("credentials = %+v", r.payload.SMTP)
	}
}

func TestBuildRowBadValues(t *testing.T) {
	det := detection("acme.io", mailhost.Zoho)
	r := build(t, []string{"email", "password", "daily_limit"}, []string{"a@acme.io", "pw", "lots"}, det, "", false)
	if r.status != models.ImportPreviewInvalid || r.cause != causeInvalidValue {
		t.Fatalf("status = %s cause = %s", r.status, r.cause)
	}
	r = build(t, []string{"email"}, []string{"not-an-address"}, det, "", false)
	if r.cause != causeInvalidEmail {
		t.Fatalf("cause = %s", r.cause)
	}
}

func TestBuildRowsDuplicatesAndExisting(t *testing.T) {
	svc := &Service{}
	headers := []string{"email", "password"}
	body := [][]string{{"a@acme.io", "pw"}, {"A@acme.io", "pw"}, {"b@acme.io", "pw"}}
	s := svc.suggest(context.Background(), headers, body, nil, nil)
	rows := buildRows(buildContext{
		headers: headers, mapping: s.mapping, detections: detection("acme.io", mailhost.Zoho),
		existing: map[string]models.EmailRef{"b@acme.io": {}},
	}, body, 2)
	if rows[1].status != models.ImportPreviewDuplicate {
		t.Fatalf("second row = %s, want duplicate", rows[1].status)
	}
	if rows[2].status != models.ImportPreviewExisting {
		t.Fatalf("third row = %s, want existing", rows[2].status)
	}
	if _, leaked := rows[0].fields["password"]; leaked {
		t.Fatal("a password reached the row's shown fields")
	}
}

func TestParseSecurity(t *testing.T) {
	cases := map[string]string{"SSL": "tls", "STARTTLS": "starttls", "ssl/tls": "tls"}
	for in, want := range cases {
		if got, ok := parseSecurity(in, 465); !ok || got != want {
			t.Errorf("%s -> %s, want %s", in, got, want)
		}
	}
	if got, _ := parseSecurity("true", 587); got != "starttls" {
		t.Errorf("true on 587 -> %s, want starttls", got)
	}
	if _, ok := parseSecurity("maybe", 587); ok {
		t.Error("an unknown word must not parse")
	}
}

func TestApplyFixSetsBothLegs(t *testing.T) {
	p := payload{MailHost: string(mailhost.GoogleWorkspace)}
	pw := "abcd efgh ijkl mnop"
	applyFix(&p, "a@acme.io", models.MailboxImportRowFix{AppPassword: &pw})
	if p.SMTP.Password != "abcdefghijklmnop" || p.IMAP.Password != "abcdefghijklmnop" {
		t.Fatalf("passwords = %q %q", p.SMTP.Password, p.IMAP.Password)
	}
	if p.SMTP.Port != 587 || p.IMAP.Port != 993 || p.SMTP.Username != "a@acme.io" {
		t.Fatalf("legs = %+v %+v", p.SMTP, p.IMAP)
	}
}

func TestApplyFixTurnsSigninRowIntoPasswordConnect(t *testing.T) {
	p := payload{MailHost: string(mailhost.GoogleWorkspace), Signin: true, AuthMethod: models.MailAuthOAuth}
	pw := "abcd efgh ijkl mnop"
	applyFix(&p, "[EMAIL]", models.MailboxImportRowFix{AppPassword: &pw})
	if p.Signin || p.AuthMethod != "app_password" {
		t.Fatalf("signin = %v auth = %q", p.Signin, p.AuthMethod)
	}
	if p.SMTP.Host != "smtp.gmail.com" || p.SMTP.Port != 587 || p.IMAP.Host != "imap.gmail.com" || p.IMAP.Port != 993 {
		t.Fatalf("legs = %+v %+v", p.SMTP, p.IMAP)
	}
	if p.SMTP.Password != "abcdefghijklmnop" || p.IMAP.Username != "[EMAIL]" {
		t.Fatalf("credentials = %+v %+v", p.SMTP, p.IMAP)
	}
}

func TestCheckGooglePassword(t *testing.T) {
	grant := uuid.New()
	cases := []struct {
		name string
		p    payload
		want bool
	}{
		{"app password", payload{MailHost: "gmail", SMTP: &models.Service{Password: "abcdefghijklmnop"}, IMAP: &models.Service{Password: "abcd efgh ijkl mnop"}}, true},
		{"account password on smtp", payload{MailHost: "google_workspace", SMTP: &models.Service{Password: "hunter2"}, IMAP: &models.Service{Password: "abcdefghijklmnop"}}, false},
		{"account password on imap", payload{MailHost: "google_workspace", SMTP: &models.Service{Password: "abcdefghijklmnop"}, IMAP: &models.Service{Password: "hunter2"}}, false},
		{"grant row", payload{MailHost: "google_workspace", GrantID: &grant, SMTP: &models.Service{Password: "hunter2"}}, true},
		{"another host", payload{MailHost: "zoho", SMTP: &models.Service{Password: "hunter2"}, IMAP: &models.Service{Password: "hunter2"}}, true},
	}
	for _, c := range cases {
		if got := checkGooglePassword(c.p) == nil; got != c.want {
			t.Errorf("%s: accepted = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestApplyFixNormalizesPerLegGooglePasswords(t *testing.T) {
	p := payload{MailHost: string(mailhost.Gmail)}
	smtpPw, imapPw := "abcd efgh ijkl mnop", "ponm lkji hgfe dcba"
	applyFix(&p, "[EMAIL]", models.MailboxImportRowFix{
		SMTP: &models.MailboxImportLegFix{Password: &smtpPw},
		IMAP: &models.MailboxImportLegFix{Password: &imapPw},
	})
	if p.SMTP.Password != "abcdefghijklmnop" || p.IMAP.Password != "ponmlkjihgfedcba" {
		t.Fatalf("passwords = %q %q", p.SMTP.Password, p.IMAP.Password)
	}
}

func TestFixableRows(t *testing.T) {
	cases := []struct {
		status, cause string
		want          bool
	}{
		{models.ImportRowFailed, "google_bad_credentials", true},
		{models.ImportRowNeedsSignin, causeGoogleSignin, true},
		{models.ImportRowNeedsSignin, causeMicrosoftSignin, true},
		{models.ImportRowNeedsSignin, causeVendorAuthorizing, false},
		{models.ImportRowConnected, "", false},
		{models.ImportRowQueued, "", false},
	}
	for _, c := range cases {
		if got := fixable(&models.MailboxImportRow{Status: c.status, Cause: c.cause}); got != c.want {
			t.Errorf("%s/%s fixable = %v, want %v", c.status, c.cause, got, c.want)
		}
	}
}

func TestCSVSafe(t *testing.T) {
	for _, v := range []string{"=HYPERLINK(1)", "+1", "-2", "@x"} {
		if got := csvSafe(v); got[0] != '\'' {
			t.Errorf("%q not neutralized: %q", v, got)
		}
	}
	if csvSafe("alex@acme.io") != "alex@acme.io" {
		t.Error("an ordinary value changed")
	}
}

func TestCursorRoundTrip(t *testing.T) {
	svcCursor := encodeCursor(mustTime(t), [16]byte{1})
	if _, _, ok := decodeCursor(svcCursor); !ok {
		t.Fatal("cursor did not round-trip")
	}
	if _, _, ok := decodeCursor("nope"); ok {
		t.Fatal("garbage decoded")
	}
}

func TestNameFromEmail(t *testing.T) {
	if got := nameFromEmail("alex.rivera@acme.io"); got != "Alex Rivera" {
		t.Fatalf("got %q", got)
	}
}

func TestReadInputRejectsTooMany(t *testing.T) {
	text := "email\n"
	for i := 0; i < 5002; i++ {
		text += "a" + strconv.Itoa(i) + "@x.com\n"
	}
	if _, xerr := readInput(Input{Text: text}); xerr == nil {
		t.Fatal("more than the row cap must be refused")
	}
}

func mustTime(t *testing.T) time.Time {
	t.Helper()
	return time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC)
}

func TestBuildRowUsesAnAdminGrant(t *testing.T) {
	grant := uuid.New()
	svc := &Service{}
	headers := []string{"email", "password"}
	for _, tc := range []struct {
		email string
		host  mailhost.Host
		pw    string
		key   string
	}{
		{"sam@contoso.com", mailhost.Microsoft365, "anything", grantKey(models.GrantProviderMicrosoft, "contoso.com")},
		{"alex@acme.io", mailhost.GoogleWorkspace, "", grantKey(models.GrantProviderGoogle, "acme.io")},
	} {
		rec := []string{tc.email, tc.pw}
		s := svc.suggest(context.Background(), headers, [][]string{rec}, nil, nil)
		rows := buildRows(buildContext{
			headers: headers, mapping: s.mapping, detections: detection(domainOf(tc.email), tc.host),
			existing: map[string]models.EmailRef{}, grants: map[string]uuid.UUID{tc.key: grant},
		}, [][]string{rec}, 2)
		r := rows[0]
		if r.status != models.ImportPreviewReady || r.payload.GrantID == nil || *r.payload.GrantID != grant ||
			r.payload.AuthMethod != models.MailAuthDelegated || r.payload.SMTP != nil {
			t.Fatalf("%s: status = %s payload = %+v", tc.email, r.status, r.payload)
		}
	}
}

type fakeVendors struct {
	fields map[models.MailboxImportField]string
	linked map[uuid.UUID]string
}

func (f *fakeVendors) Fields(context.Context, uuid.UUID, uuid.UUID, string, string, string) (map[models.MailboxImportField]string, *errx.Error) {
	out := map[models.MailboxImportField]string{}
	for k, v := range f.fields {
		out[k] = v
	}
	return out, nil
}
func (f *fakeVendors) Link(_ context.Context, a, _ uuid.UUID, id string) { f.linked[a] = id }

type fakeResolver struct{ mx map[string][]*net.MX }

func (r fakeResolver) LookupMX(_ context.Context, name string) ([]*net.MX, error) {
	if m, ok := r.mx[name]; ok {
		return m, nil
	}
	return nil, &net.DNSError{Err: "no such host", Name: name, IsNotFound: true}
}
func (r fakeResolver) LookupSRV(context.Context, string, string, string) (string, []*net.SRV, error) {
	return "", nil, &net.DNSError{Err: "no such host", IsNotFound: true}
}

func TestResolveVendorRowBuildsLikeAFileRow(t *testing.T) {
	vendors := &fakeVendors{fields: map[models.MailboxImportField]string{models.ImportFieldAppPassword: "abcd efgh ijkl mnop"}, linked: map[uuid.UUID]string{}}
	det := mailhost.NewDetector(fakeResolver{mx: map[string][]*net.MX{"acme.io": {{Host: "aspmx.l.google.com.", Pref: 1}}}}, http.DefaultClient, nil)
	svc := &Service{vendors: vendors, detector: det, google: func() bool { return false }}
	conn := uuid.New()
	w := repository.ImportWorkRow{OrgID: uuid.New(), Email: "alex@acme.io", Line: 1}
	p, cause, problem := svc.resolveVendorRow(context.Background(), w, payload{Name: "Alex", NameGiven: true, VendorConnectionID: &conn, VendorMailboxID: "m1"})
	if cause != "" {
		t.Fatalf("cause = %s (%s)", cause, problem)
	}
	if p.SMTP == nil || p.SMTP.Host != "smtp.gmail.com" || p.SMTP.Password != "abcdefghijklmnop" || p.MailHost != string(mailhost.GoogleWorkspace) {
		t.Fatalf("payload = %+v smtp = %+v", p, p.SMTP)
	}
	if p.VendorMailboxID != "m1" || p.Name != "Alex" {
		t.Fatalf("vendor refs or name lost: %+v", p)
	}
}

func TestPasteReadsASemicolonTable(t *testing.T) {
	rows := parsePaste("email;password;name\na@x.test;p;w;Ann\n")
	if len(rows) != 2 || len(rows[0]) != 3 || rows[1][0] != "a@x.test" {
		t.Fatalf("table = %q", rows)
	}
	pairs := parsePaste("a@x.test;pa;ss\nb@x.test:secret\n")
	if len(pairs) != 2 || pairs[0][1] != "pa;ss" || pairs[1][1] != "secret" {
		t.Fatalf("pairs = %q", pairs)
	}
}

func TestPasteKeepsPairsWhosePasswordHasAComma(t *testing.T) {
	rows := parsePaste("a@x.test:pa,ss\nb@x.test:secret\n")
	if len(rows) != 2 || rows[0][0] != "a@x.test" || rows[0][1] != "pa,ss" || rows[1][1] != "secret" {
		t.Fatalf("rows = %q", rows)
	}
	csv := parsePaste("email,password\na@x.test,secret\n")
	if len(csv) != 2 || len(csv[1]) != 2 || csv[1][1] != "secret" {
		t.Fatalf("csv = %q", csv)
	}
}
