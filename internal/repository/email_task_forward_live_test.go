package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// A forward's message is stored beside the note and read back at send time.
// Dropping either column from the insert or the select sends the note alone.
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/<db>?sslmode=disable \
//	  go test ./internal/repository/ -run LiveEmailTaskForward -v
func TestLiveEmailTaskForwardRoundTrip(t *testing.T) {
	_, pool := liveContactDB(t)
	ctx := context.Background()

	org, user, account := uuid.New(), uuid.New(), uuid.New()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("setup %q: %v", sql, err)
		}
	}
	exec(`INSERT INTO users (id, first_name, last_name, email) VALUES ($1, 'Fwd', 'Test', $2)`,
		user, "fwd-"+uuid.NewString()+"@example.test")
	exec(`INSERT INTO organizations (id, name, owner_user_id) VALUES ($1, 'forward test', $2)`, org, user)
	exec(`INSERT INTO email_accounts
	        (id, user_id, organization_id, email, name, signature_plain, signature_html, provider)
	      VALUES ($1, $2, $3, $4, 'Fwd Test', '', '', 'smtp_imap')`,
		account, user, org, "box-"+uuid.NewString()+"@example.test")
	t.Cleanup(func() {
		for _, stmt := range []struct {
			sql string
			arg any
		}{
			{`DELETE FROM email_accounts WHERE id = $1`, account},
			{`DELETE FROM organizations WHERE id = $1`, org},
			{`DELETE FROM users WHERE id = $1`, user},
		} {
			if _, err := pool.Exec(context.Background(), stmt.sql, stmt.arg); err != nil {
				t.Errorf("cleanup %q: %v", stmt.sql, err)
			}
		}
	})

	repo := NewTaskRepository(pool)
	at := time.Now().Add(time.Minute)
	const forwardedPlain = "---------- Forwarded message ---------\nOriginal"
	for _, tc := range []struct {
		name, note  string
		html, plain string
		// preview is what the Scheduled view lists as the body.
		preview string
	}{
		{"forward", "FYI", `<div class="gmail_quote">Forwarded message</div>`, forwardedPlain, "FYI"},
		{"forward without a note", "", `<div class="gmail_quote">Forwarded message</div>`, forwardedPlain, forwardedPlain},
		{"reply", "FYI", "", "", "FYI"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			taskID := uuid.New()
			err := repo.CreateEmailTaskFull(ctx,
				&Task{ID: taskID, TaskType: "email", EmailAccountID: account, Status: "pending", ScheduledAt: &at},
				&EmailTask{
					TaskID: taskID, To: []string{"new@example.test"}, Subject: "Fwd: x",
					Body: tc.note, BodyHTML: tc.note, BodyPlain: tc.note,
					ForwardedHTML: tc.html, ForwardedPlain: tc.plain,
				})
			if err != nil {
				t.Fatalf("create: %v", err)
			}
			got, err := repo.GetEmailTask(ctx, taskID)
			if err != nil || got == nil {
				t.Fatalf("read back: %v (%v)", err, got)
			}
			if got.ForwardedHTML != tc.html || got.ForwardedPlain != tc.plain {
				t.Fatalf("forwarded message did not survive: html %q plain %q", got.ForwardedHTML, got.ForwardedPlain)
			}
			listed, err := repo.ListScheduledForUser(ctx, user, 50)
			if err != nil {
				t.Fatalf("list scheduled: %v", err)
			}
			preview, found := "", false
			for _, it := range listed {
				if it.TaskID == taskID {
					preview, found = it.Body, true
				}
			}
			if !found || preview != tc.preview {
				t.Fatalf("scheduled preview %q (listed %v), want %q", preview, found, tc.preview)
			}
		})
	}
}
