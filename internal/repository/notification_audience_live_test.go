package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

func TestLiveNotificationAudienceRevocation(t *testing.T) {
	for _, revoke := range []string{"restricted", "removed", "unaccepted"} {
		t.Run(revoke, func(t *testing.T) {
			_, pool := liveContactDB(t)
			f := newSharedOrgFixture(t, pool)
			ctx := context.Background()
			repo := NewNotificationRepository(pool)
			due := time.Now().Add(-time.Minute)
			var noticeIDs []uuid.UUID
			for _, category := range []models.NotificationCategory{models.NotifCampaignPaused, models.NotifSecuritySignIn} {
				var org *uuid.UUID
				if category == models.NotifCampaignPaused {
					org = &f.org
				}
				n, err := repo.Create(ctx, &models.Notification{UserID: f.mate, OrganizationID: org, Category: category, Title: "private", EmailState: "pending", EmailDueAt: &due})
				if err != nil {
					t.Fatal(err)
				}
				noticeIDs = append(noticeIDs, n.ID)
			}
			for _, unread := range []bool{false, true} {
				rows, err := repo.List(ctx, f.mate, 50, unread)
				if err != nil || len(rows) != 2 {
					t.Fatalf("unrestricted feed: len=%d err=%v", len(rows), err)
				}
			}
			if n, err := repo.CountUnread(ctx, f.mate); err != nil || n != 2 {
				t.Fatalf("unrestricted count=%d err=%v", n, err)
			}
			// The resource creator is a member, not an authorization shortcut.
			if _, err := pool.Exec(ctx, `UPDATE campaigns SET user_id=$1 WHERE id=$2`, f.mate, f.campaign); err != nil {
				t.Fatal(err)
			}
			audience := NewEventAudienceRepository(pool)
			org, err := audience.ResourceOrganization(ctx, "campaign", f.campaign)
			if err != nil || org != f.org {
				t.Fatalf("resource org=%v err=%v", org, err)
			}
			if allowed, err := audience.CanAddressUser(ctx, f.org, f.mate); err != nil || !allowed {
				t.Fatalf("unrestricted owner=%v err=%v", allowed, err)
			}
			query := `UPDATE organization_members SET access_scope='restricted' WHERE organization_id=$1 AND user_id=$2`
			if revoke == "removed" {
				query = `DELETE FROM organization_members WHERE organization_id=$1 AND user_id=$2`
			} else if revoke == "unaccepted" {
				query = `UPDATE organization_members SET accepted_at=NULL WHERE organization_id=$1 AND user_id=$2`
			}
			if _, err := pool.Exec(ctx, query, f.org, f.mate); err != nil {
				t.Fatal(err)
			}
			if allowed, err := audience.CanAddressUser(ctx, f.org, f.mate); err != nil || allowed {
				t.Fatalf("revoked creator=%v err=%v", allowed, err)
			}
			for _, unread := range []bool{false, true} {
				rows, err := repo.List(ctx, f.mate, 50, unread)
				if err != nil || len(rows) != 1 || rows[0].ID != noticeIDs[1] {
					t.Fatalf("revoked feed: %+v err=%v", rows, err)
				}
			}
			if n, err := repo.CountUnread(ctx, f.mate); err != nil || n != 1 {
				t.Fatalf("revoked count=%d err=%v", n, err)
			}
			claimed, err := repo.ClaimDueEmails(ctx)
			if err != nil {
				t.Fatal(err)
			}
			for _, n := range claimed {
				if n.ID == noticeIDs[0] {
					t.Fatal("revoked content claimed for deferred email")
				}
			}
			var state string
			if err := pool.QueryRow(ctx, `SELECT email_state FROM notifications WHERE id=$1`, noticeIDs[0]).Scan(&state); err != nil || state != "skipped" {
				t.Fatalf("revoked email_state=%q err=%v", state, err)
			}
			if _, err := repo.Create(ctx, &models.Notification{UserID: f.mate, OrganizationID: &f.org, Category: models.NotifCampaignPaused, Title: "denied"}); err == nil {
				t.Fatal("revoked member persisted a new workspace notice")
			}
		})
	}
}
