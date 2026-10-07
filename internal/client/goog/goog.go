package goog

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/pkg/stoken"
	"golang.org/x/oauth2"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

type Client struct {
	Email     string
	FirstName string
	LastName  string

	srv       *gmail.Service
	rawClient *http.Client
	Cache     *cache.Cache

	// sentLabelStuck records that this mailbox's Gmail refused to remove the
	// SENT label, so FileWarmup stops asking. Gmail documents INBOX as
	// removable and says nothing either way about SENT; the answer is learned
	// from the API rather than assumed, once per mailbox.
	sentLabelStuck atomic.Bool

	// labelIDs caches label-name → id for this mailbox.
	labelMu  sync.Mutex
	labelIDs map[string]string

	// OnMessageAdded is offered every message the history feed reports as
	// added, with only its ids: the caller decides whether to hydrate it. It
	// returns false to leave the message on the server for a later pass
	// (fair use), which pins the history checkpoint before that record.
	OnMessageAdded  func(ctx context.Context, id, threadID string) (added bool, err error)
	OnMessageRemove func(ctx context.Context, messageID string) error
	// OnLabelAdd and OnLabelRemove get the labels that changed and the
	// message's labels as the history record reports them, which may be nil.
	OnLabelAdd     func(ctx context.Context, messageID string, changed, current []string) error
	OnLabelRemove  func(ctx context.Context, messageID string, changed, current []string) error
	OnTokenRefresh func(ctx context.Context, token *oauth2.Token) error
}

func (c *Client) Init(ctx context.Context, token *oauth2.Token, cfg oauth2.Config) *errx.MailError {
	ts := cfg.TokenSource(ctx, token)
	ts = oauth2.ReuseTokenSource(token, ts)
	// Only wrap when there is somewhere to persist to. stoken calls onUpdate on
	// every Token(), which the oauth2 transport does per request, so wrapping
	// with a nil OnTokenRefresh panics inside RoundTrip on the first API call.
	if c.OnTokenRefresh != nil {
		ts = stoken.New(ts, func(token *oauth2.Token) error {
			return c.OnTokenRefresh(context.Background(), token)
		})
	}
	return c.InitWithSource(ctx, ts)
}

// InitWithSource builds the client on a caller-owned token source (brokered
// tokens from Warmbly Cloud); nothing is persisted from it.
func (c *Client) InitWithSource(ctx context.Context, ts oauth2.TokenSource) *errx.MailError {
	httpClient := oauth2.NewClient(ctx, ts)
	c.rawClient = httpClient
	var err error
	c.srv, err = gmail.NewService(ctx, option.WithHTTPClient(httpClient))
	if err != nil {
		return HandleError(err)
	}

	return nil
}
