package tasks

import (
	"context"
	"github.com/warmbly/warmbly/internal/pkg/generation"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/events"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type capturingPublisher struct {
	events.Publisher
	params *events.SendEmailParams
}

type payloadAdmission struct {
	repository.OutboundAdmissionRepository
}

func (payloadAdmission) ReserveOutbound(context.Context, repository.OutboundReservation) (uuid.UUID, error) {
	return uuid.New(), nil
}

func payloadSender(pub *capturingPublisher) EmailSender {
	sender := NewEmailSender(nil, pub)
	sender.(*emailSender).WireSendAdmission(payloadAdmission{})
	return sender
}

func (p *capturingPublisher) PublishSendEmail(_ context.Context, _ uuid.UUID, params *events.SendEmailParams) error {
	p.params = params
	return nil
}

func TestSendCarriesReplyToOnlyOutsideWarmup(t *testing.T) {
	worker, org := uuid.New(), uuid.New()
	account := models.Email{ID: uuid.New(), OrganizationID: &org, WorkerID: &worker, Email: "b@acme.com", ReplyTo: "a@acme.com"}
	for _, tt := range []struct {
		name   string
		warmup bool
		want   string
	}{
		{name: "campaign", want: "a@acme.com"},
		// Warmup verification reads replies back in the sending mailbox.
		{name: "warmup", warmup: true, want: ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pub := &capturingPublisher{}
			sender := payloadSender(pub)
			if err := sender.Send(context.Background(), uuid.New(), EmailMessage{To: []string{"x@y.test"}, IsWarmup: tt.warmup}, account); err != nil {
				t.Fatal(err)
			}
			if pub.params.ReplyTo != tt.want {
				t.Fatalf("ReplyTo = %q, want %q", pub.params.ReplyTo, tt.want)
			}
		})
	}
}

func TestWarmupSendPreservesConfiguredIdentity(t *testing.T) {
	worker, org := uuid.New(), uuid.New()
	account := models.Email{ID: uuid.New(), OrganizationID: &org, WorkerID: &worker, Email: "support@example.test", Name: "Árvíztűrő Support <EMEA> {Ops}!!!"}
	body, err := generation.RenderCanonicalTurn("This hypothetical example is closed.", account.Name)
	if err != nil {
		t.Fatal(err)
	}
	if err := lintDiagnosticContent("Diagnostic example", body, false, account); err != nil {
		t.Fatal(err)
	}
	pub := &capturingPublisher{}
	sender := payloadSender(pub)
	if err := sender.Send(t.Context(), uuid.New(), EmailMessage{IsWarmup: true, BodyPlain: body, To: []string{"recipient@example.test"}}, account); err != nil {
		t.Fatal(err)
	}
	if pub.params.FromName != account.Name || pub.params.BodyPlain != body {
		t.Fatal("publisher renamed trusted sender")
	}
	for _, name := range []string{"Bad\rName", "Bad\nName", "Bad\x00Name"} {
		account.Name = name
		if err := sender.Send(t.Context(), uuid.New(), EmailMessage{IsWarmup: true}, account); err == nil {
			t.Fatal("accepted header injection")
		}
	}
}
