package goog

import (
	"context"
	"errors"
	"time"

	"github.com/warmbly/warmbly/internal/errx"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/googleapi"
)

// Watch asks Gmail to publish this mailbox's changes to a Pub/Sub topic and
// returns when the watch lapses. Calling it again renews the same watch.
func (c *Client) Watch(ctx context.Context, topic string) (time.Time, error) {
	// No label filter: the sync follows every label, so every change is worth a wake.
	resp, err := c.srv.Users.Watch("me", &gmail.WatchRequest{TopicName: topic}).Context(ctx).Do()
	if err != nil {
		// Keep Google's reason (an unknown topic, a missing publisher grant): it names the fix.
		return time.Time{}, errx.ProviderCall("gmail users.watch", googleReason(err), HandleError(err))
	}
	if resp.Expiration <= 0 {
		return time.Time{}, errors.New("goog: users.watch returned no expiration")
	}
	return time.UnixMilli(resp.Expiration), nil
}

func googleReason(err error) string {
	var gerr *googleapi.Error
	if errors.As(err, &gerr) {
		return gerr.Message
	}
	return ""
}

// StopWatch ends the mailbox's watch.
func (c *Client) StopWatch(ctx context.Context) error {
	if err := c.srv.Users.Stop("me").Context(ctx).Do(); err != nil {
		return errx.ProviderCall("gmail users.stop", googleReason(err), HandleError(err))
	}
	return nil
}
