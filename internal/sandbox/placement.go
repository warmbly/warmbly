package sandbox

import (
	"context"
	"fmt"
	"hash/fnv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/warmbly/warmbly/internal/pkg/warmlint"
)

// sandboxSeeds are the instance placement panel: test inboxes on domains of
// their own, so a sandbox placement test has somewhere to land. The mail host
// is stored so results group by provider as they would against real seeds.
var sandboxSeeds = []struct {
	id    uuid.UUID
	email string
	name  string
	host  string
}{
	{uuid.MustParse("5eed0000-aaaa-0000-0000-000000000001"), "panel.one@gmail-seeds.test", "Gmail seed 1", "gmail"},
	{uuid.MustParse("5eed0000-aaaa-0000-0000-000000000002"), "panel.two@gmail-seeds.test", "Gmail seed 2", "gmail"},
	{uuid.MustParse("5eed0000-aaaa-0000-0000-000000000003"), "panel@workspace-seeds.test", "Google Workspace seed", "google_workspace"},
	{uuid.MustParse("5eed0000-aaaa-0000-0000-000000000004"), "panel@m365-seeds.test", "Microsoft 365 seed", "microsoft365"},
	{uuid.MustParse("5eed0000-aaaa-0000-0000-000000000005"), "panel@outlook-seeds.test", "Outlook.com seed", "outlook"},
	{uuid.MustParse("5eed0000-aaaa-0000-0000-000000000006"), "panel@yahoo-seeds.test", "Yahoo seed", "yahoo"},
}

// seedPlacementPanel adds the seed inboxes. They never warm and never send,
// like any seed; the credential repair points them at dovecot.
func seedPlacementPanel(ctx context.Context, pool *pgxpool.Pool) error {
	for _, s := range sandboxSeeds {
		if _, err := pool.Exec(ctx, `
			INSERT INTO email_accounts (
				id, user_id, organization_id, worker_id, email, name,
				signature_plain, signature_html, provider, status, mail_host, seed_scope, created_at
			) VALUES ($1, $2, $3, $4, $5, $6, '', '', 'smtp_imap', 'active', $7, 'instance', NOW() - INTERVAL '90 days')
			ON CONFLICT (id) DO UPDATE SET
				worker_id = EXCLUDED.worker_id,
				status = 'active',
				mail_host = EXCLUDED.mail_host,
				seed_scope = 'instance',
				warmup = NULL,
				updated_at = NOW()`,
			s.id, sandboxUser, sandboxOrg, sandboxWorker, s.email, s.name, s.host); err != nil {
			return fmt.Errorf("seed inbox %s: %w", s.email, err)
		}
	}
	return nil
}

// seedFolder plays the recipient's spam filter for a copy landing in a seed:
// copy the rules pass dislikes goes to Junk, and a fixed share of the rest
// does too, harder on Microsoft and Yahoo and harder again for tracked copy,
// so a test and its tracking comparison both have something to show.
func seedFolder(host string, msg *mailpitMessage) string {
	score := warmlint.Score(msg.Subject, msg.HTML, msg.Text).Score
	if score < 70 {
		return "Junk"
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(msg.MessageID))
	roll := h.Sum32() % 100
	threshold := uint32(10)
	switch host {
	case "microsoft365", "outlook":
		threshold = 25
	case "yahoo":
		threshold = 20
	}
	if strings.Contains(msg.HTML, "/t/o/") || strings.Contains(msg.HTML, "/c/") {
		threshold += 15
	}
	if roll < threshold {
		return "Junk"
	}
	return "INBOX"
}
