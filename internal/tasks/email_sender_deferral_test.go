package tasks

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type deferredWarmupAdmission struct {
	repository.OutboundAdmissionRepository
	repository.WarmupDispatchRepository
	err      error
	deferred bool
}

func (a *deferredWarmupAdmission) ReserveOutbound(context.Context, repository.OutboundReservation) (uuid.UUID, error) {
	return uuid.Nil, repository.ErrSendAdmissionDenied
}

func (a *deferredWarmupAdmission) DeferWarmupDispatch(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID, time.Time) error {
	a.deferred = true
	return a.err
}

func TestWarmupSenderDistinguishesRequeueFromUnknownDeferral(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want error
	}{
		{"confirmed deferral", nil, ErrWarmupDispatchDeferred},
		{"uncertain transaction", errors.New("commit response lost"), ErrSendDispatchUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			admission := &deferredWarmupAdmission{err: tc.err}
			worker, org := uuid.New(), uuid.New()
			sender := &emailSender{admission: admission}
			err := sender.Send(t.Context(), uuid.New(), EmailMessage{
				IsWarmup: true, DispatchNonce: uuid.NewString(), To: []string{"partner@example.test"},
			}, models.Email{ID: uuid.New(), Email: "sender@example.test", WorkerID: &worker, OrganizationID: &org})
			if !admission.deferred || !errors.Is(err, tc.want) {
				t.Fatalf("deferred=%v error=%v want=%v", admission.deferred, err, tc.want)
			}
		})
	}
}
