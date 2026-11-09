package mailboxconnect

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// grantMailboxes answers each grant by its refresh token: "new", "renew" or anything else to fail.
type grantMailboxes struct {
	Mailboxes
	started atomic.Int32
}

func (g *grantMailboxes) ConnectWithGrant(_ context.Context, _ string, _ uuid.UUID, gr models.MailboxOAuthGrant, _ func(uuid.UUID) bool) (*models.Email, bool, *errx.Error) {
	switch gr.RefreshToken {
	case "new":
		return &models.Email{ID: uuid.New(), Email: gr.Email}, false, nil
	case "renew":
		return &models.Email{ID: uuid.New(), Email: gr.Email}, true, nil
	}
	return nil, false, errx.NewWithIdentifier(errx.Unprocessable, "connect_grant_refused", "refused")
}

func (g *grantMailboxes) SetWarmupLifecycle(context.Context, string, string, string) (*models.Email, *errx.Error) {
	g.started.Add(1)
	return &models.Email{}, nil
}

func TestGrantBulkAnswersEveryRowAndWarmsOnlyNewMailboxes(t *testing.T) {
	mb := &grantMailboxes{}
	s := New(nil, mb, nil, nil, nil, nil, "")
	grants := []models.MailboxOAuthGrant{
		{RefreshToken: "new", Email: "a@acme.com"},
		{RefreshToken: "renew", Email: "b@acme.com"},
		{RefreshToken: "bad", Email: "c@acme.com"},
		{RefreshToken: "new", Email: "d@acme.com"},
	}
	res, xerr := s.ConnectGrantBulk(context.Background(), uuid.New(), uuid.NewString(), grants, &models.MailboxConnectWarmup{Enabled: true}, nil)
	if xerr != nil {
		t.Fatal(xerr)
	}
	want := []models.MailboxBulkRowStatus{models.MailboxBulkConnected, models.MailboxBulkRenewed, models.MailboxBulkFailed, models.MailboxBulkConnected}
	for i, r := range res.Data {
		if r.Row != i+1 || r.Status != want[i] {
			t.Errorf("row %d = %+v, want status %s", i+1, r, want[i])
		}
	}
	if res.Data[2].Code != "connect_grant_refused" {
		t.Errorf("failed row code = %q", res.Data[2].Code)
	}
	if s := res.Summary; s.Total != 4 || s.Connected != 2 || s.Renewed != 1 || s.Failed != 1 {
		t.Errorf("summary = %+v", s)
	}
	// A renewed mailbox keeps its warmup as it was; only the two new ones start.
	if n := mb.started.Load(); n != 2 {
		t.Errorf("warmup started on %d mailboxes, want 2", n)
	}
	if ValidateGrantBatch(0) == nil || ValidateGrantBatch(GrantBulkMax+1) == nil || ValidateGrantBatch(GrantBulkMax) != nil {
		t.Error("batch size bounds")
	}
}

// warmupMailboxes refuses the ramp settings, or the start, as each test asks.
type warmupMailboxes struct {
	Mailboxes
	settingsErr, startErr *errx.Error
}

func (w warmupMailboxes) Update(context.Context, string, string, string, *models.UpdateEmail) (*models.Email, *errx.Error) {
	return nil, w.settingsErr
}

func (w warmupMailboxes) SetWarmupLifecycle(_ context.Context, _, id, _ string) (*models.Email, *errx.Error) {
	if w.startErr != nil {
		return nil, w.startErr
	}
	return &models.Email{ID: uuid.MustParse(id)}, nil
}

// A refused ramp and a failed start are told apart, and an internal failure's detail never reaches the note.
func TestApplyWarmupSeparatesSettingsFromStart(t *testing.T) {
	ctx, org, id := context.Background(), uuid.New(), uuid.New()
	w := &models.MailboxConnectWarmup{Enabled: true, Start: intp(5)}

	s := New(nil, warmupMailboxes{settingsErr: errx.New(errx.BadRequest, "start too high")}, nil, nil, nil, nil, "")
	acc, notes, started := s.applyWarmup(ctx, org, "", id, w)
	if !started || notes.start != "" || notes.settings == "" || acc == nil || acc.ID != id {
		t.Fatalf("settings refused: started=%v notes=%+v acc=%v", started, notes, acc)
	}

	s = New(nil, warmupMailboxes{startErr: errx.New(errx.Internal, "Warmbly Cloud answered 500.")}, nil, nil, nil, nil, "")
	_, notes, started = s.applyWarmup(ctx, org, "", id, w)
	if started || notes.start == "" {
		t.Fatalf("start failed: started=%v notes=%+v", started, notes)
	}
	if strings.Contains(notes.start, "500") {
		t.Fatalf("an internal failure leaked into the note: %q", notes.start)
	}
}
