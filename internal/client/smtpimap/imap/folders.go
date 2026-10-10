package imap

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/emersion/go-imap/v2"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// Folders lists the selectable folders of the account with the cursors the
// sync loop keys on (UIDVALIDITY, UIDNEXT and, on a CONDSTORE server,
// HIGHESTMODSEQ).
//
// "*", not "%": "%" stops at the top level, and on Gmail-over-IMAP every
// folder but INBOX lives under "[Gmail]/" (Dovecot commonly under "INBOX."),
// so Sent, Spam and Trash were never listed and never synced.
//
// The listing is capped at config.MaxEmailFolders, INBOX and the special
// folders first so a mailbox with hundreds of user folders still syncs what
// matters; FolderOverflow reports how many were left out.
func (c *Client) Folders() ([]models.Mailbox, *errx.MailError) {
	return c.foldersCapped(config.MaxEmailFolders)
}

// foldersCapped is Folders with the cap injectable, so a test can exercise
// the overflow without standing up a hundred folders.
func (c *Client) foldersCapped(limit int) ([]models.Mailbox, *errx.MailError) {
	if err := c.ensureConnected(); err != nil {
		return nil, err
	}
	c.lifecycle.RLock()
	defer c.lifecycle.RUnlock()
	defer c.begin()()

	caps := c.client.Caps()
	status := &imap.StatusOptions{
		UIDValidity: true,
		UIDNext:     true,
		NumMessages: true,
		// Asking a server without CONDSTORE for HIGHESTMODSEQ is a BAD.
		HighestModSeq: caps.Has(imap.CapCondStore),
	}
	opts, _ := listOptionsFor(caps, status)

	var all []models.Mailbox
	statuses := map[string]*imap.StatusData{}
	cmd := c.client.List("", "*", opts)
	for f := cmd.Next(); f != nil; f = cmd.Next() {
		attrs := make([]string, len(f.Attrs))
		for i := range f.Attrs {
			attrs[i] = string(f.Attrs[i])
		}
		box := models.Mailbox{Name: f.Mailbox, Attrs: attrs, Delim: delimString(f.Delim)}
		if !selectableFolder(attrs) || IsVirtualFolder(box) {
			continue
		}
		all = append(all, box)
		if f.Status != nil {
			statuses[f.Mailbox] = f.Status
		}
	}
	if err := cmd.Close(); err != nil {
		return nil, c.handleError(err)
	}

	// Before the cap, not after: a name the server listed twice would
	// otherwise spend one of the slots the cap allows and cost a real folder
	// its sync, which is the same failure this whole change is about.
	all, conflicts := dedupeByName(all)
	c.folderConflicts.Store(int32(conflicts))

	kept, overflow := rankFolders(all, limit)
	c.folderOverflow.Store(int32(overflow))

	resp := make([]models.Mailbox, 0, len(kept))
	for _, box := range kept {
		st := statuses[box.Name]
		if st == nil {
			data, err := c.client.Status(box.Name, status).Wait()
			if err != nil {
				merr := c.handleError(err)
				if merr.Code == errx.ErrMailResourceNotFound.Code {
					// Only explicit absence can retire a folder's stored state.
					continue
				}
				return nil, merr
			}
			st = data
		}
		if st == nil || st.UIDValidity == 0 || st.UIDNext == 0 {
			return nil, errx.ErrMailServerUnreachable
		}
		box.UIDValidity = st.UIDValidity
		box.UIDNext = uint32(st.UIDNext)
		box.HighestModSeq = st.HighestModSeq
		if st.NumMessages != nil {
			box.Messages = *st.NumMessages
		}
		resp = append(resp, box)
	}

	return resp, nil
}

// listOptionsFor is the LIST options a server's capabilities allow, and
// whether STATUS was folded into the same round trip.
//
// Every RETURN option is LIST-EXTENDED grammar (RFC 5258), which IMAP4rev2
// implies and a plain RFC 3501 server does not. SPECIAL-USE does not bring it:
// a server may advertise the folder attributes without the extended LIST that
// requests them, and sending RETURN to one of those is a BAD that costs the
// account its entire folder list, the same shape as the MODSEQ bug in #405.
func listOptionsFor(caps imap.CapSet, status *imap.StatusOptions) (*imap.ListOptions, bool) {
	opts := &imap.ListOptions{}
	if !caps.Has(imap.CapListExtended) {
		return opts, false
	}
	// LIST-STATUS folds the STATUS of every folder into the one round trip;
	// without it each kept folder is asked separately.
	listStatus := caps.Has(imap.CapListStatus)
	if listStatus {
		opts.ReturnStatus = status
	}
	// Gmail attaches \Sent, \Trash, \Junk, \All ... only when asked; on a
	// plain LIST every folder is just \HasNoChildren and the canonical-folder
	// mapping is left guessing from names ("Bin" filed as inbox).
	if caps.Has(imap.CapSpecialUse) {
		opts.ReturnSpecialUse = true
	}
	return opts, listStatus
}

// dedupeByName keeps one folder per name.
//
// A folder is identified by its name, which is the one thing IMAP does
// guarantee is unique per account, and that is also the primary key of the
// stored folder row. Two rows under one name would advance each other's
// cursor and delete each other's row, which loses mail.
//
// This used to key on UIDVALIDITY, which cost a folder its entire sync
// whenever a server derived that number from a creation time and handed the
// same one to every folder made in the same second. Keyed by name it is a
// guard against a pathological listing rather than an everyday loss, so it
// should stay at zero; it is reported all the same, because a folder silently
// not syncing is the failure that took a release to notice. Input is already
// ranked, so the inbox and the special folders win any collision.
func dedupeByName(boxes []models.Mailbox) ([]models.Mailbox, int) {
	seen := make(map[string]struct{}, len(boxes))
	kept := boxes[:0]
	conflicts := 0
	for _, box := range boxes {
		if _, dup := seen[box.Name]; dup {
			log.Warn().
				Str("folder", box.Name).
				Msg("imap: the server listed one folder name twice; the second is not synced")
			conflicts++
			continue
		}
		seen[box.Name] = struct{}{}
		kept = append(kept, box)
	}
	return kept, conflicts
}

// FolderOverflow is how many selectable folders the last Folders call left
// out because the account has more than config.MaxEmailFolders.
func (c *Client) FolderOverflow() int {
	return int(c.folderOverflow.Load())
}

// FolderConflicts is how many folders the last Folders call left out because
// the server listed their name more than once.
func (c *Client) FolderConflicts() int {
	return int(c.folderConflicts.Load())
}

// rankFolders orders a listing INBOX first, then the special folders (sent,
// drafts, junk, trash, archive), then the rest in the server's order, and
// cuts it at limit. Ties keep the server's order, so the result is stable
// from one pass to the next.
//
// The ranking applies whether or not anything is cut, because the sync pass
// walks folders in this order and can run out of budget partway: a reply to
// the customer's own outreach should land before a mailing list in a user
// folder does, whatever order the server happened to list them in.
func rankFolders(all []models.Mailbox, limit int) ([]models.Mailbox, int) {
	rank := func(box *models.Mailbox) int {
		switch {
		case strings.EqualFold(box.Name, "INBOX"):
			return 0
		case CanonicalFolder(*box) != models.FolderInbox:
			return 1
		}
		return 2
	}
	sort.SliceStable(all, func(i, j int) bool { return rank(&all[i]) < rank(&all[j]) })
	if len(all) <= limit {
		return all, 0
	}
	return all[:limit], len(all) - limit
}

// selectableFolder is false for the containers a server lists only to show
// hierarchy (\Noselect) and the placeholders of LIST-EXTENDED (\NonExistent).
func selectableFolder(attrs []string) bool {
	for _, a := range attrs {
		switch strings.ToLower(a) {
		case "\\noselect", "\\nonexistent":
			return false
		}
	}
	return true
}

// IsVirtualFolder is a Gmail label view (All Mail, Starred, Important):
// every message in it also lives in a real folder under a different UID, so
// syncing it would re-file known mail (All Mail reads as archive) and swap
// the (mailbox, uid) pair the warmup actions address. A message archived out
// of every real folder stays unsynced, which is the ceiling of
// Gmail-over-IMAP; the OAuth Gmail path has no such gap.
func IsVirtualFolder(box models.Mailbox) bool {
	for _, a := range box.Attrs {
		switch strings.ToLower(a) {
		case "\\all", "\\flagged", "\\important":
			return true
		}
	}
	// Name fallback only inside Gmail's own namespace: a plain IMAP server
	// can legitimately have a user folder called "Important" or "Starred".
	// Gmail's delimiter is always "/", so this does not need the server's.
	lower := strings.ToLower(box.Name)
	if !strings.HasPrefix(lower, "[gmail]/") && !strings.HasPrefix(lower, "[google mail]/") {
		return false
	}
	switch lower[strings.Index(lower, "/")+1:] {
	case "all mail", "starred", "important":
		return true
	}
	return false
}

// BackfillEligible excludes folders whose history is not worth importing:
// trash, spam and Gmail's virtual views. Live sync still follows trash and
// spam for placement signals and to file new mail into those scopes; only the
// bounded initial import skips them, because their history would consume the
// message budget that belongs to real conversations. Drafts IS imported: it is
// small and a Drafts scope with none of the mailbox's existing drafts in it
// reads as broken.
func BackfillEligible(box models.Mailbox) bool {
	if IsVirtualFolder(box) || !selectableFolder(box.Attrs) {
		return false
	}
	switch CanonicalFolder(box) {
	case models.FolderTrash, models.FolderSpam:
		return false
	}
	return true
}

// SkipsFolder reports whether box is one the mailbox owner asked the sync to
// leave alone. A name in skip matches the folder listed under it and every
// folder below it, compared the way servers compare names: case does not
// count. INBOX and the special folders (by attribute or by name) never match,
// whatever the list says, because a sync without them is a broken mailbox
// rather than a quieter one.
func SkipsFolder(box models.Mailbox, skip []string) bool {
	if len(skip) == 0 || !skippableFolder(box) {
		return false
	}
	for _, s := range skip {
		s = strings.TrimSpace(s)
		if s == "" || strings.EqualFold(s, "INBOX") {
			continue
		}
		if strings.EqualFold(box.Name, s) {
			return true
		}
		if box.Delim != "" && hasPrefixFold(box.Name, s+box.Delim) {
			return true
		}
	}
	return false
}

// skippableFolder is false for INBOX and for any folder the mailbox needs
// whole: a special-use attribute or a recognised special name.
func skippableFolder(box models.Mailbox) bool {
	if strings.EqualFold(strings.TrimSpace(box.Name), "INBOX") {
		return false
	}
	for _, a := range box.Attrs {
		switch strings.ToLower(a) {
		case "\\inbox", "\\sent", "\\drafts", "\\junk", "\\trash", "\\archive", "\\all", "\\flagged", "\\important":
			return false
		}
	}
	return CanonicalFolder(box) == models.FolderInbox
}

// NormalizeSkipFolders is the write-side check on a skip list: trimmed,
// deduplicated without regard to case, bounded in count and length, no
// control characters, and none of the names the sync must keep. The result
// is what gets stored; the error names the first entry refused.
func NormalizeSkipFolders(names []string) ([]string, *errx.Error) {
	out := make([]string, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, raw := range names {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		if utf8.RuneCountInString(name) > config.SyncSkipFolderNameMax {
			return nil, skipFolderError(name, "is longer than the folder name limit")
		}
		for _, r := range name {
			if r < 0x20 || r == 0x7f {
				return nil, skipFolderError(name, "contains a control character")
			}
		}
		if !skippableFolder(models.Mailbox{Name: name}) {
			return nil, skipFolderError(name, "is a folder the sync always follows")
		}
		key := strings.ToLower(name)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, name)
	}
	if len(out) > config.SyncSkipFoldersMax {
		return nil, errx.NewWithIdentifier(errx.BadRequest, "invalid_sync_folder", fmt.Sprintf("at most %d folders can be skipped", config.SyncSkipFoldersMax))
	}
	return out, nil
}

func skipFolderError(name, why string) *errx.Error {
	return errx.NewWithIdentifier(errx.BadRequest, "invalid_sync_folder", fmt.Sprintf("folder %q %s", name, why))
}

// CanonicalFolder maps an IMAP folder to the canonical unibox folder.
// Special-use attributes are authoritative, with a name fallback for servers
// that do not advertise them; unrecognized user folders file as inbox so
// their mail stays visible.
func CanonicalFolder(box models.Mailbox) string {
	for _, a := range box.Attrs {
		switch strings.ToLower(a) {
		case "\\sent":
			return models.FolderSent
		case "\\drafts":
			return models.FolderDrafts
		case "\\junk":
			return models.FolderSpam
		case "\\trash":
			return models.FolderTrash
		case "\\archive", "\\all":
			return models.FolderArchive
		}
	}
	// The server reports its own hierarchy delimiter per folder, so a folder
	// whose name contains a dot on a "/" server is not cut in the middle.
	leafName := strings.ToLower(leafWithDelim(box.Name, box.Delim))
	switch {
	case matchesFolderName(leafName, ImapSent):
		return models.FolderSent
	case matchesFolderName(leafName, ImapDrafts):
		return models.FolderDrafts
	case matchesFolderName(leafName, ImapSpam):
		return models.FolderSpam
	case matchesFolderName(leafName, ImapTrash):
		return models.FolderTrash
	case matchesFolderName(leafName, ImapArchive):
		return models.FolderArchive
	}
	return models.FolderInbox
}

// delimString renders the delimiter LIST reported. go-imap carries it as a
// rune and a server that has no hierarchy reports NIL, which arrives as 0;
// converting that directly would produce a NUL byte and make every name look
// like it has no separator.
func delimString(delim rune) string {
	if delim == 0 {
		return ""
	}
	return string(delim)
}

// matchesFolderName compares an already-lowercased leaf against one of the
// role lists. Exact match only: "spam reports" is a user folder, not spam.
func matchesFolderName(leafName string, names []string) bool {
	for _, n := range names {
		if leafName == n {
			return true
		}
	}
	return false
}
