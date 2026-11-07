package goog

import (
	"context"
	"errors"
	"fmt"
	"time"

	"google.golang.org/api/gmail/v1"
)

// Watch asks Gmail to publish this mailbox's changes to a Pub/Sub topic and
// returns when the watch lapses. Calling it again renews the same watch.
func (c *Client) Watch(ctx context.Context, topic string) (time.Time, error) {
	// No label filter: the sync follows every label, so every change is worth a wake.
	resp, err := c.srv.Users.Watch("me", &gmail.WatchRequest{TopicName: topic}).Context(ctx).Do()
	if err != nil {
		return time.Time{}, fmt.Errorf("gmail users.watch: %w", HandleError(err))
	}
	if resp.Expiration <= 0 {
		return time.Time{}, errors.New("goog: users.watch returned no expiration")
	}
	return time.UnixMilli(resp.Expiration), nil
}

// StopWatch ends the mailbox's watch.
func (c *Client) StopWatch(ctx context.Context) error {
	if err := c.srv.Users.Stop("me").Context(ctx).Do(); err != nil {
		return fmt.Errorf("gmail users.stop: %w", HandleError(err))
	}
	return nil
}
