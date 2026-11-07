package repository

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/mailvendor"
)

// The automatic switch reconnects only vendor-linked Google Workspace mailboxes on an app password, a grant
// or a sign-in without full mail access, at most once in flight and once a week after it ends.
func TestLiveSigninSwitchSelection(t *testing.T) {
	handle, pool := liveContactDB(t)
	requireSchemaVersion(t, pool, 357)
	f := newImportFixture(t, pool)
	repo := NewVendorConnectionRepository(handle)
	ctx := context.Background()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("%q: %v", sql[:min(60, len(sql))], err)
		}
	}
	conn := func(vendor, status string) uuid.UUID {
		id := uuid.New()
		exec(`INSERT INTO mailbox_vendor_connections (id, organization_id, vendor, credentials, status) VALUES ($1, $2, $3, 'sealed', $4)`, id, f.org, vendor, status)
		return id
	}
	zap, forge, broken := conn("zapmail", "active"), conn("mailforge", "active"), conn("inboxkit", "invalid")
	tag := f.org.String()[:8]
	box := func(name, provider, auth, host string, link uuid.UUID, scopes []string) uuid.UUID {
		email := name + "-" + tag + "@acme.io"
		if name == "personal" {
			email = "personal-" + tag + "@gmail.com"
		}
		id := f.mailbox(t, pool, f.org, f.owner, email)
		exec(`UPDATE email_accounts SET provider = $2, auth_method = $3, mail_host = $4, vendor_connection_id = $5, vendor_mailbox_id = 'v-'||$6
			WHERE id = $1`, id, provider, auth, host, link, name)
		if auth == "oauth" {
			exec(`INSERT INTO email_accounts_oauth (email_account_id, access_token, refresh_token, expires_at, granted_scopes) VALUES ($1, 'a', 'r', now(), $2)`, id, scopes)
		}
		return id
	}
	full, modify := []string{models.GoogleFullMailScope}, []string{"https://www.googleapis.com/auth/gmail.modify"}

	delegated := box("delegated", "gmail", "delegated", "google_workspace", zap, nil)
	unknown := box("unknown", "gmail", "oauth", "google_workspace", zap, nil)
	older := box("older", "gmail", "oauth", "google_workspace", zap, modify)
	password := box("password", "smtp_imap", "app_password", "google_workspace", zap, nil)
	eligible := []uuid.UUID{delegated, unknown, older, password}

	ineligible := map[string]uuid.UUID{
		"full mail access":         box("full", "gmail", "oauth", "google_workspace", zap, full),
		"another mail host":        box("elsewhere", "smtp_imap", "app_password", "other", zap, nil),
		"a plain password":         box("plain", "smtp_imap", "password", "google_workspace", zap, nil),
		"a vendor that cannot":     box("forge", "gmail", "oauth", "google_workspace", forge, nil),
		"an invalid vendor key":    box("broken", "gmail", "oauth", "google_workspace", broken, nil),
		"personal Gmail":           box("personal", "gmail", "oauth", "gmail", zap, nil),
		"switched off by its user": box("off", "gmail", "oauth", "google_workspace", zap, nil),
	}
	exec(`UPDATE email_accounts SET status = 'inactive' WHERE id = $1`, ineligible["switched off by its user"])
	unlinked := box("unlinked", "gmail", "delegated", "google_workspace", zap, nil)
	exec(`UPDATE email_accounts SET vendor_mailbox_id = '' WHERE id = $1`, unlinked)
	ineligible["no vendor mailbox id"] = unlinked

	ours := func() []uuid.UUID {
		t.Helper()
		cands, err := repo.AutoSwitchCandidates(ctx, mailvendor.ConsentVendors(), 10000)
		if err != nil {
			t.Fatal(err)
		}
		var out []uuid.UUID
		for _, c := range cands {
			if c.OrgID == f.org {
				if c.OwnerID != f.owner || c.ConnectionID != zap {
					t.Fatalf("candidate %+v", c)
				}
				out = append(out, c.AccountID)
			}
		}
		return out
	}
	got := ours()
	for _, id := range eligible {
		if !slices.Contains(got, id) {
			t.Fatalf("eligible %s missing from %v", id, got)
		}
	}
	for why, id := range ineligible {
		if slices.Contains(got, id) {
			t.Fatalf("%s was a candidate", why)
		}
	}

	claimed, err := repo.StartSigninSwitch(ctx, f.org, eligible, true)
	if err != nil || len(claimed) != len(eligible) {
		t.Fatalf("claim = %v, %v", claimed, err)
	}
	if again, _ := repo.StartSigninSwitch(ctx, f.org, eligible, true); len(again) != 0 {
		t.Fatalf("an in-flight mailbox was claimed twice: %v", again)
	}
	if other, _ := repo.StartSigninSwitch(ctx, f.other, eligible, false); len(other) != 0 {
		t.Fatal("another workspace started the switch")
	}
	if left := ours(); len(left) != 0 {
		t.Fatalf("claimed mailboxes are still candidates: %v", left)
	}

	// A member's click starts at once, whatever the automatic switch did.
	if manual, _ := repo.StartSigninSwitch(ctx, f.org, []uuid.UUID{older}, false); len(manual) != 1 {
		t.Fatal("a member could not start a reconnect")
	}

	if err := repo.SetSigninSwitch(ctx, f.org, delegated, "failed", "a login challenge"); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetSigninSwitch(ctx, f.org, unknown, "connected", ""); err != nil {
		t.Fatal(err)
	}
	st, err := repo.SigninState(ctx, f.org, delegated)
	if err != nil || st == nil || st.Switch == nil || st.Switch.Status != "failed" || st.Switch.Message != "a login challenge" || !st.Switch.Automatic ||
		st.Vendor == nil || st.Vendor.ID != zap || st.Vendor.Vendor != "zapmail" || st.AuthMethod != "delegated" {
		t.Fatalf("state = %+v, %v", st, err)
	}
	if st, _ := repo.SigninState(ctx, f.other, delegated); st != nil {
		t.Fatal("another workspace read the mailbox")
	}
	if st, _ := repo.SigninState(ctx, f.org, ineligible["full mail access"]); st == nil || !slices.Equal(st.GrantedScopes, full) || st.Switch != nil {
		t.Fatalf("full = %+v", st)
	}

	// Inside the week nothing is retried; past it a failure is, a connected mailbox never.
	if left := ours(); len(left) != 0 {
		t.Fatalf("retried inside the week: %v", left)
	}
	exec(`UPDATE email_accounts SET signin_switch_at = now() - $2::interval WHERE id = ANY($1)`, []uuid.UUID{delegated, unknown},
		SigninSwitchRetry+time.Hour)
	if left := ours(); !slices.Equal(left, []uuid.UUID{delegated}) {
		t.Fatalf("after a week = %v, want only the failed one", left)
	}
}
