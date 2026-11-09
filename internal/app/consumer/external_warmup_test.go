package jobs

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type externalWarmupFake struct {
	rule    models.ExternalWarmupSettings
	err     error
	reads   int
	sweeps  int
	batches [][]models.JobEventNewEmail
}

func (f *externalWarmupFake) ExternalWarmupRule(context.Context, uuid.UUID) (models.ExternalWarmupSettings, error) {
	f.reads++
	return f.rule, f.err
}

func (f *externalWarmupFake) ExternalWarmupRules(context.Context) (map[uuid.UUID]models.ExternalWarmupSettings, error) {
	if !f.rule.Enabled() {
		return map[uuid.UUID]models.ExternalWarmupSettings{}, nil
	}
	return map[uuid.UUID]models.ExternalWarmupSettings{externalWarmupOrg: f.rule}, nil
}

func (f *externalWarmupFake) OrganizationMailboxIDs(context.Context, uuid.UUID) ([]uuid.UUID, error) {
	return []uuid.UUID{uuid.New()}, nil
}

func (f *externalWarmupFake) DeleteExternalWarmup(context.Context, uuid.UUID, uuid.UUID, models.ExternalWarmupSettings, int) ([]models.JobEventNewEmail, error) {
	f.sweeps++
	if len(f.batches) == 0 {
		return nil, nil
	}
	next := f.batches[0]
	f.batches = f.batches[1:]
	return next, nil
}

var externalWarmupOrg = uuid.New()

type orgEmailRepo struct{ repository.EmailRepository }

func (orgEmailRepo) GetByID(_ context.Context, id uuid.UUID) (*models.Email, *errx.Error) {
	return &models.Email{ID: id, OrganizationID: &externalWarmupOrg}, nil
}

func arrival(subject string) *models.JobEventNewEmail {
	return &models.JobEventNewEmail{UserID: uuid.New(), Message: &models.EmailMessageStoreData{
		ID: uuid.New(), EmailID: uuid.New(), Folder: models.FolderInbox, Subject: subject,
	}}
}

func TestExternalWarmupArrivalsAreNotStored(t *testing.T) {
	inbox := &diagnosticInboxRepo{}
	store := &externalWarmupFake{rule: models.ExternalWarmupSettings{Tags: []string{"wrm-7q2x"}}}
	s := &JobsService{UniboxRepository: inbox, EmailRepository: orgEmailRepo{}, ExternalWarmup: store}

	for range 3 {
		if err := s.HandleNewEmail(t.Context(), arrival("Notes WRM-7Q2X")); err != nil {
			t.Fatal(err)
		}
	}
	if inbox.calls != 0 {
		t.Fatalf("tagged warmup was stored %d times", inbox.calls)
	}
	if err := s.HandleNewEmail(t.Context(), arrival("Lunch on Friday?")); err != nil {
		t.Fatal(err)
	}
	if inbox.calls != 1 {
		t.Fatal("ordinary mail was not stored")
	}
	if store.reads != 1 {
		t.Fatalf("rule read %d times for four arrivals, want once while cached", store.reads)
	}
}

func TestExternalWarmupRuleFailureKeepsTheMail(t *testing.T) {
	inbox := &diagnosticInboxRepo{}
	store := &externalWarmupFake{rule: models.ExternalWarmupSettings{Tags: []string{"wrm-7q2x"}}, err: errors.New("settings unavailable")}
	s := &JobsService{UniboxRepository: inbox, EmailRepository: orgEmailRepo{}, ExternalWarmup: store}
	if err := s.HandleNewEmail(t.Context(), arrival("Notes WRM-7Q2X")); err != nil {
		t.Fatal(err)
	}
	if inbox.calls != 1 {
		t.Fatal("an unreadable rule dropped the arrival")
	}
}

func TestExternalWarmupSweepRunsOncePerRule(t *testing.T) {
	full := make([]models.JobEventNewEmail, externalWarmupDeleteBatch)
	for i := range full {
		full[i] = models.JobEventNewEmail{Message: &models.EmailMessageStoreData{ID: uuid.New()}}
	}
	store := &externalWarmupFake{rule: models.ExternalWarmupSettings{HideWarmblyMarked: true}, batches: [][]models.JobEventNewEmail{full, full[:3]}}
	s := &JobsService{ExternalWarmup: store}

	if err := s.sweepExternalWarmup(t.Context()); err != nil {
		t.Fatal(err)
	}
	if store.sweeps != 2 {
		t.Fatalf("sweep ran %d batches, want to continue past a full batch and stop after a short one", store.sweeps)
	}
	if err := s.sweepExternalWarmup(t.Context()); err != nil {
		t.Fatal(err)
	}
	if store.sweeps != 2 {
		t.Fatal("an unchanged rule was swept again")
	}
	store.rule.Tags = []string{"wrm-7q2x"}
	if err := s.sweepExternalWarmup(t.Context()); err != nil {
		t.Fatal(err)
	}
	if store.sweeps != 3 {
		t.Fatal("a changed rule was not swept")
	}

	// Off then on with the same rule sweeps again: mail stored while it was off is removed.
	on := store.rule
	store.rule = models.ExternalWarmupSettings{}
	if err := s.sweepExternalWarmup(t.Context()); err != nil {
		t.Fatal(err)
	}
	store.rule = on
	if err := s.sweepExternalWarmup(t.Context()); err != nil {
		t.Fatal(err)
	}
	if store.sweeps != 4 {
		t.Fatalf("re-enabling an unchanged rule ran %d sweeps, want the one it missed", store.sweeps-3)
	}
}
