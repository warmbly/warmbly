package mailhost

import (
	"context"
	"errors"
	"net"
	"testing"
)

type recipientFake struct {
	*fakeResolver
	txt map[string][]string
}

func (f recipientFake) LookupTXT(_ context.Context, name string) ([]string, error) {
	if t, ok := f.txt[name]; ok {
		return t, nil
	}
	return nil, &net.DNSError{Err: "no such host", Name: name, IsNotFound: true}
}

func TestRecipient(t *testing.T) {
	r := recipientFake{
		fakeResolver: &fakeResolver{
			mx: map[string][]string{
				"acme.example":     {"aspmx.l.google.com", "alt1.aspmx.l.google.com"},
				"contoso.example":  {"contoso-example.mail.protection.outlook.com"},
				"guarded.example":  {"mx0a-001.pphosted.com"},
				"guarded2.example": {"eu-smtp-inbound-1.mimecast.com"},
				"selfhost.example": {"mail.selfhost.example"},
				"zoho.example":     {"mx.zoho.eu"},
			},
			mxErr: map[string]error{
				"flaky.example": &net.DNSError{Err: "server misbehaving", Name: "flaky.example", IsTemporary: true},
			},
		},
		txt: map[string][]string{
			"guarded.example":  {"google-site-verification=abc", "v=spf1 include:pphosted.com include:_spf.google.com ~all"},
			"guarded2.example": {"v=spf1 include:sendgrid.net include:spf.protection.outlook.com -all"},
			"selfhost.example": {"v=spf1 mx include:sendgrid.net -all"},
		},
	}
	cases := []struct {
		domain string
		want   Host
		ok     bool
	}{
		{"gmail.com", Gmail, true},
		{"hotmail.co.uk", Outlook, true},
		{"icloud.com", ICloud, true},
		{"contoso.onmicrosoft.com", Microsoft365, true},
		{"yahoo.com", Yahoo, true},
		{"acme.example", GoogleWorkspace, true},
		{"contoso.example", Microsoft365, true},
		{"guarded.example", GoogleWorkspace, true},
		{"guarded2.example", Microsoft365, true},
		{"selfhost.example", Other, true},
		{"zoho.example", Zoho, true},
		{"nomail.example", Unknown, true},
		{"flaky.example", Unknown, false},
		{"not a domain", Unknown, true},
	}
	for _, c := range cases {
		got, ok := Recipient(context.Background(), r, c.domain)
		if got != c.want || ok != c.ok {
			t.Errorf("Recipient(%q) = %q, %v; want %q, %v", c.domain, got, ok, c.want, c.ok)
		}
	}
}

func TestRecipientKnownDomainSkipsDNS(t *testing.T) {
	f := &fakeResolver{mxErr: map[string]error{"gmail.com": errors.New("must not be asked")}}
	if got, ok := Recipient(context.Background(), recipientFake{fakeResolver: f}, "Dana@GMail.com"); got != Gmail || !ok {
		t.Fatalf("got %q, %v", got, ok)
	}
	if f.mxCalls != 0 {
		t.Fatalf("known domain dialled DNS %d times", f.mxCalls)
	}
}

func TestKnownDomainTenant(t *testing.T) {
	if got := KnownDomain("ops@contoso.onmicrosoft.com"); got != Microsoft365 {
		t.Fatalf("tenant domain = %q", got)
	}
	if SharedProvider("contoso.onmicrosoft.com") {
		t.Fatal("a tenant domain is not a consumer provider")
	}
}
