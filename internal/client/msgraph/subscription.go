package msgraph

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/warmbly/warmbly/internal/errx"
)

// SubscriptionLifetime stays under Graph's 10,080-minute ceiling for message subscriptions.
const SubscriptionLifetime = 7*24*time.Hour - 2*time.Hour

// Subscription is a change-notification subscription on the mailbox's messages.
type Subscription struct {
	ID              string    `json:"id"`
	Resource        string    `json:"resource"`
	NotificationURL string    `json:"notificationUrl"`
	ExpiresAt       time.Time `json:"expirationDateTime"`
}

// subscriptionResource is every message in the mailbox, whichever folder it is in.
func (c *Client) subscriptionResource() string {
	if c.User == "" {
		return "me/messages"
	}
	return "users/" + url.PathEscape(c.User) + "/messages"
}

// CreateSubscription opens a subscription that posts creates, updates and deletes to
// notificationURL, with lifecycle events to the same URL.
func (c *Client) CreateSubscription(ctx context.Context, notificationURL, clientState string, now time.Time) (*Subscription, error) {
	in := map[string]any{
		"changeType":                "created,updated,deleted",
		"notificationUrl":           notificationURL,
		"lifecycleNotificationUrl":  notificationURL,
		"resource":                  c.subscriptionResource(),
		"expirationDateTime":        now.Add(SubscriptionLifetime).UTC().Format(time.RFC3339),
		"clientState":               clientState,
		"latestSupportedTlsVersion": "v1_2",
	}
	var out Subscription
	if err := c.doJSON(ctx, http.MethodPost, graphBase+"/subscriptions", in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RenewSubscription extends a subscription, which also answers a reauthorization request.
func (c *Client) RenewSubscription(ctx context.Context, id string, now time.Time) (*Subscription, error) {
	in := map[string]any{"expirationDateTime": now.Add(SubscriptionLifetime).UTC().Format(time.RFC3339)}
	var out Subscription
	if err := c.doJSON(ctx, http.MethodPatch, graphBase+"/subscriptions/"+url.PathEscape(id), in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteSubscription removes a subscription; one already gone is not an error.
func (c *Client) DeleteSubscription(ctx context.Context, id string) error {
	err := c.doJSON(ctx, http.MethodDelete, graphBase+"/subscriptions/"+url.PathEscape(id), nil, nil)
	if SubscriptionGone(err) {
		return nil
	}
	return err
}

// subscriptionListMaxPages bounds how many continuations ListSubscriptions follows.
const subscriptionListMaxPages = 20

// ListSubscriptions returns the subscriptions this app holds for the signed-in mailbox.
func (c *Client) ListSubscriptions(ctx context.Context) ([]Subscription, error) {
	var subs []Subscription
	next := graphBase + "/subscriptions"
	for range subscriptionListMaxPages {
		var page struct {
			Value    []Subscription  `json:"value"`
			NextLink json.RawMessage `json:"@odata.nextLink"`
		}
		if err := c.doJSON(ctx, http.MethodGet, next, nil, &page); err != nil {
			return nil, err
		}
		subs = append(subs, page.Value...)
		if page.NextLink == nil {
			return subs, nil
		}
		link, ok := graphContinuation(page.NextLink)
		if !ok {
			return nil, errSubscriptionListIncomplete
		}
		next = link
	}
	return nil, errSubscriptionListIncomplete
}

var errSubscriptionListIncomplete = errors.New("msgraph: subscription list continuation was not followed to its end")

// SubscriptionGone reports a subscription Graph no longer has.
func SubscriptionGone(err error) bool {
	return subscriptionStatus(err) == http.StatusNotFound
}

// SubscriptionConflict reports a create refused because an equal subscription already exists.
func SubscriptionConflict(err error) bool {
	return subscriptionStatus(err) == http.StatusConflict
}

func subscriptionStatus(err error) int {
	var mailErr *errx.MailError
	if !errors.As(err, &mailErr) || mailErr.Failure == nil {
		return 0
	}
	return mailErr.Failure.Status
}
