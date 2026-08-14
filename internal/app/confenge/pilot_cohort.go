package confenge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

const PilotCohortTarget = 30

const (
	PilotPrepared = "PREPARED"
	PilotBlocked  = "BLOCKED"
)

type PilotOperation struct {
	IdempotencyKey string
	RequestID      string
}

type PilotAccountResult struct {
	AccountID     uuid.UUID  `json:"account_id"`
	CNPJ14        string     `json:"cnpj14,omitempty"`
	Company       string     `json:"company,omitempty"`
	Status        string     `json:"status"`
	ReasonCode    string     `json:"reason_code,omitempty"`
	Reason        string     `json:"human_readable_reason,omitempty"`
	Remediation   string     `json:"remediation,omitempty"`
	PreviousState string     `json:"previous_state,omitempty"`
	IntendedState string     `json:"intended_state"`
	ContactState  string     `json:"contact_state,omitempty"`
	Recipient     string     `json:"recipient,omitempty"`
	RecipientName string     `json:"recipient_name,omitempty"`
	RecipientRole string     `json:"recipient_role,omitempty"`
	ContactID     *uuid.UUID `json:"contact_candidate_id,omitempty"`
	TouchpointID  *uuid.UUID `json:"touchpoint_id,omitempty"`
	DraftID       *uuid.UUID `json:"draft_id,omitempty"`
	DraftState    string     `json:"draft_state,omitempty"`
	Warnings      []string   `json:"warnings,omitempty"`
	SnapshotHash  string     `json:"upstream_snapshot_hash,omitempty"`
	ContextHash   string     `json:"message_context_hash,omitempty"`
	PreparedAt    *time.Time `json:"prepared_at,omitempty"`
	Idempotent    bool       `json:"idempotent"`
}

type PilotCohortResult struct {
	CohortID       string               `json:"cohort_id"`
	Target         int                  `json:"target"`
	Selected       int                  `json:"selected"`
	Prepared       int                  `json:"prepared"`
	Blocked        int                  `json:"blocked"`
	ContactNeeded  int                  `json:"contact_needed"`
	CohortPrepared int                  `json:"cohort_prepared"`
	Remaining      int                  `json:"remaining"`
	SnapshotHash   string               `json:"upstream_snapshot_hash,omitempty"`
	FeedTimestamp  *time.Time           `json:"feed_timestamp,omitempty"`
	Results        []PilotAccountResult `json:"results"`
}

type pilotFeedEvidence struct {
	State        string
	SnapshotHash string
	RunID        string
	Timestamp    *time.Time
}

func (s *service) PreparePilotCohort(ctx context.Context, orgID, userID uuid.UUID, accountIDs []uuid.UUID, operation PilotOperation) (*PilotCohortResult, *errx.Error) {
	if xerr := s.requireEnabled(); xerr != nil {
		return nil, xerr
	}
	if !s.cfg.RequireHumanApproval || s.cfg.AutoSendEnabled || s.cfg.GreenAutorunEnabled {
		return nil, errx.New(errx.Conflict, "pilot safety configuration requires human approval and auto_send=false")
	}
	unique := uniqueAccountIDs(accountIDs)
	if len(unique) == 0 || len(unique) > PilotCohortTarget {
		return nil, errx.New(errx.BadRequest, "account_ids must contain between 1 and 30 unique accounts")
	}
	if len(unique) != len(accountIDs) {
		return nil, errx.New(errx.BadRequest, "account_ids must contain valid, unique accounts")
	}
	operation.IdempotencyKey = strings.TrimSpace(operation.IdempotencyKey)
	if operation.IdempotencyKey == "" {
		return nil, errx.New(errx.BadRequest, "Idempotency-Key is required")
	}
	if len(operation.IdempotencyKey) > 200 {
		return nil, errx.New(errx.BadRequest, "Idempotency-Key must be at most 200 characters")
	}
	existingMembers, err := s.pilotPreparedAccounts(ctx, orgID)
	if err != nil {
		return nil, errx.New(errx.ServiceUnavailable, "pilot membership state is unavailable")
	}
	requestHash := pilotOperationHash(unique)
	if err := s.repo.ClaimPilotOperation(ctx, orgID, operation.IdempotencyKey, requestHash); err != nil {
		if errors.Is(err, repository.ErrPilotIdempotencyConflict) {
			return nil, errx.New(errx.Conflict, "Idempotency-Key was already used with a different account set")
		}
		return nil, errx.New(errx.ServiceUnavailable, "pilot operation idempotency is unavailable")
	}
	feed := s.pilotFeedEvidence(ctx, orgID, time.Now().UTC())
	cohortID := uuid.NewSHA1(orgID, []byte("confenge-pilot-v1")).String()
	result := &PilotCohortResult{
		CohortID: cohortID, Target: PilotCohortTarget, Selected: len(unique),
		SnapshotHash: feed.SnapshotHash, FeedTimestamp: feed.Timestamp,
		Results: make([]PilotAccountResult, 0, len(unique)),
	}
	for _, accountID := range unique {
		accountResult := s.preparePilotAccount(ctx, orgID, userID, accountID, operation, requestHash, feed, cohortID)
		result.Results = append(result.Results, accountResult)
		if accountResult.Status == PilotPrepared {
			result.Prepared++
			existingMembers[accountID] = true
		} else {
			result.Blocked++
			if strings.HasPrefix(accountResult.ReasonCode, "recipient_") || accountResult.ReasonCode == "provenance_tainted" || accountResult.ReasonCode == "generic_mailbox_not_allowed" {
				result.ContactNeeded++
			}
		}
	}
	result.CohortPrepared = len(existingMembers)
	current, err := s.repo.ListPilotMemberships(ctx, orgID, cohortID)
	if err != nil {
		return nil, errx.New(errx.ServiceUnavailable, "pilot membership state is unavailable after preparation")
	}
	result.CohortPrepared = len(current)
	if result.CohortPrepared > PilotCohortTarget {
		result.CohortPrepared = PilotCohortTarget
	}
	result.Remaining = PilotCohortTarget - result.CohortPrepared
	return result, nil
}

func (s *service) preparePilotAccount(
	ctx context.Context,
	orgID, userID, accountID uuid.UUID,
	operation PilotOperation,
	requestHash string,
	feed pilotFeedEvidence,
	cohortID string,
) PilotAccountResult {
	now := time.Now().UTC()
	result := PilotAccountResult{
		AccountID: accountID, Status: PilotBlocked, IntendedState: models.TouchpointNeedsReview,
		SnapshotHash: feed.SnapshotHash,
	}
	acc, err := s.repo.GetAccount(ctx, orgID, accountID)
	if err != nil || acc == nil {
		return s.finishPilotResult(ctx, orgID, userID, cohortID, operation, result, pilotBlock{Code: "account_not_found", Reason: "A conta selecionada não existe mais.", Remediation: "Atualize a lista e selecione novamente."})
	}
	result.CNPJ14 = acc.CNPJ14
	result.Company = firstNonEmpty(acc.NomeFantasia, acc.RazaoSocial)
	result.PreviousState = acc.QueueState
	result.ContextHash = acc.MessageContextHash

	switch feed.State {
	case "missing":
		return s.finishPilotResult(ctx, orgID, userID, cohortID, operation, result, pilotBlock{Code: "feed_missing", Reason: "Nenhum snapshot autoritativo sincronizado está disponível.", Remediation: "Sincronize o feed CONFENGE antes de preparar a coorte."})
	case "stale":
		return s.finishPilotResult(ctx, orgID, userID, cohortID, operation, result, pilotBlock{Code: "feed_stale", Reason: "O snapshot autoritativo está obsoleto.", Remediation: "Sincronize um snapshot atual e tente novamente."})
	}
	if strings.TrimSpace(feed.RunID) == "" || strings.TrimSpace(acc.SourceRunID) != strings.TrimSpace(feed.RunID) {
		return s.finishPilotResult(ctx, orgID, userID, cohortID, operation, result, pilotBlock{Code: "account_not_in_current_snapshot", Reason: "A conta não pertence ao snapshot autoritativo atual.", Remediation: "Atualize a seleção após a sincronização; contas removidas não podem ser preparadas."})
	}
	if acc.DoNotContact {
		return s.finishPilotResult(ctx, orgID, userID, cohortID, operation, result, pilotBlock{Code: "account_do_not_contact", Reason: "A conta está marcada como não contatar.", Remediation: "Mantenha a supressão e escolha outra conta."})
	}
	if acc.Blocked {
		return s.finishPilotResult(ctx, orgID, userID, cohortID, operation, result, pilotBlock{Code: "account_blocked", Reason: "A conta está bloqueada para outreach.", Remediation: "Revise o bloqueio antes de qualquer nova preparação."})
	}
	if decision := EvaluateTargetFit(acc); !decision.Eligible {
		code := "account_ineligible"
		if !acc.TargetFitFresh || decision.Reason == TargetFitReasonStale {
			code = "target_fit_stale"
		}
		return s.finishPilotResult(ctx, orgID, userID, cohortID, operation, result, pilotBlock{Code: code, Reason: "A conta não atende mais aos gates de target fit.", Remediation: "Revise os reason codes e aguarde nova evidência elegível."})
	}
	if acc.ActivationExpiresAt != nil && !acc.ActivationExpiresAt.After(now) {
		return s.finishPilotResult(ctx, orgID, userID, cohortID, operation, result, pilotBlock{Code: "commercial_context_expired", Reason: "O contexto comercial desta conta expirou.", Remediation: "Atualize a evidência comercial antes de gerar a mensagem."})
	}
	candidates, err := s.repo.ListCandidates(ctx, orgID, accountID)
	if err != nil {
		return s.finishPilotResult(ctx, orgID, userID, cohortID, operation, result, pilotBlock{Code: "recipient_lookup_failed", Reason: "Não foi possível validar os destinatários desta conta.", Remediation: "Tente novamente usando o mesmo Idempotency-Key."})
	}
	recipient, recipientBlock := resolvePilotRecipient(candidates, acc.LastImportRunID, now)
	if recipientBlock != nil {
		result.ContactState = models.OutreachQueueNeedsContact
		return s.finishPilotResult(ctx, orgID, userID, cohortID, operation, result, *recipientBlock)
	}
	result.ContactState = "READY"
	result.Recipient = recipient.Candidate.Email
	result.RecipientName = recipient.Candidate.Name
	result.RecipientRole = recipient.Candidate.Role
	if isGenericRecipient(recipient.Candidate) {
		result.RecipientName = ""
		result.RecipientRole = ""
	}
	result.ContactID = &recipient.Candidate.ID
	result.Warnings = recipient.Warnings

	evidence, err := s.repo.ListEvidence(ctx, orgID, accountID)
	if err != nil {
		return s.finishPilotResult(ctx, orgID, userID, cohortID, operation, result, pilotBlock{Code: "copy_evidence_unavailable", Reason: "Não foi possível carregar as evidências da conta.", Remediation: "Tente novamente usando o mesmo Idempotency-Key."})
	}
	if block := pilotCopyContextBlock(acc, recipient.Candidate, evidence); block != nil {
		return s.finishPilotResult(ctx, orgID, userID, cohortID, operation, result, *block)
	}
	existing, hasExisting, existingErr := s.existingPilotTouchpoint(ctx, orgID, accountID)
	if existingErr != nil {
		return s.finishPilotResult(ctx, orgID, userID, cohortID, operation, result, pilotBlock{Code: "preparation_failed", Reason: "Não foi possível validar a cadência existente.", Remediation: "Use o request_id nos logs e tente novamente com o mesmo Idempotency-Key."})
	}
	if hasExisting && existing.DraftID != nil &&
		(existing.GeneratedContextHash != acc.MessageContextHash || existing.ContactCandidateID == nil || *existing.ContactCandidateID != recipient.Candidate.ID) {
		return s.finishPilotResult(ctx, orgID, userID, cohortID, operation, result, pilotBlock{
			Code: "recipient_changed_requires_review", Reason: "A cadência existente aponta para outro destinatário ou contexto.",
			Remediation: "Revise a mensagem existente; a aprovação anterior deve permanecer inválida.",
		})
	}
	if _, reserveErr := s.repo.ReservePilotSlot(ctx, orgID, cohortID, acc.ID, acc.CNPJ14, PilotCohortTarget); reserveErr != nil {
		if errors.Is(reserveErr, repository.ErrPilotCapacityReached) {
			return s.finishPilotResult(ctx, orgID, userID, cohortID, operation, result, pilotBlock{Code: "cohort_capacity_reached", Reason: "A coorte piloto já possui 30 contas.", Remediation: "Revise a coorte existente em vez de adicionar outra conta."})
		}
		return s.finishPilotResult(ctx, orgID, userID, cohortID, operation, result, pilotBlock{Code: "cohort_reservation_failed", Reason: "A capacidade da coorte não pôde ser reservada com segurança.", Remediation: "Tente novamente usando o mesmo Idempotency-Key; nenhuma mensagem foi criada."})
	}
	membershipClaimed := false
	defer func() {
		if membershipClaimed {
			return
		}
		releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := s.repo.ReleasePilotSlot(releaseCtx, orgID, cohortID, acc.ID); err != nil {
			slog.ErrorContext(ctx, "confenge pilot slot release failed", "organization_id", orgID, "cohort_id", cohortID, "account_id", acc.ID, "error", err)
		}
	}()

	if hasExisting && existing.DraftID != nil {
		if existing.GeneratedContextHash == acc.MessageContextHash && existing.ContactCandidateID != nil && *existing.ContactCandidateID == recipient.Candidate.ID {
			if existing.State != models.TouchpointNeedsReview {
				return s.finishPilotResult(ctx, orgID, userID, cohortID, operation, result, pilotBlock{
					Code:        "already_advanced",
					Reason:      "A etapa existente já saiu da fila de revisão.",
					Remediation: "Revise ou cancele a etapa atual antes de preparar de novo.",
				})
			}
			if block := s.claimPilotPrepared(ctx, orgID, cohortID, operation, requestHash, feed, acc, recipient.Candidate, existing); block != nil {
				return s.finishPilotResult(ctx, orgID, userID, cohortID, operation, result, *block)
			}
			membershipClaimed = true
			result.Status = PilotPrepared
			result.TouchpointID = &existing.ID
			result.DraftID = existing.DraftID
			result.DraftState = existing.State
			result.PreparedAt = &existing.UpdatedAt
			result.Idempotent = true
			return s.finishPilotResult(ctx, orgID, userID, cohortID, operation, result, pilotBlock{})
		}
	}
	touchpoints, xerr := s.PlanAccountCadence(ctx, orgID, userID, accountID, &recipient.Candidate.ID, models.OutreachChannelEmail)
	if xerr != nil {
		return s.finishPilotResult(ctx, orgID, userID, cohortID, operation, result, pilotBlockFromError(xerr.Message))
	}
	first := firstPilotTouchpoint(touchpoints)
	if first == nil {
		return s.finishPilotResult(ctx, orgID, userID, cohortID, operation, result, pilotBlock{Code: "cohort_membership_failed", Reason: "A primeira etapa da conta não foi persistida.", Remediation: "Tente novamente usando o mesmo Idempotency-Key."})
	}
	if first.ContactCandidateID == nil || *first.ContactCandidateID != recipient.Candidate.ID {
		return s.finishPilotResult(ctx, orgID, userID, cohortID, operation, result, pilotBlock{
			Code: "recipient_changed_requires_review", Reason: "A cadência existente aponta para outro destinatário.",
			Remediation: "Revise e altere o destinatário na mensagem; a aprovação anterior deve permanecer inválida.",
		})
	}
	generated, xerr := s.GenerateTouchpointDraft(ctx, orgID, userID, first.ID)
	if xerr != nil {
		return s.finishPilotResult(ctx, orgID, userID, cohortID, operation, result, pilotBlockFromError(xerr.Message))
	}
	if generated.State != models.TouchpointNeedsReview || generated.DraftID == nil {
		return s.finishPilotResult(ctx, orgID, userID, cohortID, operation, result, pilotBlock{Code: "invalid_review_state", Reason: "A mensagem não terminou na fila de revisão.", Remediation: "Não prossiga com envio; revise a inconsistência operacional."})
	}
	if block := s.claimPilotPrepared(ctx, orgID, cohortID, operation, requestHash, feed, acc, recipient.Candidate, generated); block != nil {
		return s.finishPilotResult(ctx, orgID, userID, cohortID, operation, result, *block)
	}
	membershipClaimed = true
	result.Status = PilotPrepared
	result.TouchpointID = &generated.ID
	result.DraftID = generated.DraftID
	result.DraftState = generated.State
	result.PreparedAt = &generated.UpdatedAt
	return s.finishPilotResult(ctx, orgID, userID, cohortID, operation, result, pilotBlock{})
}

func (s *service) claimPilotPrepared(ctx context.Context, orgID uuid.UUID, cohortID string, operation PilotOperation, requestHash string, feed pilotFeedEvidence, acc *models.OutreachAccount, candidate *models.OutreachContactCandidate, touchpoint *models.OutreachTouchpoint) *pilotBlock {
	if acc == nil || candidate == nil || touchpoint == nil || touchpoint.DraftID == nil || feed.Timestamp == nil {
		return &pilotBlock{Code: "cohort_membership_failed", Reason: "A coorte não possui dependências completas.", Remediation: "Tente novamente com o mesmo Idempotency-Key; nenhum envio foi autorizado."}
	}
	membership, _, claimErr := s.repo.ClaimPilotMembership(ctx, &models.OutreachPilotMembership{
		OrganizationID: orgID, CohortID: cohortID, AccountID: acc.ID, CNPJ14: acc.CNPJ14,
		ContactCandidateID: candidate.ID, TouchpointID: touchpoint.ID, DraftID: *touchpoint.DraftID,
		SnapshotHash: feed.SnapshotHash, SourceRunID: feed.RunID, ContextHash: acc.MessageContextHash,
		OperationKey: operation.IdempotencyKey, RequestHash: requestHash,
		FeedGeneratedAt: *feed.Timestamp, CandidateUpdatedAt: candidate.UpdatedAt,
	}, PilotCohortTarget)
	if claimErr != nil {
		if errors.Is(claimErr, repository.ErrPilotCapacityReached) {
			return &pilotBlock{Code: "cohort_capacity_reached", Reason: "A coorte piloto já possui 30 contas.", Remediation: "Revise a coorte existente em vez de adicionar outra conta."}
		}
		return &pilotBlock{Code: "cohort_membership_failed", Reason: "A coorte não pôde validar atomicamente o touchpoint e o draft.", Remediation: "Tente novamente com o mesmo Idempotency-Key; nenhum envio foi autorizado."}
	}
	if membership.TouchpointID != touchpoint.ID || membership.DraftID != *touchpoint.DraftID ||
		membership.ContactCandidateID != candidate.ID || membership.ContextHash != acc.MessageContextHash ||
		membership.SnapshotHash != feed.SnapshotHash || membership.SourceRunID != feed.RunID {
		return &pilotBlock{Code: "cohort_membership_conflict", Reason: "A conta já pertence à coorte com outra mensagem, destinatário ou contexto.", Remediation: "Revise a membership existente; não crie uma autorização paralela."}
	}
	return nil
}

func (s *service) finishPilotResult(ctx context.Context, orgID, userID uuid.UUID, cohortID string, operation PilotOperation, result PilotAccountResult, block pilotBlock) PilotAccountResult {
	if result.Status != PilotPrepared {
		result.ReasonCode = block.Code
		result.Reason = block.Reason
		result.Remediation = block.Remediation
	}
	gate := result.ReasonCode
	if gate == "" {
		gate = "all_gates_passed"
	}
	slog.InfoContext(ctx, "confenge pilot cohort account result",
		"request_id", operation.RequestID, "idempotency_key", operation.IdempotencyKey,
		"user_id", userID, "organization_id", orgID, "cohort_id", cohortID,
		"account_id", result.AccountID, "cnpj14", result.CNPJ14,
		"previous_state", result.PreviousState, "intended_state", result.IntendedState,
		"status", result.Status, "gate", gate, "reason_code", result.ReasonCode,
		"upstream_snapshot_hash", result.SnapshotHash,
	)
	if s.audit != nil && result.AccountID != uuid.Nil {
		s.audit.LogAction(ctx, orgID, userID, models.AuditActionCreate, models.AuditEntityOutreachAccount, &result.AccountID, "", "",
			map[string]string{"cohort_id": cohortID, "status": result.Status, "reason_code": result.ReasonCode},
			map[string]string{"request_id": operation.RequestID, "snapshot_hash": result.SnapshotHash},
		)
	}
	return result
}

func (s *service) pilotFeedEvidence(ctx context.Context, orgID uuid.UUID, now time.Time) pilotFeedEvidence {
	maxAge := s.cfg.FeedMaxAge
	if maxAge <= 0 {
		maxAge = 24 * time.Hour
	}
	state, stateErr := s.repo.GetFeedSyncState(ctx, orgID)
	if stateErr != nil {
		return pilotFeedEvidence{State: "missing"}
	}
	if state != nil {
		if state.LastStatus != "completed" || state.SourceGeneratedAt == nil || state.LastSnapshotHash == "" || state.LastRunID == "" {
			return pilotFeedEvidence{State: "missing"}
		}
		value := pilotFeedEvidence{State: "fresh", SnapshotHash: state.LastSnapshotHash, RunID: state.LastRunID, Timestamp: state.SourceGeneratedAt}
		if state.SourceGeneratedAt.After(now.Add(5*time.Minute)) || now.Sub(state.SourceGeneratedAt.UTC()) > maxAge {
			value.State = "stale"
		}
		return value
	}
	if runs, err := s.repo.ListImportRuns(ctx, orgID, 1); err == nil && len(runs) > 0 && runs[0].Status == models.OutreachImportCompleted {
		timestamp := runs[0].SourceGeneratedAt
		value := pilotFeedEvidence{State: "fresh", SnapshotHash: runs[0].SnapshotHash, RunID: runs[0].SourceRunID, Timestamp: timestamp}
		if timestamp == nil || now.Sub(timestamp.UTC()) > maxAge {
			value.State = "stale"
		}
		return value
	}
	return pilotFeedEvidence{State: "missing"}
}

func (s *service) pilotPreparedAccounts(ctx context.Context, orgID uuid.UUID) (map[uuid.UUID]bool, error) {
	result := map[uuid.UUID]bool{}
	cohortID := uuid.NewSHA1(orgID, []byte("confenge-pilot-v1")).String()
	memberships, err := s.repo.ListPilotMemberships(ctx, orgID, cohortID)
	if err != nil {
		return nil, err
	}
	for i := range memberships {
		result[memberships[i].AccountID] = true
	}
	return result, nil
}

func (s *service) existingPilotTouchpoint(ctx context.Context, orgID, accountID uuid.UUID) (*models.OutreachTouchpoint, bool, error) {
	touchpoints, err := s.repo.ListTouchpoints(ctx, orgID, accountID, "", 50, 0)
	if err != nil {
		return nil, false, err
	}
	first := firstPilotTouchpoint(touchpoints)
	return first, first != nil, nil
}

func (s *service) requirePilotMembershipForTouchpoint(ctx context.Context, orgID uuid.UUID, touchpoint *models.OutreachTouchpoint) error {
	if touchpoint == nil || touchpoint.DraftID == nil || touchpoint.ContactCandidateID == nil {
		return errors.New("pilot touchpoint dependencies are incomplete")
	}
	cohortID := uuid.NewSHA1(orgID, []byte("confenge-pilot-v1")).String()
	memberships, err := s.repo.ListPilotMemberships(ctx, orgID, cohortID)
	if err != nil {
		return fmt.Errorf("pilot membership state is unavailable: %w", err)
	}
	for i := range memberships {
		membership := &memberships[i]
		if membership.TouchpointID != touchpoint.ID {
			continue
		}
		if membership.AccountID != touchpoint.AccountID || membership.DraftID != *touchpoint.DraftID ||
			membership.ContactCandidateID != *touchpoint.ContactCandidateID ||
			membership.ContextHash != touchpoint.GeneratedContextHash {
			return errors.New("pilot membership does not match the exact message authorization context")
		}
		return nil
	}
	return errors.New("touchpoint is not a member of the controlled pilot cohort")
}

func pilotCopyContextBlock(acc *models.OutreachAccount, candidate *models.OutreachContactCandidate, evidence []models.OutreachEvidence) *pilotBlock {
	playbook, _ := LoadPlaybook()
	strategy := PlanOutreachStrategy(playbook, acc, candidate, evidence, 1)
	incomplete := strings.TrimSpace(strategy.MicroOfferCode) == "" || strings.TrimSpace(strategy.WhyThisAccount) == "" ||
		strings.TrimSpace(strategy.WhyNow) == "" || strings.TrimSpace(strategy.ObservedFact) == "" ||
		containsStr(strategy.RiskFlags, "incomplete_strategy") || containsStr(strategy.RiskFlags, "incomplete_copy_context") ||
		containsStr(strategy.RiskFlags, "unknown_service_code") || containsStr(strategy.RiskFlags, "missing_service_code")
	if incomplete {
		return &pilotBlock{Code: "incomplete_copy_context", Reason: "Não há contexto comercial suficiente para uma mensagem factual.", Remediation: "Atualize serviço, oferta e evidência específica da conta antes de gerar."}
	}
	return nil
}

func pilotBlockFromError(message string) pilotBlock {
	lower := strings.ToLower(strings.TrimSpace(message))
	switch {
	case strings.Contains(lower, "email_send_ready"):
		return pilotBlock{Code: "recipient_not_send_ready", Reason: "O destinatário não está validado para email comercial.", Remediation: "Revalide o contato e sincronize o feed."}
	case strings.Contains(lower, "contact candidate") || strings.Contains(lower, "contact is not enrollable"):
		return pilotBlock{Code: "recipient_invalid", Reason: "O destinatário não atende aos gates de contato.", Remediation: "Resolva outro contato corporativo validado."}
	case strings.Contains(lower, "target fit") || strings.Contains(lower, "commercial outreach blocked"):
		return pilotBlock{Code: "account_ineligible", Reason: "A conta não atende aos gates de target fit.", Remediation: "Revise a elegibilidade antes de tentar novamente."}
	case strings.Contains(lower, "incomplete") || strings.Contains(lower, "service"):
		return pilotBlock{Code: "incomplete_copy_context", Reason: "O contexto comercial não sustenta uma mensagem factual.", Remediation: "Atualize a evidência e o serviço selecionado."}
	default:
		return pilotBlock{Code: "preparation_failed", Reason: "A preparação falhou antes de criar uma mensagem revisável.", Remediation: "Use o request_id nos logs e tente novamente com o mesmo Idempotency-Key."}
	}
}

func firstPilotTouchpoint(touchpoints []models.OutreachTouchpoint) *models.OutreachTouchpoint {
	for i := range touchpoints {
		if touchpoints[i].Ordinal == 1 {
			return &touchpoints[i]
		}
	}
	return nil
}

func uniqueAccountIDs(values []uuid.UUID) []uuid.UUID {
	seen := make(map[uuid.UUID]bool, len(values))
	result := make([]uuid.UUID, 0, len(values))
	for _, value := range values {
		if value == uuid.Nil || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func pilotOperationHash(accountIDs []uuid.UUID) string {
	values := make([]string, 0, len(accountIDs))
	for _, accountID := range accountIDs {
		values = append(values, accountID.String())
	}
	sort.Strings(values)
	sum := sha256.Sum256([]byte(strings.Join(values, ",")))
	return hex.EncodeToString(sum[:])
}
