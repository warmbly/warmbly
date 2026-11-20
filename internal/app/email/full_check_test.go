package email

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/instancesettings"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type countingAllowance struct {
	used      int
	allowance *int
	reads     int
	err       *errx.Error
}

func (a *countingAllowance) MailboxAllowance(context.Context, uuid.UUID) (*models.MailboxAllowance, *errx.Error) {
	a.reads++
	if a.err != nil {
		return nil, a.err
	}
	return &models.MailboxAllowance{Used: a.used, Allowance: a.allowance}, nil
}

type fixedSyncBudget struct{ sync instancesettings.Sync }

func (b fixedSyncBudget) SyncBudget(context.Context) instancesettings.Sync { return b.sync }

type linkedMailboxes map[uuid.UUID]bool

type poolLinkByAccount struct {
	repository.PoolLinkRepository
	linked linkedMailboxes
}

func (p poolLinkByAccount) GetMailboxByAccount(_ context.Context, id uuid.UUID) (*models.PoolLinkMailbox, error) {
	if p.linked[id] {
		return &models.PoolLinkMailbox{}, nil
	}
	return nil, nil
}

type seedMailboxes map[uuid.UUID]bool

func (s seedMailboxes) SeedScope(_ context.Context, id uuid.UUID) (string, error) {
	if s[id] {
		return "instance", nil
	}
	return "", nil
}

func fullCheckSeconds(t *testing.T, svc *emailService, mailbox uuid.UUID, org *uuid.UUID) int {
	t.Helper()
	data, err := svc.syncDataFor(context.Background(), mailbox, org, false)
	if err != nil {
		t.Fatal(err)
	}
	return data.Policy.FullCheckSeconds
}

func TestSyncPolicyCarriesTheWorkspacesFullCheckInterval(t *testing.T) {
	ten := 10
	for _, tc := range []struct {
		name      string
		used      int
		allowance *int
		want      int
	}{
		{"empty", 0, &ten, 15 * 60},
		{"a quarter", 2, &[]int{8}[0], 15 * 60},
		{"half", 5, &ten, 30 * 60},
		{"full", 10, &ten, 60 * 60},
		{"over", 20, &ten, 60 * 60},
		{"unlimited", 300, nil, 15 * 60},
	} {
		org := uuid.New()
		svc := &emailService{allowance: &countingAllowance{used: tc.used, allowance: tc.allowance}}
		if got := fullCheckSeconds(t, svc, uuid.New(), &org); got != tc.want {
			t.Errorf("%s: %d seconds, want %d", tc.name, got, tc.want)
		}
	}
}

func TestFullCheckIntervalReadsTheAllowanceOncePerWorkspace(t *testing.T) {
	ten := 10
	allowance := &countingAllowance{used: 10, allowance: &ten}
	svc := &emailService{allowance: allowance}
	a, b := uuid.New(), uuid.New()
	for range 25 {
		fullCheckSeconds(t, svc, uuid.New(), &a)
		fullCheckSeconds(t, svc, uuid.New(), &b)
	}
	if allowance.reads != 2 {
		t.Fatalf("read the allowance %d times for two workspaces' 50 mailboxes", allowance.reads)
	}
}

func TestFullCheckIntervalFallsBackToTheMinimum(t *testing.T) {
	org := uuid.New()
	svc := &emailService{allowance: &countingAllowance{err: errx.InternalError()}}
	if got := fullCheckSeconds(t, svc, uuid.New(), &org); got != config.SyncFullCheckMinMinutesDefault*60 {
		t.Fatalf("an unreadable allowance gave %d seconds", got)
	}
	if got := fullCheckSeconds(t, &emailService{}, uuid.New(), nil); got != config.SyncFullCheckMinMinutesDefault*60 {
		t.Fatalf("a mailbox with no workspace gave %d seconds", got)
	}
}

func TestFullCheckIntervalFollowsTheOperatorsBounds(t *testing.T) {
	sync := instancesettings.DefaultSync()
	sync.FullCheckMinMinutes, sync.FullCheckMaxMinutes = 10, 120
	ten := 10
	org := uuid.New()
	svc := &emailService{allowance: &countingAllowance{used: 5, allowance: &ten}}
	svc.WireSyncBudget(fixedSyncBudget{sync: sync})
	if got := fullCheckSeconds(t, svc, uuid.New(), &org); got != 60*60 {
		t.Fatalf("half the allowance under 10-120 minutes gave %d seconds", got)
	}
}

// A placement seed and a pool-linked mailbox keep their faster cadence whatever the workspace holds.
func TestSeedAndPoolLinkedMailboxesKeepTheFastCadence(t *testing.T) {
	ten := 10
	org := uuid.New()
	seed, linked, plain := uuid.New(), uuid.New(), uuid.New()
	svc := &emailService{allowance: &countingAllowance{used: 10, allowance: &ten}}
	svc.WireSeedScope(seedMailboxes{seed: true})
	svc.WirePoolLink(poolLinkByAccount{linked: linkedMailboxes{linked: true}})
	for id, want := range map[uuid.UUID]int{seed: config.SyncFullCheckFastSeconds, linked: config.SyncFullCheckFastSeconds, plain: 60 * 60} {
		if got := fullCheckSeconds(t, svc, id, &org); got != want {
			t.Errorf("%v: %d seconds, want %d", id, got, want)
		}
	}
}
