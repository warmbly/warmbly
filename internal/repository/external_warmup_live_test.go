package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/models"
)

// The sweep's SQL and the arrival check's Go have to hide the same mail:
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/<db>?sslmode=disable \
//	  go test ./internal/repository/ -run LiveExternalWarmup -v
func TestLiveExternalWarmupSweepMatchesTheArrivalRule(t *testing.T) {
	handle := liveUniboxFolderDB(t)
	f := newUniboxFolderFixture(t, handle.Pool)
	other := newUniboxFolderFixture(t, handle.Pool)
	unibox := NewUniboxRepository(handle)
	repo := NewExternalWarmupRepository(handle.Pool)
	ctx := context.Background()

	rule := models.ExternalWarmupSettings{HideWarmblyMarked: true, Tags: []string{"WRM-7Q2X", "lemwarm-abc"}}
	messages := []*models.EmailMessageStoreData{
		{Subject: "Quick question", BodyText: "Thanks for the notes. wrm-7q2x"},
		{Subject: "Garden update LEMWARM-ABC", BodyText: "The tomatoes are in."},
		{Subject: "Life with a rescue dog", Snippet: "Any advice?", Flags: []string{config.WarmupVerifyHeader + ": " + uuid.NewString()}},
		{Subject: "Old marker", Flags: []string{"X-Warmbly-Token:" + uuid.NewString()}},
		{Subject: "Empty marker value", Flags: []string{config.WarmupVerifyHeader + ":"}},
		{Subject: "Lunch on Friday?", BodyText: "WRM-7Q is close but not the tag."},
		{Subject: "Invoice 2041", Flags: []string{`\Seen`}},
	}
	want := map[uuid.UUID]bool{}
	for _, m := range messages {
		id := insertExternalWarmupMessage(t, unibox, f, m)
		if rule.Matches(m) {
			want[id] = true
		}
	}
	if len(want) != 4 {
		t.Fatalf("fixture drifted: the Go rule matches %d messages, want 4", len(want))
	}
	foreign := insertExternalWarmupMessage(t, unibox, other, &models.EmailMessageStoreData{Subject: "wrm-7q2x"})

	deleted, err := repo.DeleteExternalWarmup(ctx, f.org, f.mailbox, rule, 3)
	if err != nil {
		t.Fatalf("DeleteExternalWarmup: %v", err)
	}
	more, err := repo.DeleteExternalWarmup(ctx, f.org, f.mailbox, rule, 3)
	if err != nil {
		t.Fatalf("DeleteExternalWarmup second batch: %v", err)
	}
	if len(deleted) != 3 || len(more) != 1 {
		t.Fatalf("batches removed %d then %d, want 3 then 1", len(deleted), len(more))
	}
	for _, e := range append(deleted, more...) {
		if !want[e.Message.ID] {
			t.Errorf("SQL removed %s (%q), which the arrival rule keeps", e.Message.ID, e.Message.Subject)
		}
		if e.UserID != f.user || e.Message.EmailID != f.mailbox {
			t.Errorf("removed row reports owner %s mailbox %s", e.UserID, e.Message.EmailID)
		}
		delete(want, e.Message.ID)
	}
	if len(want) != 0 {
		t.Errorf("SQL kept %d messages the arrival rule hides", len(want))
	}

	// The tenant predicate: another workspace's mailbox is never touched, even when named.
	if got, err := repo.DeleteExternalWarmup(ctx, f.org, other.mailbox, rule, 10); err != nil || len(got) != 0 {
		t.Fatalf("a foreign mailbox lost %d rows (err %v)", len(got), err)
	}
	if _, err := unibox.GetByID(ctx, other.user, foreign); err != nil {
		t.Fatalf("foreign message is gone: %v", err)
	}
}

func TestLiveExternalWarmupRulesListsOnlyEnabledWorkspaces(t *testing.T) {
	handle := liveUniboxFolderDB(t)
	on, off, tagged := newUniboxFolderFixture(t, handle.Pool), newUniboxFolderFixture(t, handle.Pool), newUniboxFolderFixture(t, handle.Pool)
	settings := NewAdvancedOutreachRepository(handle.Pool)
	repo := NewExternalWarmupRepository(handle.Pool)
	ctx := context.Background()

	for f, rule := range map[*uniboxFolderFixture]models.ExternalWarmupSettings{
		on:     {HideWarmblyMarked: true},
		off:    {},
		tagged: {Tags: []string{"wrm-7q2x"}},
	} {
		s := models.DefaultAdvancedOutreachSettings()
		s.ExternalWarmup = rule
		if err := settings.UpsertOutreachSettings(ctx, f.org, f.user, &s); err != nil {
			t.Fatalf("UpsertOutreachSettings: %v", err)
		}
		t.Cleanup(func() {
			_, _ = handle.Pool.Exec(context.Background(), `DELETE FROM outreach_settings WHERE organization_id = $1`, f.org)
		})
	}

	rules, err := repo.ExternalWarmupRules(ctx)
	if err != nil {
		t.Fatalf("ExternalWarmupRules: %v", err)
	}
	if _, ok := rules[off.org]; ok {
		t.Error("a workspace with nothing to hide was listed")
	}
	if !rules[on.org].HideWarmblyMarked || len(rules[tagged.org].Tags) != 1 {
		t.Errorf("enabled workspaces missing: %+v", rules)
	}
	if got, err := repo.ExternalWarmupRule(ctx, tagged.org); err != nil || got.Tags[0] != "wrm-7q2x" {
		t.Errorf("ExternalWarmupRule = %+v, %v", got, err)
	}
	if got, err := repo.ExternalWarmupRule(ctx, uuid.New()); err != nil || got.Enabled() {
		t.Errorf("a workspace with no settings row = %+v, %v", got, err)
	}
}

func insertExternalWarmupMessage(t *testing.T, repo UniboxRepository, f *uniboxFolderFixture, m *models.EmailMessageStoreData) uuid.UUID {
	t.Helper()
	now := time.Now().UTC()
	m.ID, m.EmailID, m.Folder = uuid.New(), f.mailbox, models.FolderInbox
	m.ThreadID, m.MessageID = "thread-"+m.ID.String(), "<"+m.ID.String()+"@test.local>"
	m.FromAddr, m.ToAddr = []string{"partner@test.local"}, []string{"me@test.local"}
	m.InternalDate, m.SentDate, m.CreatedAt, m.UpdatedAt = now, now, now, now
	if err := repo.CreateEntry(context.Background(), f.user, m); err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	return m.ID
}
