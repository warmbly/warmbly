package jobs

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/advanced"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// fakeInboundAdvanced lets a handler test drive the classification the advanced
// service makes: a technical failure (non-nil errx) versus a genuine no-match
// (nil). Only the two inbound methods are overridden; the rest of the interface
// is never called on this path.
type fakeInboundAdvanced struct {
	advanced.Service
	bounceErr    *errx.Error
	complaintErr *errx.Error
}

func (f *fakeInboundAdvanced) RecordInboundBounce(context.Context, uuid.UUID, string, string, string) *errx.Error {
	return f.bounceErr
}

func (f *fakeInboundAdvanced) RecordInboundComplaint(context.Context, uuid.UUID, string, string, string) *errx.Error {
	return f.complaintErr
}

// A technical failure must be returned so the bus redelivers the event instead
// of acking a dropped bounce.
func TestHandleInboundBounceReturnsTechnicalFailure(t *testing.T) {
	s := &JobsService{AdvancedService: &fakeInboundAdvanced{bounceErr: errx.InternalError()}}
	if err := s.HandleInboundBounce(context.Background(), &models.JobEventInboundBounce{EmailID: uuid.New()}); err == nil {
		t.Fatal("a technical failure must be returned for redelivery, not swallowed")
	}
}

// A genuine no-match reads back nil and the handler acks, so the bus does not
// loop on a bounce that can never be attributed.
func TestHandleInboundBounceAcksGenuineNoMatch(t *testing.T) {
	s := &JobsService{AdvancedService: &fakeInboundAdvanced{}}
	if err := s.HandleInboundBounce(context.Background(), &models.JobEventInboundBounce{EmailID: uuid.New()}); err != nil {
		t.Fatalf("a genuine no-match must ack with nil, got %v", err)
	}
}

func TestHandleInboundComplaintReturnsTechnicalFailure(t *testing.T) {
	s := &JobsService{AdvancedService: &fakeInboundAdvanced{complaintErr: errx.InternalError()}}
	if err := s.HandleInboundComplaint(context.Background(), &models.JobEventInboundComplaint{EmailID: uuid.New()}); err == nil {
		t.Fatal("a technical failure must be returned for redelivery, not swallowed")
	}
}

func TestHandleInboundComplaintAcksGenuineNoMatch(t *testing.T) {
	s := &JobsService{AdvancedService: &fakeInboundAdvanced{}}
	if err := s.HandleInboundComplaint(context.Background(), &models.JobEventInboundComplaint{EmailID: uuid.New()}); err != nil {
		t.Fatalf("a genuine no-match must ack with nil, got %v", err)
	}
}

type fakeWarmupBounces struct {
	repository.WarmupRepository
	calls []string
}

func (f *fakeWarmupBounces) RecordWarmupBounce(_ context.Context, _ uuid.UUID, id, rcpt string) (bool, error) {
	f.calls = append(f.calls, id+" "+rcpt)
	return true, nil
}

// Only a DSN saying the address does not exist is held against a pool partner.
func TestHandleInboundBounceRecordsOnlyUnknownAddressWarmupBounces(t *testing.T) {
	for _, tc := range []struct {
		reason string
		want   bool
	}{
		{"5.1.1 smtp; 550 5.1.1 <a@b.test>: Recipient address rejected: User unknown in virtual mailbox table", true},
		{"550 5.1.10 RESOLVER.ADR.RecipientNotFound; Recipient not found by SMTP address lookup", true},
		{"552 5.2.2 Mailbox full", false},
		{"550 5.7.1 Message rejected due to sender reputation", false},
		{"Undelivered Mail Returned to Sender", false},
	} {
		repo := &fakeWarmupBounces{}
		s := &JobsService{WarmupRepo: repo, AdvancedService: &fakeInboundAdvanced{}}
		err := s.HandleInboundBounce(context.Background(), &models.JobEventInboundBounce{
			EmailID: uuid.New(), OriginalMessageID: "<m1@x.test>", FailedRecipient: "Box <a@b.test>", Reason: tc.reason,
		})
		if err != nil {
			t.Fatal(err)
		}
		if got := len(repo.calls) == 1; got != tc.want {
			t.Fatalf("%q recorded = %v, want %v", tc.reason, got, tc.want)
		}
		if tc.want && repo.calls[0] != "m1@x.test a@b.test" {
			t.Fatalf("recorded %q, want bare message id and recipient", repo.calls[0])
		}
	}
}
