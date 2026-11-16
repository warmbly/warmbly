package keystatus

import (
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/warmbly/warmbly/internal/app/mailcred"
	"github.com/warmbly/warmbly/internal/app/orgtransfer"
	"github.com/warmbly/warmbly/internal/app/twofa"
	"github.com/warmbly/warmbly/internal/pkg/encrypt"
)

func TestSealedColumnsCoverEveryInstanceKeySecret(t *testing.T) {
	got := map[string]Domain{}
	for _, c := range Columns() {
		got[c.Table+"."+c.Column] = c.Domain
	}
	for _, want := range []string{
		"email_accounts_smtp_imap.smtp_password",
		"email_accounts_smtp_imap.imap_password",
		"email_accounts_oauth.access_token",
		"email_accounts_oauth.refresh_token",
		"webhook_endpoints.secret",
		"oauth_applications.webhook_secret",
		"agency_vendor_connections.credentials",
		"cloud_link.token",
		"mailbox_erasures.refresh_token",
	} {
		if got[want] != DomainInstance {
			t.Fatalf("%s must be scanned under the instance credential key", want)
		}
	}
	if got["user_totp_settings.totp_secret_sealed"] != DomainMFA {
		t.Fatal("TOTP enrollments must be scanned under the MFA key")
	}

	for _, c := range instanceOnlyColumns {
		if _, ok := orgtransfer.ExcludedTables[c.Table]; c.Domain == DomainInstance && !ok {
			t.Fatalf("%s is listed as instance-only but is not in orgtransfer.ExcludedTables", c.Table)
		}
	}
}

func TestResealValueClassifiesEveryShape(t *testing.T) {
	oldKey, newKey := strings.Repeat("11", 32), strings.Repeat("22", 32)
	t.Setenv("CREDENTIALS_ENCRYPTION_KEY", oldKey)
	t.Setenv("CREDENTIALS_ENCRYPTION_READ_KEYS", "")
	old, err := encrypt.FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := old.Encrypt("smtp-password")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("CREDENTIALS_ENCRYPTION_KEY", newKey)
	t.Setenv("CREDENTIALS_ENCRYPTION_READ_KEYS", `["`+oldKey+`"]`)
	current, err := encrypt.FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	keys := &Keyring{instance: current, instanceRead: 1}
	col := SealedColumn{Table: "email_accounts_smtp_imap", Column: "smtp_password", Domain: DomainInstance}

	resealed, changed, err := resealValue(keys, col, legacy)
	if err != nil || !changed {
		t.Fatalf("a read-key value must be resealed, got changed=%v err=%v", changed, err)
	}
	if _, changed, err := resealValue(keys, col, resealed); err != nil || changed {
		t.Fatal("a resealed value must open under the primary")
	}
	if _, _, err := resealValue(keys, col, mailcred.OrgPrefix+"c2VhbGVk"); !errors.Is(err, errWorkspaceSealed) {
		t.Fatal("a workspace-sealed value must be counted apart and never touched by an instance rotation")
	}
	if _, _, err := resealValue(keys, col, "ya29.a0Af-legacy-token"); !errors.Is(err, errUnsealed) {
		t.Fatal("a value outside the sealed format must be reported unsealed, never rewritten")
	}
	foreign := hex.EncodeToString(make([]byte, 64))
	if _, _, err := resealValue(keys, col, foreign); err == nil || errors.Is(err, errUnsealed) {
		t.Fatal("a sealed value no key opens must be reported unreadable")
	}

	mfaOld, mfaNew := twofa.DeriveKey("old-mfa-secret"), twofa.DeriveKey("new-mfa-secret")
	sealed, err := twofa.Seal(mfaOld, "JBSWY3DPEHPK3PXP")
	if err != nil {
		t.Fatal(err)
	}
	keys.mfa, keys.mfaRead = &mfaNew, [][32]byte{mfaOld}
	mfa := SealedColumn{Table: "user_totp_settings", Column: "totp_secret_sealed", Domain: DomainMFA}
	if _, changed, err := resealValue(keys, mfa, sealed); err != nil || !changed {
		t.Fatal("an enrollment sealed under the MFA read key must be resealed")
	}
}

func TestReportOnReadKeyCountsValuesStillNeedingAReadKey(t *testing.T) {
	r := Report{Columns: []Count{{ReadKey: 2, Primary: 5}, {ReadKey: 1, Unreadable: 3}}}
	if r.OnReadKey() != 3 || r.Unreadable() != 3 {
		t.Fatalf("OnReadKey=%d Unreadable=%d", r.OnReadKey(), r.Unreadable())
	}
}
