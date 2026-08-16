package wmail

import (
	"testing"

	"github.com/warmbly/warmbly/internal/models"
)

func TestParentReference(t *testing.T) {
	tests := []struct {
		name          string
		req           *SendRequest
		wantNil       bool
		wantMessageID string
		wantThreadID  string
	}{
		{
			// The dashboard reply case. The composer knows the thread and
			// nothing else, and requiring InReplyTo threw the thread away.
			name:         "thread id only",
			req:          &SendRequest{Parent: &models.EmailParent{ThreadID: "t-1"}},
			wantThreadID: "t-1",
		},
		{
			// The warmup reply case: an RFC Message-ID from the token flow and
			// no provider thread record.
			name:          "in-reply-to only",
			req:           &SendRequest{InReplyTo: "<parent@example.com>"},
			wantMessageID: "parent@example.com",
		},
		{
			name: "both, parent wins for message id",
			req: &SendRequest{
				InReplyTo: "<header@example.com>",
				Parent:    &models.EmailParent{MessageID: "parent@example.com", ThreadID: "t-1"},
			},
			wantMessageID: "parent@example.com",
			wantThreadID:  "t-1",
		},
		{
			// A parent carrying only a thread still picks the header up, so
			// both the sender's and the recipient's clients can thread it.
			name: "parent thread plus header message id",
			req: &SendRequest{
				InReplyTo: "<header@example.com>",
				Parent:    &models.EmailParent{ThreadID: "t-1"},
			},
			wantMessageID: "header@example.com",
			wantThreadID:  "t-1",
		},
		{
			name:    "nothing to thread onto",
			req:     &SendRequest{},
			wantNil: true,
		},
		{
			name:    "empty parent is not a parent",
			req:     &SendRequest{Parent: &models.EmailParent{}},
			wantNil: true,
		},
		{
			name:    "nil request",
			req:     nil,
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parentReference(tt.req)
			if tt.wantNil {
				if got != nil {
					t.Fatalf("got %+v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatal("got nil, want a parent reference")
			}
			if got.MessageID != tt.wantMessageID {
				t.Errorf("MessageID = %q, want %q", got.MessageID, tt.wantMessageID)
			}
			if got.ThreadID != tt.wantThreadID {
				t.Errorf("ThreadID = %q, want %q", got.ThreadID, tt.wantThreadID)
			}
		})
	}
}
