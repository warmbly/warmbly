package imap

import (
	"context"
	"errors"
	"io"
	"time"

	goimap "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

func (c *Client) DiagnosticRawMessage(ctx context.Context, folder string, validity, uid uint32, limit int) ([]byte, error) {
	if folder == "" || validity == 0 || uid == 0 || limit <= 0 || limit > 1<<20 {
		return nil, errors.New("invalid diagnostic locator")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	for !c.mu.TryLock() {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
	defer c.mu.Unlock()
	for !c.lifecycle.TryRLock() {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
	defer c.lifecycle.RUnlock()
	client := c.client
	conn := c.conn
	if client == nil || conn == nil || (client.State() != goimap.ConnStateAuthenticated && client.State() != goimap.ConnStateSelected) {
		return nil, errors.New("diagnostic IMAP connection unavailable")
	}
	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { _ = conn.Close(); close(done) })
	defer func() {
		if !stop() {
			<-done
		}
	}()
	defer c.begin()()
	selected, err := c.selectMailbox(c.qualifyMailboxLocked(folder), nil)
	if err != nil || selected.UIDValidity != validity {
		return nil, errors.New("diagnostic folder generation changed")
	}
	section := &goimap.FetchItemBodySection{Peek: true, Partial: &goimap.SectionPartial{Size: int64(limit + 1)}}
	cmd := client.Fetch(goimap.UIDSetNum(goimap.UID(uid)), &goimap.FetchOptions{UID: true, BodySection: []*goimap.FetchItemBodySection{section}})
	defer cmd.Close()
	var raw []byte
	for msg := cmd.Next(); msg != nil; msg = cmd.Next() {
		for item := msg.Next(); item != nil; item = msg.Next() {
			if part, ok := item.(imapclient.FetchItemDataBodySection); ok && part.Literal != nil {
				raw, err = io.ReadAll(io.LimitReader(part.Literal, int64(limit+1)))
				if err != nil || len(raw) > limit {
					clear(raw)
					_ = conn.Close()
					return nil, errors.New("diagnostic raw message exceeds limit or is unavailable")
				}
			}
		}
	}
	if err = cmd.Close(); err != nil || ctx.Err() != nil || len(raw) == 0 {
		clear(raw)
		return nil, errors.New("diagnostic raw message unavailable")
	}
	return raw, nil
}
