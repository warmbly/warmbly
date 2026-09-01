package wmail

import (
	"testing"

	"github.com/warmbly/warmbly/internal/models"
)

// imapCanonicalFolder decides which sidebar folder a message lands in, so a
// wrong answer here silently files mail under the wrong scope.
func TestImapCanonicalFolder(t *testing.T) {
	for _, tc := range []struct {
		name string
		box  models.Mailbox
		want string
	}{
		// Special-use attributes are authoritative.
		{"sent by attribute", models.Mailbox{Name: "Whatever", Attrs: []string{"\\Sent"}}, models.FolderSent},
		{"drafts by attribute", models.Mailbox{Name: "Whatever", Attrs: []string{"\\Drafts"}}, models.FolderDrafts},
		{"junk by attribute", models.Mailbox{Name: "Whatever", Attrs: []string{"\\Junk"}}, models.FolderSpam},
		{"trash by attribute", models.Mailbox{Name: "Whatever", Attrs: []string{"\\Trash"}}, models.FolderTrash},
		{"archive by attribute", models.Mailbox{Name: "Whatever", Attrs: []string{"\\Archive"}}, models.FolderArchive},
		{"all mail is archive", models.Mailbox{Name: "[Gmail]/All Mail", Attrs: []string{"\\All"}}, models.FolderArchive},
		// Name fallback for servers that advertise no special-use.
		{"sent by name", models.Mailbox{Name: "Sent Items"}, models.FolderSent},
		{"sent by nested name", models.Mailbox{Name: "INBOX.Sent"}, models.FolderSent},
		{"spam by name", models.Mailbox{Name: "Junk E-Mail"}, models.FolderSpam},
		{"trash by name", models.Mailbox{Name: "Deleted Items"}, models.FolderTrash},
		{"drafts by name", models.Mailbox{Name: "Drafts"}, models.FolderDrafts},
		// Anything unrecognised stays visible rather than vanishing into a
		// scope the user never opens.
		{"inbox", models.Mailbox{Name: "INBOX"}, models.FolderInbox},
		{"user folder", models.Mailbox{Name: "INBOX.Clients.Acme"}, models.FolderInbox},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := imapCanonicalFolder(&tc.box); got != tc.want {
				t.Fatalf("imapCanonicalFolder(%+v) = %q, want %q", tc.box, got, tc.want)
			}
		})
	}
}

// Drafts is imported now that the folder sidebar gives it a destination; the
// rest of the eligibility matrix lives in TestImapBackfillEligible.
func TestImapBackfillEligible_DraftsByName(t *testing.T) {
	for _, name := range []string{"Drafts", "Draft", "INBOX.Drafts"} {
		if !imapBackfillEligible(&models.Mailbox{Name: name}) {
			t.Fatalf("imapBackfillEligible(%q) = false, want true", name)
		}
	}
}
