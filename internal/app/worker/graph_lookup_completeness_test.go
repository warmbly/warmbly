package worker

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/worker/wmail"
	"github.com/warmbly/warmbly/internal/client/msgraph"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"golang.org/x/oauth2"
)

func TestGraphWarmupDeleteIncompleteLookupRetainsBody(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"missing collection", `{}`},
		{"null collection", `{"value":null}`},
		{"continuation not exhausted", `{"value":[],"@odata.nextLink":"https://graph.microsoft.com/v1.0/me/messages?$skiptoken=more"}`},
		{"foreign continuation", `{"value":[],"@odata.nextLink":"https://other.example.test/collect"}`},
		{"blank identity", `{"value":[{"id":"  "}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lookups, deletes := 0, 0
			transport := filingTransport(func(r *http.Request) (*http.Response, error) {
				status, body := http.StatusOK, tc.body
				if r.Method == http.MethodGet {
					lookups++
				} else if r.Method == http.MethodDelete {
					deletes++
					status, body = http.StatusNotFound, `{"error":{"code":"ErrorItemNotFound"}}`
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}, nil
			})
			ctx := context.WithValue(t.Context(), oauth2.HTTPClient, &http.Client{Transport: transport})
			client := &msgraph.Client{}
			if err := client.Init(ctx, &oauth2.Token{AccessToken: "test", Expiry: time.Now().Add(time.Hour)}, oauth2.Config{}); err != nil {
				t.Fatal(err)
			}
			account := uuid.New()
			w, _ := filingWorker(account, &filingIMAP{})
			store := &warmupDeleteStore{}
			w.mailManager.Emails[account] = &wmail.WMail{ID: account, Storage: store, GraphData: &wmail.GraphData{Client: client}}
			action := models.WarmupEmailAction{EmailID: account, GmailID: "stale-id", InternalID: uuid.NewString(), RFCMessageID: "<warmup@example.test>", Actions: []string{models.WarmupActionDelete}}
			ctx = context.WithValue(ctx, deliveryKey{}, delivery{attempt: 1, redelivers: true})
			err := w.HandleWarmupAction(ctx, action)
			var merr *errx.MailError
			if !errors.As(err, &merr) || merr.Code != errx.MailErrorCodeServerUnreachable || merr.ResolveMethod != errx.MailErrorResolveMethodRetry || store.deletes != 0 || deletes != 0 || lookups != 1 {
				t.Fatalf("incomplete lookup acknowledged or deleted: lookup=%d provider_delete=%d body_delete=%d err=%v", lookups, deletes, store.deletes, err)
			}
		})
	}
}
