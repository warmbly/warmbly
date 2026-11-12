package mailcred

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"golang.org/x/oauth2"

	"github.com/warmbly/warmbly/internal/app/cipher"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/legacysecret"
	"github.com/warmbly/warmbly/internal/pkg/encrypt"
)

func testSealer(t *testing.T, mode Mode) *Sealer {
	t.Helper()
	inst, err := encrypt.NewEncrypter(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewSealer(inst, cipher.NewStatic(bytes.Repeat([]byte{9}, 32)), mode)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestParseMode(t *testing.T) {
	for in, want := range map[string]Mode{"": ModeInstance, "instance": ModeInstance, " Organization ": ModeOrganization} {
		if got, err := ParseMode(in); err != nil || got != want {
			t.Fatalf("ParseMode(%q) = %q, %v", in, got, err)
		}
	}
	if _, err := ParseMode("kms"); err == nil {
		t.Fatal("an unknown mode must be refused")
	}
	if _, err := NewSealer(nil, nil, ModeOrganization); err == nil {
		t.Fatal("organization mode without a key service must be refused")
	}
}

func TestSealFollowsMode(t *testing.T) {
	ctx := context.Background()
	org := uuid.New()

	inst := testSealer(t, ModeInstance)
	v, err := inst.Seal(ctx, &org, "hunter2")
	if err != nil || IsOrgSealed(v) {
		t.Fatalf("instance mode sealed %q, %v", v, err)
	}

	orgs := testSealer(t, ModeOrganization)
	v, err = orgs.Seal(ctx, &org, "hunter2")
	if err != nil || !IsOrgSealed(v) || strings.Contains(v, "hunter2") {
		t.Fatalf("organization mode sealed %q, %v", v, err)
	}
	if v, err = orgs.Seal(ctx, nil, "hunter2"); err != nil || IsOrgSealed(v) {
		t.Fatalf("a mailbox with no organization must stay on the instance key, got %q, %v", v, err)
	}
}

func TestOpenReadsBothFormats(t *testing.T) {
	ctx := context.Background()
	org := uuid.New()
	inst, orgs := testSealer(t, ModeInstance), testSealer(t, ModeOrganization)

	old, _ := inst.Seal(ctx, &org, "old-secret")
	fresh, _ := orgs.Seal(ctx, &org, "new-secret")
	for _, s := range []*Sealer{inst, orgs} {
		if got, err := s.Open(ctx, &org, old); err != nil || got != "old-secret" {
			t.Fatalf("instance value opened as %q, %v", got, err)
		}
		if got, err := s.Open(ctx, &org, fresh); err != nil || got != "new-secret" {
			t.Fatalf("organization value opened as %q, %v", got, err)
		}
	}
	if got, err := orgs.Open(ctx, &org, OrgPrefix); err != nil || got != "" {
		t.Fatalf("an empty organization value opened as %q, %v", got, err)
	}
	if _, err := orgs.Open(ctx, nil, fresh); err == nil {
		t.Fatal("an organization value must not open without its organization")
	}
}

func TestOpenOrLegacy(t *testing.T) {
	ctx := context.Background()
	org := uuid.New()
	s := testSealer(t, ModeOrganization)

	before := legacysecret.Count(legacysecret.MailboxCredential)
	if got, legacy, err := s.OpenOrLegacy(ctx, &org, "ya29.plain"); err != nil || !legacy || got != "ya29.plain" {
		t.Fatalf("legacy plaintext read as %q legacy=%v, %v", got, legacy, err)
	}
	if legacysecret.Count(legacysecret.MailboxCredential) != before+1 {
		t.Fatal("a plaintext read was not counted")
	}
	sealed, _ := s.Seal(ctx, &org, "tok")
	if got, legacy, err := s.OpenOrLegacy(ctx, &org, sealed); err != nil || legacy || got != "tok" {
		t.Fatalf("sealed token read as %q legacy=%v, %v", got, legacy, err)
	}
	if legacysecret.Count(legacysecret.MailboxCredential) != before+1 {
		t.Fatal("a sealed read was counted as plaintext")
	}
	if _, legacy, err := s.OpenOrLegacy(ctx, &org, strings.Repeat("ab", 64)); !errors.Is(err, encrypt.ErrUnreadable) || legacy {
		t.Fatal("a sealed-format value no key opens must be unreadable, never legacy plaintext")
	}
}

func TestSealAllUsesOneKey(t *testing.T) {
	ctx := context.Background()
	org := uuid.New()
	s := testSealer(t, ModeOrganization)
	out, err := s.SealAll(ctx, &org, "a", "b", "")
	if err != nil || len(out) != 3 {
		t.Fatal(out, err)
	}
	for i, want := range []string{"a", "b", ""} {
		if got, err := s.Open(ctx, &org, out[i]); err != nil || got != want {
			t.Fatalf("value %d opened as %q, %v", i, got, err)
		}
	}
}

func TestAddEmailCarriesNoPlaintext(t *testing.T) {
	ctx := context.Background()
	c, err := cipher.NewStatic(bytes.Repeat([]byte{5}, 32)).Cipher(ctx, uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]*models.AddWorkerEmail{
		"google": {Google: &models.AddWorkerEmailGoogleData{Token: &oauth2.Token{AccessToken: "ya29.secret", RefreshToken: "1//refresh"}}},
		"graph":  {Graph: &models.AddWorkerEmailGraphData{Token: &oauth2.Token{AccessToken: "eyJ.secret", RefreshToken: "M.refresh"}}},
		"smtp": {SmtpImap: &models.AddWorkerEmailSmtpImapData{Credentials: &models.SmtpImap{
			SMTP: &models.Service{Username: "u@x.test", Password: "smtp-secret", Host: "smtp.x.test", Port: 465},
			IMAP: &models.Service{Username: "u@x.test", Password: "imap-secret", Host: "imap.x.test", Port: 993},
		}}},
		"smtp-oauth": {SmtpImap: &models.AddWorkerEmailSmtpImapData{Token: &oauth2.Token{AccessToken: "xoauth-secret"}}},
	}
	for name, e := range cases {
		want, _ := json.Marshal(e)
		if err := SealAddEmail(ctx, c, e); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		wire, _ := json.Marshal(e)
		// Whole values: a three-letter fragment like "eyJ" turns up in random base64 ciphertext now and then.
		for _, secret := range []string{"ya29.secret", "1//refresh", "eyJ.secret", "M.refresh", "smtp-secret", "imap-secret", "xoauth-secret"} {
			if strings.Contains(string(wire), secret) {
				t.Fatalf("%s: the sealed payload still carries %q", name, secret)
			}
		}
		var back models.AddWorkerEmail
		if err := json.Unmarshal(wire, &back); err != nil {
			t.Fatal(err)
		}
		if err := OpenAddEmail(ctx, c, &back); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		back.SealedSecrets = ""
		if got, _ := json.Marshal(&back); string(got) != string(want) {
			t.Fatalf("%s: round trip changed the payload\n got %s\nwant %s", name, got, want)
		}
	}
}

func TestOpenAddEmailRefusesAnotherKey(t *testing.T) {
	ctx := context.Background()
	a, _ := cipher.NewStatic(bytes.Repeat([]byte{1}, 32)).Cipher(ctx, uuid.New())
	b, _ := cipher.NewStatic(bytes.Repeat([]byte{2}, 32)).Cipher(ctx, uuid.New())
	e := &models.AddWorkerEmail{Google: &models.AddWorkerEmailGoogleData{Token: &oauth2.Token{AccessToken: "x"}}}
	if err := SealAddEmail(ctx, a, e); err != nil {
		t.Fatal(err)
	}
	if err := OpenAddEmail(ctx, b, e); err == nil {
		t.Fatal("a payload sealed for one organization must not open under another's key")
	}
}

// The background reseal runs under either sealing as long as a key exists to seal under.
func TestResealsUnderEitherSealing(t *testing.T) {
	if !testSealer(t, ModeInstance).Reseals() || !testSealer(t, ModeOrganization).Reseals() {
		t.Fatal("a configured sealer must reseal")
	}
	var none *Sealer
	if none.Reseals() || (&Sealer{Mode: ModeInstance}).Reseals() {
		t.Fatal("no key means nothing to reseal under")
	}
}
