package confenge

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// AttentionItem is one needs-attention / cockpit row for human reply handoff.
type AttentionItem struct {
	AccountID       uuid.UUID       `json:"account_id"`
	CompanyName     string          `json:"company_name"`
	CNPJ14          string          `json:"cnpj14"`
	ContactName     string          `json:"contact_name,omitempty"`
	ContactEmail    string          `json:"contact_email,omitempty"`
	ContactPhone    string          `json:"contact_phone,omitempty"`
	Channel         string          `json:"channel,omitempty"`
	ServiceCode     string          `json:"service_code,omitempty"`
	ServiceName     string          `json:"service_name,omitempty"`
	FactToMention   string          `json:"fact_to_mention,omitempty"`
	QueueState      string          `json:"queue_state"`
	CommercialState string          `json:"commercial_state"`
	DoNotContact    bool            `json:"do_not_contact"`
	Blocked         bool            `json:"blocked"`
	Intent          string          `json:"intent,omitempty"`
	Confidence      float64         `json:"confidence,omitempty"`
	SuggestedAction string          `json:"suggested_action,omitempty"`
	Evidence        []EvidenceBrief `json:"evidence,omitempty"`
	LastSnippet     string          `json:"last_snippet,omitempty"`
	ThreadSubject   string          `json:"thread_subject,omitempty"`
	Thread          string          `json:"thread,omitempty"`
	UpdatedAt       time.Time       `json:"updated_at"`
	ReplyDraftID    *uuid.UUID      `json:"reply_draft_id,omitempty"`
	ResumeAt        string          `json:"resume_at,omitempty"`
}

// EvidenceBrief is a compact evidence row for the attention detail pane.
type EvidenceBrief struct {
	ID             string `json:"id"`
	Title          string `json:"title,omitempty"`
	Excerpt        string `json:"excerpt,omitempty"`
	EpistemicClass string `json:"epistemic_class,omitempty"`
	URL            string `json:"url,omitempty"`
}

// ListAttention returns cockpit items for a filter (needs_attention default).
// awaiting_approval is draft-centric (reply/resume drafts in NEEDS_REVIEW).
func (s *service) ListAttention(ctx context.Context, orgID uuid.UUID, filter string, limit int) ([]AttentionItem, *errx.Error) {
	if xerr := s.requireEnabled(); xerr != nil {
		return nil, xerr
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	f := strings.ToLower(strings.TrimSpace(filter))
	if f == "" {
		f = FilterNeedsAttention
	}
	if f == FilterAwaitingApproval || f == "awaiting-approval" || f == "review" {
		return s.listAwaitingApprovalAttention(ctx, orgID, limit)
	}
	qs := MapCockpitFilterToQueueState(f)
	if qs == "" {
		qs = models.OutreachQueueReplied
	}
	accs, err := s.repo.ListAccounts(ctx, orgID, repository.OutreachAccountFilter{
		QueueState: qs,
		Limit:      limit,
	})
	if err != nil {
		return nil, errx.New(errx.Internal, "failed to list attention: "+err.Error())
	}
	out := make([]AttentionItem, 0, len(accs))
	for i := range accs {
		item, xerr := s.buildAttentionItem(ctx, orgID, &accs[i], false)
		if xerr != nil {
			continue
		}
		out = append(out, *item)
	}
	return out, nil
}

func (s *service) listAwaitingApprovalAttention(ctx context.Context, orgID uuid.UUID, limit int) ([]AttentionItem, *errx.Error) {
	drafts, err := s.repo.ListDrafts(ctx, orgID, models.OutreachDraftNeedsReview, limit, 0)
	if err != nil {
		return nil, errx.New(errx.Internal, "list drafts failed")
	}
	seen := map[uuid.UUID]bool{}
	var out []AttentionItem
	for i := range drafts {
		d := drafts[i]
		if seen[d.AccountID] {
			continue
		}
		// Prefer reply/resume drafts; still include any NEEDS_REVIEW so the queue is useful.
		seen[d.AccountID] = true
		acc, err := s.repo.GetAccount(ctx, orgID, d.AccountID)
		if err != nil || acc == nil {
			continue
		}
		item, xerr := s.buildAttentionItem(ctx, orgID, acc, false)
		if xerr != nil {
			continue
		}
		id := d.ID
		item.ReplyDraftID = &id
		if d.Channel != "" {
			item.Channel = d.Channel
		}
		if d.Subject != "" {
			item.ThreadSubject = d.Subject
		}
		out = append(out, *item)
	}
	return out, nil
}

// GetAttention returns one attention detail row (account + evidence + intent).
func (s *service) GetAttention(ctx context.Context, orgID, accountID uuid.UUID) (*AttentionItem, *errx.Error) {
	if xerr := s.requireEnabled(); xerr != nil {
		return nil, xerr
	}
	acc, err := s.repo.GetAccount(ctx, orgID, accountID)
	if err != nil || acc == nil {
		return nil, errx.New(errx.NotFound, "account not found")
	}
	return s.buildAttentionItem(ctx, orgID, acc, true)
}

// buildAttentionItem assembles cockpit payload. full=true loads evidence.
func (s *service) buildAttentionItem(ctx context.Context, orgID uuid.UUID, acc *models.OutreachAccount, full bool) (*AttentionItem, *errx.Error) {
	if acc == nil {
		return nil, errx.New(errx.NotFound, "account not found")
	}
	item := &AttentionItem{
		AccountID:       acc.ID,
		CompanyName:     firstNonEmpty(acc.NomeFantasia, acc.RazaoSocial),
		CNPJ14:          acc.CNPJ14,
		ServiceCode:     acc.ServiceCode,
		ServiceName:     acc.ServiceName,
		FactToMention:   acc.FactToMention,
		QueueState:      acc.QueueState,
		CommercialState: acc.CommercialState,
		DoNotContact:    acc.DoNotContact,
		Blocked:         acc.Blocked,
		Intent:          acc.CommercialState,
		SuggestedAction: SuggestNextAction(acc.CommercialState, nil, ""),
		UpdatedAt:       acc.UpdatedAt,
	}
	if cands, err := s.repo.ListCandidates(ctx, orgID, acc.ID); err == nil {
		cand := pickRecommended(cands)
		if cand == nil && len(cands) > 0 {
			cand = &cands[0]
		}
		if cand != nil {
			item.ContactName = cand.Name
			item.ContactEmail = cand.Email
			item.ContactPhone = firstNonEmpty(cand.PhoneE164, cand.Phone)
		}
	}
	if full {
		if ev, err := s.repo.ListEvidence(ctx, orgID, acc.ID); err == nil {
			for _, e := range ev {
				item.Evidence = append(item.Evidence, EvidenceBrief{
					ID:             e.SourceEvidenceID,
					Title:          e.Title,
					Excerpt:        truncateRunes(e.Excerpt, 400),
					EpistemicClass: e.EpistemicClass,
					URL:            e.URL,
				})
			}
		}
	}
	// Prefer active NEEDS_REVIEW reply draft if present.
	if drafts, err := s.repo.ListDrafts(ctx, orgID, models.OutreachDraftNeedsReview, 20, 0); err == nil {
		for i := range drafts {
			if drafts[i].AccountID != acc.ID {
				continue
			}
			id := drafts[i].ID
			item.ReplyDraftID = &id
			item.Channel = drafts[i].Channel
			if strings.HasPrefix(drafts[i].StrategyCode, "RESUME_AT:") {
				item.ResumeAt = strings.TrimPrefix(drafts[i].StrategyCode, "RESUME_AT:")
			}
			if strings.Contains(strings.ToLower(drafts[i].StrategyCode), "reply") || containsFlag(drafts[i].RiskFlags, "reply_draft") || containsFlag(drafts[i].RiskFlags, "resume_scheduled") {
				break
			}
		}
	}
	// Hydrate confidence / thread snippet from latest handoff outcome payload.
	email := item.ContactEmail
	if outc, err := s.repo.GetLatestOutcomeForLead(ctx, orgID, acc.CNPJ14, acc.SourceLeadID, email); err == nil && outc != nil {
		if m := parseHandoffPayload(outc.Payload); m != nil {
			if v, ok := m["confidence"].(float64); ok {
				item.Confidence = v
			}
			if v, ok := m["snippet"].(string); ok {
				item.LastSnippet = v
				item.Thread = v
			}
			if v, ok := m["subject"].(string); ok {
				item.ThreadSubject = v
			}
			if v, ok := m["channel"].(string); ok && item.Channel == "" {
				item.Channel = v
			}
			if v, ok := m["intent"].(string); ok && (item.Intent == "" || item.Intent == "NEW") {
				item.Intent = v
				item.SuggestedAction = SuggestNextAction(v, nil, "")
			}
			if v, ok := m["suggested_action"].(string); ok && v != "" {
				item.SuggestedAction = v
			}
		}
	}
	// Parse resume_at from block_reason when present.
	if strings.HasPrefix(acc.BlockReason, "resume_at:") {
		item.ResumeAt = strings.TrimPrefix(acc.BlockReason, "resume_at:")
	}
	if item.Confidence == 0 && item.Intent == IntentDoNotContact {
		item.Confidence = 0.95
	} else if item.Confidence == 0 && item.Intent != "" && item.Intent != IntentUnknown && item.Intent != "NEW" {
		item.Confidence = 0.75
	}
	if item.Channel == "" {
		// Infer from block_reason reply:CHANNEL:INTENT
		if parts := strings.Split(acc.BlockReason, ":"); len(parts) >= 3 && parts[0] == "reply" {
			item.Channel = parts[1]
		} else {
			item.Channel = models.OutreachChannelEmail
		}
	}
	return item, nil
}

func parseHandoffPayload(raw []byte) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	return m
}

// GenerateReplyDraft builds a human-review reply draft (never auto-send).
func (s *service) GenerateReplyDraft(ctx context.Context, orgID, userID, accountID uuid.UUID, contactID *uuid.UUID) (*models.OutreachDraft, *errx.Error) {
	if xerr := s.requireEnabled(); xerr != nil {
		return nil, xerr
	}
	acc, err := s.repo.GetAccount(ctx, orgID, accountID)
	if err != nil || acc == nil {
		return nil, errx.New(errx.NotFound, "account not found")
	}
	if acc.DoNotContact || acc.Blocked {
		return nil, errx.New(errx.BadRequest, "account is blocked or DO_NOT_CONTACT")
	}

	var cand *models.OutreachContactCandidate
	if contactID != nil {
		cand, err = s.repo.GetCandidate(ctx, orgID, *contactID)
		if err != nil || cand == nil || cand.AccountID != accountID {
			return nil, errx.New(errx.NotFound, "contact candidate not found")
		}
	} else {
		list, lerr := s.repo.ListCandidates(ctx, orgID, accountID)
		if lerr != nil {
			return nil, errx.New(errx.Internal, "failed to list contacts")
		}
		cand = pickRecommended(list)
		if cand == nil && len(list) > 0 {
			cand = &list[0]
		}
	}
	if cand == nil {
		return nil, errx.New(errx.BadRequest, "no contact candidate for reply draft")
	}

	evidence, _ := s.repo.ListEvidence(ctx, orgID, accountID)
	intent := acc.CommercialState
	if intent == "" {
		intent = IntentUnknown
	}

	out := buildReplyTemplate(acc, cand, intent)
	if intent == IntentObjection {
		out.BodyText = sanitizeObjectionReply(out.BodyText)
	}
	provider, model := "template", "deterministic_reply"
	usedTemplate := true
	if s.ai != nil {
		// AI may draft; still lands in NEEDS_REVIEW with never_auto_send.
		genOut, p, m, gerr := s.generator().Generate(ctx, GenerateInput{
			Channel:      ChannelEmailInitial,
			Account:      acc,
			Contact:      cand,
			Evidence:     evidence,
			InboundReply: intent,
		})
		if gerr == nil {
			out = genOut
			if intent == IntentObjection {
				out.BodyText = sanitizeObjectionReply(out.BodyText)
			}
			provider, model = p, m
			usedTemplate = false
		}
	}

	draft := &models.OutreachDraft{
		OrganizationID:     orgID,
		AccountID:          accountID,
		ContactCandidateID: &cand.ID,
		Channel:            models.OutreachChannelEmail,
		RecipientName:      cand.Name,
		RecipientRole:      cand.Role,
		RecipientEmail:     cand.Email,
		VerificationStatus: cand.VerificationStatus,
		Status:             models.OutreachDraftNeedsReview,
		PromptVersion:      PromptVersion + ".reply",
		StrategyCode:       "REPLY_" + intent,
		Provider:           provider,
		Model:              model,
	}
	if existing, _ := s.repo.GetActiveDraftForAccount(ctx, orgID, accountID); existing != nil {
		draft.ID = existing.ID
		draft.Generation = existing.Generation + 1
		draft.CreatedAt = existing.CreatedAt
	}

	draft.Subject = SanitizeText(out.Subject, 200)
	draft.BodyText = SanitizeText(out.BodyText, 8000)
	svcCode := ""
	if acc != nil {
		svcCode = acc.ServiceCode
	}
	draft.ServiceCode = SanitizeText(firstNonEmpty(out.ServiceCode, svcCode), 100)
	fact := ""
	if acc != nil {
		fact = acc.FactToMention
	}
	draft.FactUsed = SanitizeText(firstNonEmpty(out.FactUsed, fact), 2000)
	draft.EvidenceIDs = out.EvidenceIDs
	if len(draft.EvidenceIDs) == 0 && len(acc.MomentEvidenceIDs) > 0 {
		draft.EvidenceIDs = acc.MomentEvidenceIDs
	}
	draft.Question = SanitizeText(out.Question, 1000)
	draft.CTA = SanitizeText(out.CTA, 500)

	val := ValidateDraft(&out, acc, cand, ValidateOpts{
		MaxWords: s.cfg.MaxInitialEmailWords,
		Evidence: evidence,
		Channel:  ChannelEmailInitial,
	})
	ok := val.OK
	draft.ValidationOK = &ok
	valJSON, _ := json.Marshal(val)
	draft.ValidationJSON = valJSON
	risk, flags := ClassifyRisk(acc, cand, &out, val)
	flags = appendUniqueFlag(flags, "reply_draft")
	flags = appendUniqueFlag(flags, "never_auto_send")
	if usedTemplate {
		flags = appendUniqueFlag(flags, "template_fallback")
		if risk == "GREEN" {
			risk = "YELLOW"
		}
	}
	if intent == IntentObjection {
		for _, g := range ObjectionReplyGuardrails() {
			flags = appendUniqueFlag(flags, "guardrail:"+g)
		}
	}
	draft.RiskClass = risk
	draft.RiskFlags = flags
	draft.Status = models.OutreachDraftNeedsReview

	if err := s.repo.UpsertDraft(ctx, draft); err != nil {
		return nil, errx.New(errx.Internal, "failed to save reply draft: "+err.Error())
	}
	// Surface under Awaiting approval (NEEDS_REVIEW); do not auto-enroll or send.
	if acc.QueueState != models.OutreachQueueDoNotContact {
		_ = s.repo.SetAccountHumanFlags(ctx, orgID, accountID, acc.Blocked, acc.DoNotContact, acc.BlockReason, models.OutreachQueueNeedsReview)
	}
	if s.audit != nil {
		s.audit.LogAction(ctx, orgID, userID, models.AuditActionCreate, models.AuditEntityOutreachAccount, &accountID, "", "",
			map[string]string{"draft_id": draft.ID.String(), "status": draft.Status, "kind": "reply"},
			map[string]string{"provider": provider, "never_auto_send": "true"},
		)
	}
	return draft, nil
}

func buildReplyTemplate(acc *models.OutreachAccount, cand *models.OutreachContactCandidate, intent string) DraftOutput {
	name := "ola"
	if cand != nil && strings.TrimSpace(cand.Name) != "" {
		parts := strings.Fields(cand.Name)
		name = "ola " + parts[0]
	}
	company := ""
	if acc != nil {
		company = firstNonEmpty(acc.NomeFantasia, acc.RazaoSocial)
	}
	fact := ""
	if acc != nil {
		fact = strings.TrimSpace(acc.FactToMention)
	}
	service := ""
	if acc != nil {
		service = firstNonEmpty(acc.ServiceName, acc.ServiceCode)
	}

	subject := "Re: " + firstNonEmpty(company, "sua mensagem")
	var body string
	switch intent {
	case IntentPositiveInterest:
		body = fmt.Sprintf("%s,\n\nObrigado pela resposta. Posso resumir o contexto de %s e propor um diagnostico objetivo", name, company)
		if fact != "" {
			body += " com base em: " + fact
		}
		body += ".\n\nFaz sentido uma conversa curta esta semana?\n"
	case IntentReferral:
		body = fmt.Sprintf("%s,\n\nObrigado por indicar o interlocutor correto. Vou atualizar o destinatario e retomar com o contato indicado, sem perder o historico da conta %s.\n\nSe puder confirmar o e-mail ou telefone da pessoa, agradeço.\n", name, company)
	case IntentQuestion:
		body = fmt.Sprintf("%s,\n\nObrigado pela pergunta. Respondo com base no dossie de %s", name, company)
		if fact != "" {
			body += " (fato: " + fact + ")"
		}
		if service != "" {
			body += ". Sobre " + service + ", posso detalhar o escopo em uma mensagem curta ou em uma call de 15 min"
		}
		body += ".\n\nPrefere por e-mail ou uma conversa?\n"
	case IntentObjection:
		body = fmt.Sprintf("%s,\n\nEntendi a preocupacao. Nao discuto pontos juridicos aqui e nao invento fatos ausentes do dossie.\n\nPosso esclarecer apenas com base no que ja esta documentado sobre %s", name, company)
		if fact != "" {
			body += ": " + fact
		}
		body += ".\n\nSe quiser, envio um resumo objetivo para sua avaliacao interna.\n"
	case IntentNotNow:
		body = fmt.Sprintf("%s,\n\nSem problema. Posso retomar em uma data que faca sentido para %s, com um unico toque futuro sujeito a aprovacao humana (sem reabrir cadencia automatica).\n\nQual janela prefere?\n", name, company)
	case IntentNegative:
		body = fmt.Sprintf("%s,\n\nObrigado pela clareza. Vou interromper os toques para %s e nao insistirei.\n", name, company)
	case IntentOutOfOffice:
		body = fmt.Sprintf("%s,\n\nRecebi o aviso de ausencia. Nao invento data de retorno; quando estiver de volta, posso retomar com um toque pontual sujeito a aprovacao.\n", name)
	default:
		body = fmt.Sprintf("%s,\n\nObrigado pela mensagem. Vou revisar o contexto de %s com o time e retorno com uma proposta objetiva", name, company)
		if fact != "" {
			body += " ancorada em: " + fact
		}
		body += ".\n"
	}

	svcCode := ""
	if acc != nil {
		svcCode = acc.ServiceCode
	}
	return DraftOutput{
		Subject:     subject,
		BodyText:    body,
		FactUsed:    fact,
		ServiceCode: svcCode,
		Question:    "Faz sentido conversarmos?",
		CTA:         "Posso enviar um resumo objetivo?",
		RiskFlags:   []string{"reply_draft", "never_auto_send"},
	}
}

func sanitizeObjectionReply(body string) string {
	b := body
	// Soft-strip aggressive / invented-legal phrasing from templates or model output.
	banned := []string{
		"voce esta errado",
		"você está errado",
		"isso e ilegal",
		"isso é ilegal",
		"processo judicial",
		"vamos processar",
		"garantimos resultado",
		"dinheiro a receber",
		"obrigatorio contratar",
		"obrigatório contratar",
	}
	lower := strings.ToLower(b)
	for _, phrase := range banned {
		if !strings.Contains(lower, phrase) {
			continue
		}
		parts := strings.Split(b, ".")
		var kept []string
		for _, p := range parts {
			if !strings.Contains(strings.ToLower(p), phrase) {
				kept = append(kept, p)
			}
		}
		b = strings.Join(kept, ".")
		lower = strings.ToLower(b)
	}
	if !strings.Contains(strings.ToLower(b), "nao discuto") && !strings.Contains(strings.ToLower(b), "não discuto") {
		b = strings.TrimSpace(b) + "\n\nNao discuto pontos juridicos e nao invento fatos ausentes do dossie."
	}
	return strings.TrimSpace(b)
}

func containsFlag(flags []string, f string) bool {
	for _, x := range flags {
		if x == f {
			return true
		}
	}
	return false
}
