package jobs

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/models"
)

// Every invalid-token attempt on a live self-host was the mailbox re-reading
// its own mail: 88 of 211 a message whose token had expired, 67 the Sent copy
// carrying the recipient's token, and 56 a reconnected mailbox replaying a
// history whose tokens had cascaded away with its previous row. None was
// tampering, and three in a day blocks a mailbox from the pool for a month.
func TestWarmupTokenIsOwnMail(t *testing.T) {
	mailbox := uuid.New()
	partner := uuid.New()
	grace := time.Duration(config.WarmupReconnectGraceMinutes) * time.Minute

	own := &models.WarmupToken{RecipientAccountID: mailbox, SenderAccountID: partner}
	sentByMe := &models.WarmupToken{RecipientAccountID: partner, SenderAccountID: mailbox}
	strangers := &models.WarmupToken{RecipientAccountID: partner, SenderAccountID: uuid.New()}

	old := grace * 4

	tests := []struct {
		name   string
		folder string
		tok    *models.WarmupToken
		age    time.Duration
		want   bool
	}{
		{"sent copy carries the recipient's token", models.FolderSent, strangers, old, true},
		{"expired token of mail this mailbox received", models.FolderInbox, own, old, true},
		{"consumed token of mail this mailbox sent", models.FolderInbox, sentByMe, old, true},
		{"first sync after a reconnect, token cascaded away", models.FolderInbox, nil, grace / 2, true},
		{"a token that belongs to neither side is signal", models.FolderInbox, strangers, old, false},
		{"an unknown token on an established mailbox is signal", models.FolderInbox, nil, old, false},
		{"an unknown token with no mailbox age is signal", models.FolderInbox, nil, -1, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := warmupTokenIsOwnMail(tc.folder, mailbox, tc.tok, tc.age); got != tc.want {
				t.Errorf("warmupTokenIsOwnMail = %v, want %v", got, tc.want)
			}
		})
	}
}
