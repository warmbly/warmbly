package notification

import (
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

func TestPartitionEmails(t *testing.T) {
	org := uuid.New()
	alice, bob := uuid.New(), uuid.New()
	shared := func(user uuid.UUID, group string) models.Notification {
		return models.Notification{ID: uuid.New(), UserID: user, OrganizationID: &org, GroupKey: group}
	}

	t.Run("a burst of shared events becomes one digest each", func(t *testing.T) {
		var claimed []models.Notification
		for _, g := range []string{"m1", "m2", "m3", "m4"} {
			claimed = append(claimed, shared(alice, g), shared(bob, g))
		}
		groups, byUser := partitionEmails(claimed)
		if len(groups) != 0 || len(byUser) != 2 || len(byUser[alice]) != 4 || len(byUser[bob]) != 4 {
			t.Fatalf("groups=%d alice=%d bob=%d, want 0, 4, 4", len(groups), len(byUser[alice]), len(byUser[bob]))
		}
	})

	t.Run("a lone shared event goes out once to everyone", func(t *testing.T) {
		groups, byUser := partitionEmails([]models.Notification{shared(alice, "m1"), shared(bob, "m1")})
		if len(groups) != 1 || len(groups[0]) != 2 || len(byUser) != 0 {
			t.Fatalf("groups=%v byUser=%v, want one shared email", groups, byUser)
		}
	})

	t.Run("anyone with more pending takes the shared event into the digest", func(t *testing.T) {
		claimed := []models.Notification{shared(alice, "m1"), shared(bob, "m1"), {ID: uuid.New(), UserID: alice}}
		groups, byUser := partitionEmails(claimed)
		if len(groups) != 0 || len(byUser[alice]) != 2 || len(byUser[bob]) != 1 {
			t.Fatalf("groups=%d alice=%d bob=%d, want 0, 2, 1", len(groups), len(byUser[alice]), len(byUser[bob]))
		}
	})
}
