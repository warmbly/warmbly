package goog

import (
	"errors"
	"fmt"
	"testing"

	"google.golang.org/api/googleapi"
)

// IsThreadRefusal decides whether a failed send is worth retrying without the
// threadId. It must be narrow: a wrong yes re-sends a message for a reason
// that will refuse it again, and a wrong no turns a campaign follow-up into a
// failed step because Gmail did not like a thread handle.
func TestIsThreadRefusal(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"nothing went wrong", nil, false},
		{"a plain error", errors.New("thread"), false},
		{
			"a 400 naming the thread",
			&googleapi.Error{Code: 400, Message: "Invalid thread_id value."},
			true,
		},
		{
			// Gmail's real wording for a thread that is gone names nothing,
			// which is the case a match on the word "thread" would miss.
			"a 404 naming nothing",
			&googleapi.Error{Code: 404, Message: "Requested entity was not found."},
			true,
		},
		{
			"a wrapped 400 naming the thread",
			fmt.Errorf("send message failed: %w", &googleapi.Error{Code: 400, Message: "Invalid threadId"}),
			true,
		},
		{
			// Re-sending this without the handle would fail the same way, so
			// it must not be mistaken for a thread the send can do without.
			"a 400 about the message",
			&googleapi.Error{Code: 400, Message: "Invalid To header"},
			false,
		},
		{
			"an auth failure",
			&googleapi.Error{Code: 401, Message: "Invalid Credentials for thread"},
			false,
		},
		{
			"a rate limit",
			&googleapi.Error{Code: 429, Message: "User-rate limit exceeded"},
			false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsThreadRefusal(tc.err); got != tc.want {
				t.Errorf("IsThreadRefusal(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
