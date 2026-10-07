package cloudlink

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

func TestParticipationUsesCapabilitySpecificRouteAndRefusesOldCloud(t *testing.T) {
	org, account, remote := uuid.New(), uuid.New(), uuid.New()
	calls := 0
	daily, rolling := 8, 11
	participation := models.DiagnosticParticipation{Mode: models.TestParticipationDiagnostic, Send: false, Receive: true, SharedDailyLimit: &daily, RollingRecipientLimit: &rolling}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodPatch || r.URL.Path != "/v1/pool-link/instance/mailboxes/"+remote.String()+"/participation" {
			t.Error("participation sent to legacy silently ignored route", r.Method, r.URL.Path)
		}
		var patch models.PoolLinkMailboxPatch
		if err := json.NewDecoder(r.Body).Decode(&patch); err != nil || patch.Participation == nil || patch.Participation.Send || !patch.Participation.Receive || patch.Participation.SharedDailyLimit == nil || *patch.Participation.SharedDailyLimit != daily || patch.Participation.RollingRecipientLimit == nil || *patch.Participation.RollingRecipientLimit != rolling {
			t.Error("typed directions/limits lost", patch, err)
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"code":"not_found","message":"old cloud route"}`))
	}))
	defer srv.Close()
	s := &service{repo: &stubLinkRepo{link: &models.CloudLink{CloudURL: srv.URL, Token: "local-fixture"}, mailbox: &models.CloudLinkMailbox{EmailAccountID: account, RemoteID: remote}}, emails: stubEmails{account: &models.Email{ID: account, OrganizationID: &org}}, tokens: map[uuid.UUID]cachedToken{}}
	if row, xerr := s.SetParticipation(context.Background(), uuid.New(), account, participation); xerr == nil || row != nil || calls != 0 {
		t.Fatal("cross-tenant participation reached Cloud", row, xerr, calls)
	}
	if row, xerr := s.SetParticipation(context.Background(), org, account, participation); xerr == nil || row != nil || calls != 1 {
		t.Fatal("old Cloud silently accepted unsupported controls", row, xerr, calls)
	}
	participation.Mode = models.TestParticipationOff
	if row, xerr := s.SetParticipation(context.Background(), org, account, participation); xerr == nil || row != nil || calls != 1 {
		t.Fatal("off with receiving enabled accepted", row, xerr, calls)
	}
}
