package repository

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// A campaign copied into another workspace arrives as a draft carrying its
// configuration and nothing that only means something in the source.
//
//	WARMBLY_TEST_DB=postgres://... go test ./internal/repository/ -run LiveCopyCampaignToOrganization -v
func TestLiveCopyCampaignToOrganization(t *testing.T) {
	handle, pool := liveContactDB(t)
	f := newSharedOrgFixture(t, pool)
	ctx := context.Background()
	repo := NewCampaignRepostory(handle)

	dest := uuid.New()
	mailbox, tag, folder, category := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	first, second := uuid.New(), uuid.New()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("fixture %q: %v", sql[:min(60, len(sql))], err)
		}
	}
	exec(`INSERT INTO organizations (id, name, slug, owner_user_id) VALUES ($1, 'Client', $2, $3)`, dest, "copy-"+dest.String()[:8], f.owner)
	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM campaigns WHERE organization_id = $1`, dest)
		_, _ = pool.Exec(c, `DELETE FROM organizations WHERE id = $1`, dest)
		_, _ = pool.Exec(c, `DELETE FROM campaign_email_tags WHERE campaign_id = $1`, f.campaign)
		_, _ = pool.Exec(c, `DELETE FROM campaign_folders WHERE campaign_id = $1`, f.campaign)
		_, _ = pool.Exec(c, `DELETE FROM campaign_senders WHERE campaign_id = $1`, f.campaign)
		_, _ = pool.Exec(c, `DELETE FROM campaign_contact_progress WHERE campaign_id = $1`, f.campaign)
		_, _ = pool.Exec(c, `DELETE FROM sequences WHERE campaign_id = $1`, f.campaign)
		_, _ = pool.Exec(c, `DELETE FROM email_accounts WHERE id = $1`, mailbox)
		_, _ = pool.Exec(c, `DELETE FROM tags WHERE id = $1`, tag)
		_, _ = pool.Exec(c, `DELETE FROM folders WHERE id = $1`, folder)
	})

	exec(`UPDATE campaigns SET status = 'active', tracking_domain = 'track.agency.test', tracking_domain_verified = true,
	      ramp_level = 30, guardrail_reason = 'tripped', stop_on_reply = true, daily_limit = 77 WHERE id = $1`, f.campaign)
	exec(`INSERT INTO sequences (id, campaign_id, organization_id, name, subject, body_plain, body_html, wait_after, position, conditions)
	      VALUES ($1, $2, $3, 'Opener', 'Hi {{.FirstName}}', 'Book here {{form_link:abc123}}', '', 0, 1,
	              jsonb_build_object('branches', jsonb_build_array(jsonb_build_object('branch_id', 'b1', 'target_step_id', $4::text))))`,
		first, f.campaign, f.org, second.String())
	exec(`INSERT INTO sequences (id, campaign_id, organization_id, name, subject, body_plain, body_html, wait_after, position, kind, action)
	      VALUES ($1, $2, $3, 'Tag them', '', '', '', 2, 2, 'action', jsonb_build_object('type', 'add_tag', 'category_id', $4::text))`,
		second, f.campaign, f.org, category.String())
	exec(`INSERT INTO email_accounts (id, user_id, organization_id, email, name, signature_plain, signature_html, provider)
	      VALUES ($1, $2, $3, $4, 'Sender', '', '', 'smtp_imap')`, mailbox, f.owner, f.org, mailbox.String()+"@example.test")
	exec(`INSERT INTO campaign_senders (campaign_id, email_account_id, weight, enabled) VALUES ($1, $2, 1, true)`, f.campaign, mailbox)
	exec(`INSERT INTO tags (id, user_id, organization_id, title, color, position) VALUES ($1, $2, $3, 'Agency mailboxes', '#0ea5e9', 1)`, tag, f.owner, f.org)
	exec(`INSERT INTO campaign_email_tags (campaign_id, tag_id) VALUES ($1, $2)`, f.campaign, tag)
	exec(`INSERT INTO folders (id, user_id, organization_id, title, color, position) VALUES ($1, $2, $3, 'Agency', '#0ea5e9', 1)`, folder, f.owner, f.org)
	exec(`INSERT INTO campaign_folders (campaign_id, folder_id) VALUES ($1, $2)`, f.campaign, folder)
	exec(`INSERT INTO campaign_leads (campaign_id, contact_id, position) VALUES ($1, $2, 0)`, f.campaign, f.contact)
	exec(`INSERT INTO campaign_contact_progress (campaign_id, contact_id, sequence_id, sent_at) VALUES ($1, $2, $3, NOW())`, f.campaign, f.contact, first)

	newID := uuid.New()
	copied, review, err := repo.CopyToOrganization(ctx, CopyCampaignAcrossInput{
		SourceOrgID: f.org, DestOrgID: dest, SourceID: f.campaign, NewID: newID, UserID: f.mate,
	})
	if err != nil {
		t.Fatal(err)
	}
	if copied.Status != "draft" || copied.OrganizationID == nil || *copied.OrganizationID != dest {
		t.Fatalf("copy = status %q org %v", copied.Status, copied.OrganizationID)
	}
	if !copied.StopOnReply || copied.DailyLimit != 77 || copied.RampLevel != 0 || copied.GuardrailReason != "" {
		t.Fatalf("settings did not travel as configuration: %+v", copied)
	}
	if copied.TrackingDomain != "" || copied.TrackingDomainVerified {
		t.Fatalf("tracking domain travelled: %q", copied.TrackingDomain)
	}

	count := func(sql string) int {
		t.Helper()
		var n int
		if err := pool.QueryRow(ctx, sql, newID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	for name, sql := range map[string]string{
		"leads":    `SELECT count(*) FROM campaign_leads WHERE campaign_id = $1`,
		"progress": `SELECT count(*) FROM campaign_contact_progress WHERE campaign_id = $1`,
		"senders":  `SELECT count(*) FROM campaign_senders WHERE campaign_id = $1`,
		"tags":     `SELECT count(*) FROM campaign_email_tags WHERE campaign_id = $1`,
		"folders":  `SELECT count(*) FROM campaign_folders WHERE campaign_id = $1`,
		"logs":     `SELECT count(*) FROM campaign_logs WHERE campaign_id = $1`,
		"stats":    `SELECT count(*) FROM campaign_daily_sends WHERE campaign_id = $1`,
	} {
		if n := count(sql); n != 0 {
			t.Errorf("%s travelled: %d rows", name, n)
		}
	}
	if n := count(`SELECT count(*) FROM sequences WHERE campaign_id = $1 AND organization_id <> (SELECT organization_id FROM campaigns WHERE id = $1)`); n != 0 {
		t.Fatalf("%d steps kept the source workspace", n)
	}

	steps, err := repo.GetSequencesRoutingByCampaignID(ctx, newID)
	if err != nil || len(steps) != 2 {
		t.Fatalf("steps = %d, %v", len(steps), err)
	}
	var opener, action models.Sequence
	for _, s := range steps {
		if s.Position == 1 {
			opener = s
		} else {
			action = s
		}
	}
	var cond models.BranchConditions
	if err := json.Unmarshal(opener.Conditions, &cond); err != nil || len(cond.Branches) != 1 || cond.Branches[0].TargetSequenceID == nil || *cond.Branches[0].TargetSequenceID != action.ID {
		t.Fatalf("branch not remapped onto the copy: %s", opener.Conditions)
	}
	var cfg models.ActionConfig
	if err := json.Unmarshal(action.Action, &cfg); err != nil || cfg.Type != "add_tag" || cfg.CategoryID != nil {
		t.Fatalf("action config = %s", action.Action)
	}
	fields := map[uuid.UUID][]string{}
	for _, r := range review {
		fields[r.StepID] = r.Fields
	}
	if !slices.Contains(fields[action.ID], "category_id") || !slices.Contains(fields[opener.ID], "form_link") {
		t.Fatalf("review = %+v", review)
	}

	// The source workspace is part of the predicate: a campaign of another
	// workspace is not found even with its real id.
	if _, _, err := repo.CopyToOrganization(ctx, CopyCampaignAcrossInput{
		SourceOrgID: dest, DestOrgID: dest, SourceID: f.campaign, NewID: uuid.New(), UserID: f.mate,
	}); !errors.Is(err, errx.ErrResourceNotFound) {
		t.Fatalf("foreign source copied: %v", err)
	}
}
