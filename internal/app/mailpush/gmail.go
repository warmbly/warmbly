package mailpush

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"cloud.google.com/go/pubsub"
	"google.golang.org/api/option"
)

// ParseGmailSubscription splits "projects/<project>/subscriptions/<id>".
func ParseGmailSubscription(name string) (project, id string, err error) {
	parts := strings.Split(strings.TrimSpace(name), "/")
	if len(parts) != 4 || parts[0] != "projects" || parts[2] != "subscriptions" || parts[1] == "" || parts[3] == "" {
		return "", "", fmt.Errorf("GMAIL_PUSH_SUBSCRIPTION must be projects/<project>/subscriptions/<id>, got %q", name)
	}
	return parts[1], parts[3], nil
}

// GmailAddress reads the mailbox address from a Gmail watch notification.
func GmailAddress(data []byte) (string, bool) {
	var n struct {
		EmailAddress string `json:"emailAddress"`
	}
	if json.Unmarshal(data, &n) != nil {
		return "", false
	}
	n.EmailAddress = strings.TrimSpace(n.EmailAddress)
	return n.EmailAddress, n.EmailAddress != ""
}

// RunGmailSubscriber pulls Gmail watch notifications and wakes each mailbox
// until ctx ends. A pull needs no public endpoint. serviceAccountJSON may be
// empty to use Application Default Credentials.
func RunGmailSubscriber(ctx context.Context, subscription, serviceAccountJSON string, waker *Waker) error {
	project, id, err := ParseGmailSubscription(subscription)
	if err != nil {
		return err
	}
	var opts []option.ClientOption
	if serviceAccountJSON != "" {
		opts = append(opts, option.WithAuthCredentialsJSON(option.ServiceAccount, []byte(serviceAccountJSON)))
	}
	client, err := pubsub.NewClient(ctx, project, opts...)
	if err != nil {
		return fmt.Errorf("gmail push: pubsub client: %w", err)
	}
	defer client.Close()
	sub := client.Subscription(id)
	sub.ReceiveSettings.MaxOutstandingMessages = 1000
	return sub.Receive(ctx, func(ctx context.Context, m *pubsub.Message) {
		address, ok := GmailAddress(m.Data)
		if !ok {
			m.Ack()
			return
		}
		wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if err := waker.WakeGmail(wctx, address); err != nil {
			m.Nack()
			return
		}
		m.Ack()
	})
}
