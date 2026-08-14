package confenge

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

func TestResolvePilotRecipientUsesOnlyCurrentSnapshot(t *testing.T) {
	now := time.Now().UTC()
	currentRun, oldRun := uuid.New(), uuid.New()
	old := readyPilotCandidate(now, oldRun, "old@empresa.com.br")
	old.Recommended = true
	current := readyPilotCandidate(now, currentRun, "atual@empresa.com.br")
	resolved, block := resolvePilotRecipient([]models.OutreachContactCandidate{old, current}, &currentRun, now)
	if block != nil || resolved.Candidate == nil || resolved.Candidate.ID != current.ID {
		t.Fatalf("current authoritative recipient not selected: resolved=%+v block=%+v", resolved, block)
	}

	current.EmailSendReady = false
	if _, block := resolvePilotRecipient([]models.OutreachContactCandidate{old, current}, &currentRun, now); block == nil || block.Code != "recipient_not_send_ready" {
		t.Fatalf("historical valid contact must not resurrect current invalid recipient: %+v", block)
	}
}

func TestResolvePilotRecipientConflictsFailClosed(t *testing.T) {
	now := time.Now().UTC()
	runID := uuid.New()
	first := readyPilotCandidate(now, runID, "comercial@empresa.com.br")
	second := readyPilotCandidate(now, runID, "financeiro@empresa.com.br")
	if _, block := resolvePilotRecipient([]models.OutreachContactCandidate{first, second}, &runID, now); block == nil || block.Code != "recipient_conflict" {
		t.Fatalf("two distinct current addresses must conflict: %+v", block)
	}
}

func TestResolvePilotRecipientSuppressionDominatesDuplicate(t *testing.T) {
	now := time.Now().UTC()
	runID := uuid.New()
	active := readyPilotCandidate(now, runID, "CONTATO@EMPRESA.COM.BR ")
	tombstone := readyPilotCandidate(now, runID, " contato@empresa.com.br")
	tombstone.DoNotContact = true
	if _, block := resolvePilotRecipient([]models.OutreachContactCandidate{active, tombstone}, &runID, now); block == nil || block.Code != "recipient_opt_out" {
		t.Fatalf("DNC duplicate must dominate active history: %+v", block)
	}
}

func TestResolvePilotRecipientCanonicalAndDeterministic(t *testing.T) {
	now := time.Now().UTC()
	runID := uuid.New()
	first := readyPilotCandidate(now, runID, " CONTATO@EMPRESA.COM.BR ")
	second := readyPilotCandidate(now, runID, "contato@empresa.com.br")
	first.ID = uuid.MustParse("00000000-0000-0000-0000-000000000002")
	second.ID = uuid.MustParse("00000000-0000-0000-0000-000000000001")
	first.UpdatedAt, second.UpdatedAt = now, now
	resolved, block := resolvePilotRecipient([]models.OutreachContactCandidate{first, second}, &runID, now)
	if block != nil || resolved.Candidate.ID != second.ID || resolved.Candidate.Email != "contato@empresa.com.br" {
		t.Fatalf("duplicate selection must be stable and canonical: resolved=%+v block=%+v", resolved, block)
	}
}

func TestResolvePilotRecipientAdversarialValidation(t *testing.T) {
	now := time.Now().UTC()
	runID := uuid.New()
	cases := []struct {
		name string
		edit func(*models.OutreachContactCandidate)
		code string
	}{
		{name: "unicode address", edit: func(c *models.OutreachContactCandidate) { c.Email = "contatô@empresa.com.br" }, code: "recipient_invalid"},
		{name: "source domain mismatch", edit: func(c *models.OutreachContactCandidate) { c.SourceURL = "https://outra.com.br/contato" }, code: "recipient_domain_mismatch"},
		{name: "missing timestamp", edit: func(c *models.OutreachContactCandidate) { c.SourceDate = nil }, code: "recipient_evidence_date_missing"},
		{name: "stale evidence", edit: func(c *models.OutreachContactCandidate) {
			stale := now.Add(-366 * 24 * time.Hour)
			c.SourceDate = &stale
		}, code: "recipient_evidence_stale"},
		{name: "hard bounce", edit: func(c *models.OutreachContactCandidate) { c.Bounced = true }, code: "recipient_hard_bounce"},
		{name: "tainted provenance", edit: func(c *models.OutreachContactCandidate) { c.Blocked = true; c.BlockReason = "provenance tainted" }, code: "provenance_tainted"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			candidate := readyPilotCandidate(now, runID, "contato@empresa.com.br")
			test.edit(&candidate)
			if _, block := resolvePilotRecipient([]models.OutreachContactCandidate{candidate}, &runID, now); block == nil || block.Code != test.code {
				t.Fatalf("block=%+v want %s", block, test.code)
			}
		})
	}
}

func TestResolvePilotRecipientRemovedAndGenericWarning(t *testing.T) {
	now := time.Now().UTC()
	currentRun, oldRun := uuid.New(), uuid.New()
	old := readyPilotCandidate(now, oldRun, "contato@empresa.com.br")
	if _, block := resolvePilotRecipient([]models.OutreachContactCandidate{old}, &currentRun, now); block == nil || block.Code != "recipient_removed_current_snapshot" {
		t.Fatalf("removed contact must fail closed: %+v", block)
	}
	generic := readyPilotCandidate(now, currentRun, "contato@empresa.com.br")
	generic.MailboxPurpose = "GENERIC_CONTACT"
	resolved, block := resolvePilotRecipient([]models.OutreachContactCandidate{generic}, &currentRun, now)
	if block != nil || len(resolved.Warnings) != 1 || resolved.Warnings[0] != "generic_mailbox_allowed_by_policy" {
		t.Fatalf("generic mailbox warning missing: resolved=%+v block=%+v", resolved, block)
	}
}

func readyPilotCandidate(now time.Time, runID uuid.UUID, email string) models.OutreachContactCandidate {
	sourceDate := now.Add(-time.Hour)
	return models.OutreachContactCandidate{
		ID: uuid.New(), LastImportRunID: &runID, Email: email,
		SourceURL: "https://empresa.com.br/contato", SourceDate: &sourceDate,
		VerificationStatus: models.OutreachVerifyOfficialSource,
		EmailSendReady:     true, OwnershipStatus: "COMPANY_OWNED",
		RecipientCommercialSuitability: "SUITABLE", UpdatedAt: now,
	}
}
