package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

// insertTemplateConversation mirrors what the /warmup/templates handler writes:
// an org-scoped row in the shared bank with no reply turns.
func insertTemplateConversation(t *testing.T, f *poolLinkFixture, segment string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := f.pool.Exec(t.Context(), `INSERT INTO warmup_conversations(id,pool_type,segment,source,subject,description,messages,status,scenario_version,rendering_version,reply_eligible,lint_passed,semantic_review,organization_id)
 VALUES($1,'premium',$2,'template',$4,'Hi, quick question about your Q4 plans.','[]','active','natural-v1','canonical-v1',true,true,'passed',$3)`,
		id, segment, f.org, "Template subject "+id.String()[:8])
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = f.pool.Exec(context.Background(), `DELETE FROM warmup_conversations WHERE id=$1`, id) })
	return id
}

func templatePick(t *testing.T, f *poolLinkFixture, segment string, sender, recipient uuid.UUID) WarmupContentPick {
	t.Helper()
	pick := contentPoolPick(t, f, segment, sender, recipient)
	pick.OrganizationID = &f.org
	pick.TemplatesOnly = true
	return pick
}

// An organization's template enters its own warmup rotation, never another
// organization's, and a sender sees it at most once per day.
func TestLiveTemplatePickScopesToOrgAndCapsPerDay(t *testing.T) {
	f, _ := lineageFixture(t)
	r := NewWarmupContentRepository(f.pool).(*warmupContentRepository)
	segment := "fixture-" + uuid.NewString()
	template := insertTemplateConversation(t, f, segment)
	ctx := t.Context()

	// Org A's sender picks org A's template.
	c, err := r.PickConversation(ctx, templatePick(t, f, segment, f.sender, f.recipient))
	if err != nil || c == nil || c.ID != template {
		t.Fatalf("own template not picked: %+v %v", c, err)
	}
	if c.Source != "template" || c.OrganizationID == nil || *c.OrganizationID != f.org {
		t.Fatalf("picked row lost its org scope: %+v", c)
	}

	// A bank draw never returns a template, whatever the organization.
	bank := templatePick(t, f, segment, f.sender, f.recipient)
	bank.TemplatesOnly, bank.IncludeAI = false, false
	if fromBank, err := r.PickConversation(ctx, bank); err != nil || fromBank != nil {
		t.Fatalf("bank draw returned a template: %+v %v", fromBank, err)
	}

	// Org B's sender can never draw org A's template.
	fB := newPoolLinkFixture(t)
	other, err := r.PickConversation(ctx, templatePick(t, fB, segment, fB.sender, fB.recipient))
	if err != nil {
		t.Fatalf("org B pick errored: %v", err)
	}
	if other != nil {
		t.Fatalf("org B drew org A's template: %+v", other)
	}

	// Same sender, new task, same day: the cap filters the template.
	if again, err := r.PickConversation(ctx, templatePick(t, f, segment, f.sender, f.recipient)); err != nil || again != nil {
		t.Fatalf("template resent within the day: %+v %v", again, err)
	}

	// A day later it is eligible again.
	if _, err := f.pool.Exec(ctx, `UPDATE warmup_conversation_mailboxes SET created_at = NOW() - INTERVAL '2 days' WHERE conversation_id=$1`, template); err != nil {
		t.Fatal(err)
	}
	next, err := r.PickConversation(ctx, templatePick(t, f, segment, f.sender, f.recipient))
	if err != nil || next == nil || next.ID != template {
		t.Fatalf("template not eligible next day: %+v %v", next, err)
	}
}

// Duplicate detection is per owner: one organization's template never blocks
// another organization or the shared library from the same wording.
func TestLiveTemplateDuplicateCheckIsPerOrganization(t *testing.T) {
	f, _ := lineageFixture(t)
	fB := newPoolLinkFixture(t)
	r := NewWarmupContentRepository(f.pool)
	ctx := t.Context()
	scenario, rendering := "natural-v1", "canonical-v1"
	subject := "Template subject " + uuid.NewString()[:8]
	row := func(org *uuid.UUID, source string) *models.WarmupConversation {
		return &models.WarmupConversation{PoolType: "premium", Source: source, Subject: subject, Description: "Body " + uuid.NewString(), Messages: []string{}, Status: "active", LintPassed: true, ReplyEligible: true, ScenarioVersion: &scenario, RenderingVersion: &rendering, SemanticReview: "passed", OrganizationID: org}
	}
	insert := func(c *models.WarmupConversation) error {
		err := r.InsertConversation(ctx, c)
		if err == nil {
			t.Cleanup(func() { _, _ = f.pool.Exec(context.Background(), `DELETE FROM warmup_conversations WHERE id=$1`, c.ID) })
		}
		return err
	}
	if err := insert(row(&f.org, "template")); err != nil {
		t.Fatalf("org A template: %v", err)
	}
	if err := insert(row(&f.org, "template")); !errors.Is(err, ErrWarmupConversationDuplicate) {
		t.Fatalf("org A repeat accepted: %v", err)
	}
	if err := insert(row(&fB.org, "template")); err != nil {
		t.Fatalf("org B blocked by org A's template: %v", err)
	}
	if err := insert(row(nil, "ai")); err != nil {
		t.Fatalf("shared library blocked by a template: %v", err)
	}
}

// Template settings default to a half share and round-trip per organization.
func TestLiveTemplateSettingsDefaultAndRoundTrip(t *testing.T) {
	f, _ := lineageFixture(t)
	r := NewWarmupContentRepository(f.pool)
	ctx := t.Context()
	got, err := r.GetTemplateSettings(ctx, f.org)
	if err != nil || got != models.DefaultWarmupTemplateSettings() {
		t.Fatalf("defaults: %+v %v", got, err)
	}
	want := models.WarmupTemplateSettings{TemplateShare: 80, TemplatesOnly: true}
	if err := r.UpsertTemplateSettings(ctx, f.org, want); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = f.pool.Exec(context.Background(), `DELETE FROM warmup_template_settings WHERE organization_id=$1`, f.org)
	})
	if got, err = r.GetTemplateSettings(ctx, f.org); err != nil || got != want {
		t.Fatalf("round trip: %+v %v", got, err)
	}
	other := newPoolLinkFixture(t)
	if got, err = r.GetTemplateSettings(ctx, other.org); err != nil || got != models.DefaultWarmupTemplateSettings() {
		t.Fatalf("another organization inherited the setting: %+v %v", got, err)
	}
}

// Placement is read per template from the sender's tokens and the receipts
// the recipient filed against them, split by the recipient's provider group.
func TestLiveTemplatePlacementCountsReceipts(t *testing.T) {
	f, _ := lineageFixture(t)
	r := NewWarmupContentRepository(f.pool)
	ctx := t.Context()
	segment := "fixture-" + uuid.NewString()
	template := insertTemplateConversation(t, f, segment)
	pick := templatePick(t, f, segment, f.sender, f.recipient)
	if c, err := r.PickConversation(ctx, pick); err != nil || c == nil {
		t.Fatalf("pick: %+v %v", c, err)
	}
	msgID := "<" + uuid.NewString() + "@fixture>"
	if _, err := f.pool.Exec(ctx, `INSERT INTO warmup_tokens(task_id,sender_account_id,recipient_account_id,content_source,conversation_id,sent_message_id)
 VALUES($1,$2,$3,'template',$4,$5)`, pick.TaskID, f.sender, f.recipient, template, msgID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = f.pool.Exec(context.Background(), `DELETE FROM warmup_tokens WHERE task_id=$1`, pick.TaskID)
	})
	since := time.Now().Add(-time.Hour)
	all, err := r.TemplatePlacement(ctx, f.org, since)
	if err != nil || all[template].Sent != 1 || all[template].Inbox != 0 {
		t.Fatalf("sent-only token: %+v %v", all[template], err)
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO warmup_received(email_account_id,internal_id,message_id,sender_account_id,first_landing)
 VALUES($1,$2,$3,$4,'inbox')`, f.recipient, uuid.New(), msgID, f.sender); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = f.pool.Exec(context.Background(), `DELETE FROM warmup_received WHERE message_id=$1`, msgID)
	})
	all, err = r.TemplatePlacement(ctx, f.org, since)
	if err != nil || all[template].Sent != 1 || all[template].Inbox != 1 {
		t.Fatalf("receipt not counted: %+v %v", all[template], err)
	}
	groups, err := r.TemplatePlacementByGroup(ctx, template, f.org, since)
	if err != nil || len(groups) != 1 || groups[0].Inbox != 1 || groups[0].Group == "" {
		t.Fatalf("by group: %+v %v", groups, err)
	}
	other := newPoolLinkFixture(t)
	if foreign, err := r.TemplatePlacementByGroup(ctx, template, other.org, since); err != nil || len(foreign) != 0 {
		t.Fatalf("another organization read the template's placement: %+v %v", foreign, err)
	}
}
