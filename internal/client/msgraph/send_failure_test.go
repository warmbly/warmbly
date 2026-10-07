package msgraph

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/warmbly/warmbly/internal/errx"
)

func TestNativeSendFailureGraph(t *testing.T) {
	for _, tc := range []struct {
		status      int
		disposition string
	}{
		{401, errx.SendAuth}, {403, errx.SendAuth}, {404, errx.SendPermanent},
		{429, errx.SendThrottle}, {503, errx.SendRetry},
	} {
		at := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
		got := HandleError(&http.Response{StatusCode: tc.status, Header: http.Header{"Retry-After": {at.Format(http.TimeFormat)}}, Body: io.NopCloser(strings.NewReader(`{"error":{"code":"NativeCode","message":"private body"}}`))})
		f := got.Failure
		if f == nil || f.Status != tc.status || f.Cause != "NativeCode" || f.Provider != "microsoft" || f.Disposition != tc.disposition || f.RetryAt == nil || !f.RetryAt.Equal(at) {
			t.Fatalf("failure = %+v", f)
		}
	}
}

func TestSendAmbiguousGraphDoesNotFallbackOrDelete(t *testing.T) {
	for _, draft := range []bool{false, true} {
		rt := &sendRT{assignedID: "<assigned@test.local>", isDraft: "true"}
		if draft {
			rt.sendStatus = 503
		} else {
			rt.createStatus = 503
		}
		c := newSendClient(rt)
		_, err := c.SendMessage(context.Background(), "", []string{"receiver@test.local"}, nil, nil, "<minted@test.local>", "subject", "body", "", nil, nil)
		var mailErr *errx.MailError
		want := errx.SendRetry
		if draft {
			want = errx.SendAmbiguous
		}
		if !errors.As(err, &mailErr) || mailErr.Failure == nil || mailErr.Failure.Disposition != want {
			t.Fatalf("error = %v", err)
		}
		if rt.did(http.MethodPost, "/sendMail") || rt.did(http.MethodDelete, "/DRAFT_ID") {
			t.Fatal("ambiguous submission was retried or draft deleted")
		}
	}
}
