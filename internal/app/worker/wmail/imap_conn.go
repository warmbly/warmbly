package wmail

import (
	"context"
	"time"

	goimap "github.com/emersion/go-imap/v2"
	"github.com/warmbly/warmbly/internal/client/smtpimap/imap"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// ImapConn is everything the worker drives on one IMAP connection.
// SmtpImapData holds this interface rather than *imap.Client so a sync pass
// can be run against a fake and its round trips counted; production always
// holds an *imap.Client.
type ImapConn interface {
	// Sync pass.
	Folders() ([]models.Mailbox, *errx.MailError)
	// FolderOverflow is how many folders the last listing left out for the
	// cap, FolderConflicts how many it left out for a duplicate UIDVALIDITY.
	FolderOverflow() int
	FolderConflicts() int
	// HasCondStore picks the incremental strategy: mod-sequences when the
	// server has CONDSTORE, UIDNEXT plus a periodic flag scan when it does not.
	HasCondStore() bool
	ReleaseMailbox()
	SelectForSync(mailbox string) (uint32, *errx.MailError)
	SearchChangedSince(modSeq uint64) ([]goimap.UID, *errx.MailError)
	SearchNewSince(uidNext uint32) ([]goimap.UID, *errx.MailError)
	FetchFlags(ctx context.Context, uidFrom uint32) (map[uint32]imap.FlagState, *errx.MailError)
	SearchSince(since time.Time) ([]goimap.UID, *errx.MailError)
	FetchEnvelopes(ctx context.Context, uids []goimap.UID) ([]*imap.Fetched, *errx.MailError)
	FetchBody(f *imap.Fetched)

	// Send path.
	AppendToSent(ctx context.Context, raw []byte, sentAt time.Time) error

	// Warmup actions.
	MarkAsRead(ctx context.Context, mailboxName string, uid uint32) error
	// SetSeen is the unibox's read/unread relay: many UIDs in one folder, in
	// one STORE, in either direction.
	SetSeen(ctx context.Context, mailboxName string, uids []uint32, seen bool) error
	MarkImportant(ctx context.Context, mailboxName string, uid uint32) error
	MoveToFolder(ctx context.Context, sourceMailbox, dstFolder string, uid uint32) error
	RemoveFromSpam(ctx context.Context, sourceMailbox, inboxName string, uid uint32) error
}

var _ ImapConn = (*imap.Client)(nil)
