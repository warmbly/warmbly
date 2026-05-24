package goog

import (
	"context"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/stoken"
	"golang.org/x/oauth2"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

type Client struct {
	Email     string
	FirstName string
	LastName  string

	srv   *gmail.Service
	Cache *cache.Cache

	OnMessageAdd    func(ctx context.Context, msg *models.EmailMessageData) error
	OnMessageRemove func(ctx context.Context, messageID string) error
	OnLabelAdd      func(ctx context.Context, messageID string, labelIds []string) error
	OnLabelRemove   func(ctx context.Context, messageID string, labelIds []string) error
	OnTokenRefresh  func(ctx context.Context, token *oauth2.Token) error
}

func (c *Client) Init(ctx context.Context, token *oauth2.Token, cfg oauth2.Config) *errx.MailError {
	ts := cfg.TokenSource(ctx, token)
	ts = oauth2.ReuseTokenSource(token, ts)
	ts = stoken.New(ts, func(token *oauth2.Token) error {
		return c.OnTokenRefresh(context.Background(), token)
	})

	httpClient := oauth2.NewClient(ctx, ts)
	var err error
	c.srv, err = gmail.NewService(ctx, option.WithHTTPClient(httpClient))
	if err != nil {
		return HandleError(err)
	}

	return nil
}
