package wmail

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/pkg/nodeevidence"
	"github.com/warmbly/warmbly/internal/repository"
)

func TestControlPlane503EvidenceHoldsCursorsAndIsNotProviderFailure(t *testing.T) {
	var got nodeevidence.Event
	nodeevidence.Install(func(e nodeevidence.Event) { got = e })
	t.Cleanup(func() { nodeevidence.Install(nil) })
	w := &WMail{ID: uuid.New()}
	stats := &tickStats{}
	if err := w.controlPlaneError(fmt.Errorf("wrapped: %w", &repository.ControlPlaneHTTPError{Status: 503}), stats); err != nil {
		t.Fatal("control-plane failure became a provider error", err)
	}
	if !stats.aborted || got.Name != nodeevidence.ControlPlaneHeld || got.Category != "control_plane" || got.HTTPStatus != 503 || got.MailboxID != w.ID {
		t.Fatal("missing cursor-held evidence", got)
	}
}
