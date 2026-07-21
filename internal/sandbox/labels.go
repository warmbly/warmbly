package sandbox

// Label registry for the sandbox user, plus the bindings that make labels
// visible across the product: mailbox tags, campaign folders, contact
// categories, and inbox thread labels. Idempotent like the rest of the seeder.

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	sbxFolderOutbound = uuid.MustParse("66666666-aaaa-0000-0000-000000000001")
	sbxFolderNurture  = uuid.MustParse("66666666-aaaa-0000-0000-000000000002")
	sbxTagVIP         = uuid.MustParse("66666666-aaaa-0000-0000-000000000011")
	sbxTagCold        = uuid.MustParse("66666666-aaaa-0000-0000-000000000012")
	sbxTagAgency      = uuid.MustParse("66666666-aaaa-0000-0000-000000000013")
	sbxCatLead        = uuid.MustParse("66666666-aaaa-0000-0000-000000000021")
	sbxCatCustomer    = uuid.MustParse("66666666-aaaa-0000-0000-000000000022")
	sbxCatChurn       = uuid.MustParse("66666666-aaaa-0000-0000-000000000023")
)

func seedLabels(ctx context.Context, pool *pgxpool.Pool) error {
	groups := []struct {
		table string
		id    uuid.UUID
		title string
		color string
		pos   int
	}{
		{"folders", sbxFolderOutbound, "Outbound", "#0ea5e9", 0},
		{"folders", sbxFolderNurture, "Nurture", "#10b981", 1},
		{"tags", sbxTagVIP, "VIP", "#a855f7", 0},
		{"tags", sbxTagCold, "Cold", "#64748b", 1},
		{"tags", sbxTagAgency, "Agency", "#f59e0b", 2},
		{"categories", sbxCatLead, "Lead", "#f97316", 0},
		{"categories", sbxCatCustomer, "Customer", "#10b981", 1},
		{"categories", sbxCatChurn, "Churn risk", "#ef4444", 2},
	}
	for _, g := range groups {
		if _, err := pool.Exec(ctx, `
			INSERT INTO `+g.table+` (id, user_id, title, color, position, created_at, updated_at)
			VALUES ($1,$2,$3,$4,$5,NOW(),NOW())
			ON CONFLICT (id) DO UPDATE SET
				title = EXCLUDED.title,
				color = EXCLUDED.color,
				position = EXCLUDED.position,
				updated_at = NOW()
		`, g.id, sandboxUser, g.title, g.color, g.pos); err != nil {
			return fmt.Errorf("%s %s: %w", g.table, g.title, err)
		}
	}

	// Mailbox tags: every sender gets one so tag-based selection and the list
	// chips have data everywhere. First third VIP (launch senders), next third
	// Agency, the rest Cold; a couple carry a second tag for realism.
	for i, m := range sandboxMailboxes {
		tag := sbxTagCold
		switch {
		case i < len(sandboxMailboxes)/3:
			tag = sbxTagVIP
		case i < 2*len(sandboxMailboxes)/3:
			tag = sbxTagAgency
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO email_tags (email_id, tag_id) VALUES ($1,$2)
			ON CONFLICT DO NOTHING
		`, m.id, tag); err != nil {
			return fmt.Errorf("email tag: %w", err)
		}
	}
	for _, dual := range []struct {
		mailbox uuid.UUID
		tag     uuid.UUID
	}{
		{sandboxMailboxes[0].id, sbxTagAgency},
		{sandboxMailboxes[7].id, sbxTagVIP},
	} {
		if _, err := pool.Exec(ctx, `
			INSERT INTO email_tags (email_id, tag_id) VALUES ($1,$2)
			ON CONFLICT DO NOTHING
		`, dual.mailbox, dual.tag); err != nil {
			return fmt.Errorf("email tag: %w", err)
		}
	}

	campaignFolders := []struct {
		campaign uuid.UUID
		folder   uuid.UUID
	}{
		{campaignLaunch, sbxFolderOutbound},
		{campaignAgency, sbxFolderOutbound},
		{campaignDormant, sbxFolderNurture},
	}
	for _, cf := range campaignFolders {
		if _, err := pool.Exec(ctx, `
			INSERT INTO campaign_folders (campaign_id, folder_id) VALUES ($1,$2)
			ON CONFLICT DO NOTHING
		`, cf.campaign, cf.folder); err != nil {
			return fmt.Errorf("campaign folder: %w", err)
		}
	}

	// Contact categories, resolved by seeded email so the binding follows the
	// roster without hardcoding contact IDs here.
	contactCats := []struct {
		email    string
		category uuid.UUID
	}{
		{"aiden.park@northwind.test", sbxCatLead},
		{"eli.grant@hooli.test", sbxCatLead},
		{"beth.chen@initech.test", sbxCatCustomer},
		{"amara.bell@brightloop.test", sbxCatCustomer},
		{"diana.fox@globex.test", sbxCatChurn},
	}
	for _, cc := range contactCats {
		if _, err := pool.Exec(ctx, `
			INSERT INTO contact_categories (contact_id, category_id)
			SELECT id, $3 FROM contacts WHERE user_id = $1 AND email = $2
			ON CONFLICT DO NOTHING
		`, sandboxUser, cc.email, cc.category); err != nil {
			return fmt.Errorf("contact category %s: %w", cc.email, err)
		}
	}

	// Inbox thread labels on the seeded conversations (thread IDs from
	// seedUniboxHistory).
	threadLabels := []struct {
		threadID string
		category uuid.UUID
	}{
		{"sbx-thread-northwind", sbxCatLead},
		{"sbx-thread-hooli", sbxCatLead},
		{"sbx-thread-brightloop", sbxCatCustomer},
		{"sbx-thread-globex-ooo", sbxCatChurn},
	}
	for _, tl := range threadLabels {
		if _, err := pool.Exec(ctx, `
			INSERT INTO unibox_thread_labels (user_id, thread_id, category_id, created_at)
			VALUES ($1,$2,$3,NOW())
			ON CONFLICT DO NOTHING
		`, sandboxUser, tl.threadID, tl.category); err != nil {
			return fmt.Errorf("thread label %s: %w", tl.threadID, err)
		}
	}
	return nil
}
