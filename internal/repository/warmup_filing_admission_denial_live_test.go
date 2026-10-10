package repository

import (
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

func TestLiveWarmupFilingAdmissionDeniesMissingOwnedRow(t *testing.T) {
	for _, scenario := range []string{"completed", "unknown", "foreign", "mixed"} {
		t.Run(scenario, func(t *testing.T) {
			f, r, req := filingFloorFixture(t)
			switch scenario {
			case "completed":
				if err := NewWarmupRecoveryRepository(f.pool).CompleteFiling(t.Context(), req.MailboxID, uuid.MustParse(req.FilingID)); err != nil {
					t.Fatal(err)
				}
			case "unknown":
				req.FilingID = uuid.NewString()
			case "foreign":
				id, err := NewWarmupRecoveryRepository(f.pool).EnqueueFiling(t.Context(), models.WarmupEmailAction{EmailID: f.sender, UserID: f.user, RFCMessageID: "<foreign-filing@example.test>", Actions: req.Actions})
				if err != nil {
					t.Fatal(err)
				}
				req.FilingID = id.String()
			case "mixed":
				if _, err := f.pool.Exec(t.Context(), `UPDATE warmup_pending_filings SET payload=jsonb_set(payload,'{actions}',to_jsonb($2::text[])) WHERE id=$1`, req.FilingID, []string{models.WarmupActionFile, models.WarmupActionDelete}); err != nil {
					t.Fatal(err)
				}
			}
			out, err := r.AdmitWarmupAction(t.Context(), req)
			if err != nil || len(out.Actions) != 0 || out.FilingPending || out.FilingRecovery != nil {
				t.Fatalf("unowned/nonpending exact filing remains admitted: %+v %v", out, err)
			}
		})
	}
}

func TestLiveWarmupFilingAdmissionKeepsUntrackedLegacyAction(t *testing.T) {
	_, r, req := filingFloorFixture(t)
	req.FilingID = ""
	out, err := r.AdmitWarmupAction(t.Context(), req)
	if err != nil || len(out.Actions) != 1 || out.Actions[0] != models.WarmupActionFile || out.FilingPending || out.FilingRecovery != nil {
		t.Fatalf("untracked permitted legacy action changed: %+v %v", out, err)
	}
}
