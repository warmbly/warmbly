package wmail

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"golang.org/x/oauth2"
)

func TestMailboxProviderDeadlineReleasesPassAndAllowsRetry(t *testing.T) {
	for _, provider := range []models.InboxProvider{models.InboxProviderGoogle, models.InboxProviderOutlook} {
		for _, body := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/body=%v", provider, body), func(t *testing.T) {
				var stalled atomic.Bool
				stalled.Store(true)
				release := make(chan struct{})
				srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
					rw.Header().Set("Content-Type", "application/json")
					if stalled.Load() {
						if body {
							rw.WriteHeader(http.StatusOK)
							_, _ = rw.Write([]byte(`{"`))
							rw.(http.Flusher).Flush()
						}
						select {
						case <-release:
						case <-r.Context().Done():
						}
						return
					}
					if provider == models.InboxProviderGoogle {
						_, _ = rw.Write([]byte(`{"historyId":"91","history":[]}`))
					} else {
						_, _ = fmt.Fprintf(rw, `{"value":[],"@odata.deltaLink":%q}`, "https://graph.microsoft.com"+r.URL.Path+"?next=live")
					}
				}))
				defer srv.Close()
				defer close(release)
				var events []captured
				var w *WMail
				ctx := context.WithValue(context.Background(), oauth2.HTTPClient, &http.Client{Timeout: 40 * time.Millisecond, Transport: rewriteToTestServer(srv.URL)})
				token := &oauth2.Token{AccessToken: "test", Expiry: time.Now().Add(time.Hour)}
				if provider == models.InboxProviderGoogle {
					w = newGoogleTestMail(t, srv, &events)
					w.GoogleData.LastHistoryID = 91
					if err := w.GoogleData.Client.Init(ctx, token, oauth2.Config{}); err != nil {
						t.Fatal(err)
					}
					w.googleReconciledAt = time.Now()
				} else {
					var states []models.SyncState
					w = newGraphTestMail(t, srv, &events, &states)
					if err := w.GraphData.Client.Init(ctx, token, oauth2.Config{}); err != nil {
						t.Fatal(err)
					}
				}
				w.tracker.state.BackfillStatus = models.SyncBackfillComplete
				mailCtx, cancel := context.WithCancel(context.Background())
				defer cancel()
				w.Ctx, w.Cancel = mailCtx, cancel
				result := make(chan *errx.MailError, 1)
				go func() { result <- w.SyncMail(context.Background()) }()
				select {
				case err := <-result:
					if err == nil {
						t.Fatal("stalled provider pass reported success")
					}
				case <-time.After(time.Second):
					stalled.Store(false)
					srv.CloseClientConnections()
					<-result
					t.Fatal("provider request deadline did not release sync pass")
				}
				if ctx.Err() != nil || w.Ctx.Err() != nil {
					t.Fatal("request timeout cancelled mailbox lifetime")
				}
				stalled.Store(false)
				if err := w.SyncMail(context.Background()); err != nil {
					t.Fatalf("later retry without reconnect failed: %v", err)
				}
			})
		}
	}
}
