package imap

import (
	"context"
	"fmt"
	"strings"

	"github.com/emersion/go-imap/v2"
)

// MoveUIDs moves messages out of one folder in one command, for the unibox's
// filing relay. It reports each moved UID's new one and the destination's
// UIDVALIDITY when the server says (UIDPLUS COPYUID); an empty map means it
// did not, and the caller finds the messages by Message-ID instead.
//
// Without MOVE, only a UID-scoped expunge is safe to finish the COPY with, so
// a server offering neither refuses with ErrNoSingleMessageDelete.
func (c *Client) MoveUIDs(ctx context.Context, src, dst string, uids []uint32) (map[uint32]uint32, uint32, error) {
	moved := make(map[uint32]uint32, len(uids))
	if len(uids) == 0 {
		return moved, 0, nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if merr := c.ensureConnected(); merr != nil {
		return nil, 0, merr
	}
	c.lifecycle.RLock()
	defer c.lifecycle.RUnlock()
	defer c.begin()()

	src, dst = c.qualifyMailboxLocked(src), c.qualifyMailboxLocked(dst)
	if strings.EqualFold(src, dst) {
		return moved, 0, nil
	}
	caps := c.caps()
	if !caps.Has(imap.CapMove) && !caps.Has(imap.CapUIDPlus) {
		return nil, 0, ErrNoSingleMessageDelete
	}
	if err := c.ensureMailboxExists(dst); err != nil {
		return nil, 0, err
	}
	if _, err := c.selectMailbox(src, nil); err != nil {
		return nil, 0, fmt.Errorf("select %q: %w", src, err)
	}

	set := imap.UIDSet{}
	for _, uid := range uids {
		set.AddNum(imap.UID(uid))
	}
	data, err := c.client.Move(set, dst).Wait()
	if err != nil {
		return nil, 0, fmt.Errorf("move %d uids %q→%q: %w", len(uids), src, dst, err)
	}
	if data == nil {
		return moved, 0, nil
	}
	pairCopyUIDs(moved, data.SourceUIDs, data.DestUIDs)
	return moved, data.UIDValidity, nil
}

// pairCopyUIDs reads a COPYUID response: both sets list the same messages in
// the same order, so the n-th source UID became the n-th destination UID.
func pairCopyUIDs(into map[uint32]uint32, source, dest imap.NumSet) {
	srcSet, ok := source.(imap.UIDSet)
	if !ok {
		return
	}
	dstSet, ok := dest.(imap.UIDSet)
	if !ok {
		return
	}
	from, ok := srcSet.Nums()
	if !ok {
		return
	}
	to, ok := dstSet.Nums()
	if !ok || len(from) != len(to) {
		return
	}
	for i := range from {
		into[uint32(from[i])] = uint32(to[i])
	}
}
