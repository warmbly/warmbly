package confenge

import (
	"strings"
	"time"

	"github.com/warmbly/warmbly/internal/models"
)

// Recipient terminal states. Every target ends in exactly one of these.
const (
	RecipientValidated = "VALIDATED"
	RecipientException = "EXCEPTION"
	RecipientBlocked   = "BLOCKED"
)

// RecipientPolicyVersion stamps the identity/provenance policy.
const RecipientPolicyVersion = "confenge.recipient.v1"

// RecipientResolution is the durable, auditable outcome of identity checks.
type RecipientResolution struct {
	State              string                   `json:"state"`
	PolicyVersion      string                   `json:"policy_version"`
	ValidatedAt        time.Time                `json:"validated_at"`
	CanonicalTargetID  string                   `json:"canonical_target_id,omitempty"`
	CanonicalContactID string                   `json:"canonical_contact_id,omitempty"`
	Company            string                   `json:"company,omitempty"`
	CNPJ14             string                   `json:"cnpj14,omitempty"`
	Email              string                   `json:"email,omitempty"`
	Name               string                   `json:"name,omitempty"`
	Role               string                   `json:"role,omitempty"`
	Domain             string                   `json:"domain,omitempty"`
	SourceURL          string                   `json:"source_url,omitempty"`
	SourceDocument     string                   `json:"source_document,omitempty"`
	EvidenceDate       *time.Time               `json:"evidence_date,omitempty"`
	Provenance         string                   `json:"provenance,omitempty"`
	Confidence         string                   `json:"confidence,omitempty"`
	Suppressed         bool                     `json:"suppressed"`
	Bounced            bool                     `json:"bounced"`
	OptOut             bool                     `json:"opt_out"`
	DNC                bool                     `json:"dnc"`
	Suitability        string                   `json:"suitability,omitempty"`
	Reason             string                   `json:"reason,omitempty"`
	ReasonCodes        []string                 `json:"reason_codes,omitempty"`
	NextAction         string                   `json:"next_action,omitempty"`
	HumanDecision      string                   `json:"human_decision,omitempty"`
	Candidates         []RecipientCandidateView `json:"candidates,omitempty"`
}

// RecipientCandidateView is an operator-facing candidate (no invented fields).
type RecipientCandidateView struct {
	CanonicalContactID string `json:"canonical_contact_id,omitempty"`
	Email              string `json:"email,omitempty"`
	Name               string `json:"name,omitempty"`
	Role               string `json:"role,omitempty"`
	Generic            bool   `json:"generic"`
	Confidence         string `json:"confidence,omitempty"`
	BlockCode          string `json:"block_code,omitempty"`
}

// ResolveRecipient classifies one account's current candidates. It never
// invents name, role, ownership, relation, personal email, consent, or evidence.
func ResolveRecipient(acc *models.OutreachAccount, candidates []models.OutreachContactCandidate, now time.Time) RecipientResolution {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	res := RecipientResolution{
		PolicyVersion: RecipientPolicyVersion,
		ValidatedAt:   now,
	}
	if acc != nil {
		res.CanonicalTargetID = strings.TrimSpace(acc.SourceLeadID)
		res.Company = firstNonEmpty(acc.NomeFantasia, acc.RazaoSocial)
		res.CNPJ14 = acc.CNPJ14
		res.DNC = acc.DoNotContact
		if acc.Blocked {
			res.Suppressed = true
		}
	}

	add := func(code, reason, next, human string) {
		res.ReasonCodes = appendUnique(res.ReasonCodes, code)
		if res.Reason == "" && reason != "" {
			res.Reason = reason
		}
		if res.NextAction == "" && next != "" {
			res.NextAction = next
		}
		if res.HumanDecision == "" && human != "" {
			res.HumanDecision = human
		}
	}

	if acc != nil && acc.DoNotContact {
		res.State = RecipientBlocked
		add("account_dnc", "A conta está marcada como não contatar.",
			"Mantenha a supressão.", "")
		return res
	}
	if acc != nil && acc.Blocked {
		res.State = RecipientBlocked
		add("account_blocked", "A conta está bloqueada.",
			"Revise o bloqueio antes de qualquer abordagem.", "")
		return res
	}

	if len(candidates) == 0 {
		res.State = RecipientBlocked
		add("recipient_missing",
			"Nenhum destinatário foi publicado para esta conta.",
			"Publique no extra-cli um contato corporativo nomeado, com email, fonte e data.",
			"")
		return res
	}

	views := make([]RecipientCandidateView, 0, len(candidates))
	var validated []models.OutreachContactCandidate
	var exceptions []models.OutreachContactCandidate
	var lastBlock *pilotBlock
	for i := range candidates {
		c := candidates[i]
		view := RecipientCandidateView{
			CanonicalContactID: strings.TrimSpace(c.SourceContactID),
			Email:              strings.TrimSpace(c.Email),
			Confidence:         c.Confidence,
			Generic:            isGenericRecipient(&c),
		}
		// Only surface name/role when they are actually present (never invent).
		if provenPersonName(&c) {
			view.Name = strings.TrimSpace(c.Name)
		}
		if provenRole(&c) {
			view.Role = strings.TrimSpace(c.Role)
		}
		if blk := classifyRecipientCandidate(acc, &c, now); blk != nil {
			view.BlockCode = blk.Code
			lastBlock = blk
			if blk.Code == "generic_mailbox" || blk.Code == "recipient_conflict_identity" ||
				blk.Code == "role_unproven" || blk.Code == "name_unproven" ||
				blk.Code == "suitability_ambiguous" || blk.Code == "source_conflict" ||
				blk.Code == "recipient_evidence_stale" || blk.Code == "ownership_uncertain" ||
				blk.Code == "named_human_manual_channel" || blk.Code == "recipient_not_send_ready" ||
				blk.Code == "role_mailbox" {
				exceptions = append(exceptions, c)
			}
		} else {
			validated = append(validated, c)
		}
		views = append(views, view)
	}
	res.Candidates = views

	if len(validated) > 1 {
		res.State = RecipientException
		add("recipient_conflict",
			"Há mais de um destinatário humano plausível.",
			"Escolha o destinatário autoritativo e suprima os demais.",
			"Qual pessoa deve receber o primeiro contato?")
		return res
	}
	if len(validated) == 1 {
		return fillValidated(res, acc, &validated[0], now)
	}

	// No VALIDATED candidate. Prefer EXCEPTION for honest human ambiguity.
	if len(exceptions) > 0 {
		res.State = RecipientException
		primary := &exceptions[0]
		copyKnownIdentity(&res, primary)
		if lastBlock != nil && containsStr(res.ReasonCodes, lastBlock.Code) == false {
			add(lastBlock.Code, lastBlock.Reason, lastBlock.Remediation, humanDecisionFor(lastBlock.Code))
		}
		if res.Reason == "" {
			add("recipient_exception",
				"O dossiê não prova uma pessoa comercial única.",
				"Publique identidade nomeada no extra-cli ou decida a exceção.",
				"Quem é o destinatário comercial desta conta?")
		}
		if res.HumanDecision == "" {
			res.HumanDecision = "Quem é o destinatário comercial desta conta?"
		}
		return res
	}

	res.State = RecipientBlocked
	if lastBlock != nil {
		add(lastBlock.Code, lastBlock.Reason, lastBlock.Remediation, "")
	} else {
		add("recipient_blocked",
			"Não há fundamento suficiente para um destinatário comercial.",
			"Publique evidência de contato no extra-cli.",
			"")
	}
	return res
}

func fillValidated(res RecipientResolution, acc *models.OutreachAccount, c *models.OutreachContactCandidate, now time.Time) RecipientResolution {
	res.State = RecipientValidated
	res.CanonicalContactID = strings.TrimSpace(c.SourceContactID)
	res.Email = canonicalPilotEmail(c.Email)
	res.Name = strings.TrimSpace(c.Name)
	res.Role = strings.TrimSpace(c.Role)
	res.Domain = emailDomain(c.Email)
	res.SourceURL = strings.TrimSpace(c.SourceURL)
	res.SourceDocument = strings.TrimSpace(c.SourceDocument)
	res.EvidenceDate = c.SourceDate
	res.Provenance = firstNonEmpty(c.SourceURL, c.SourceDocument, c.VerificationStatus)
	res.Confidence = c.Confidence
	res.Suitability = c.RecipientCommercialSuitability
	res.Suppressed = c.Blocked
	res.Bounced = c.Bounced
	res.OptOut = c.DoNotContact
	res.DNC = c.DoNotContact || (acc != nil && acc.DoNotContact)
	res.ValidatedAt = now
	return res
}

func copyKnownIdentity(res *RecipientResolution, c *models.OutreachContactCandidate) {
	if c == nil {
		return
	}
	res.CanonicalContactID = strings.TrimSpace(c.SourceContactID)
	res.Email = strings.TrimSpace(c.Email)
	res.Domain = emailDomain(c.Email)
	if provenPersonName(c) {
		res.Name = strings.TrimSpace(c.Name)
	}
	if provenRole(c) {
		res.Role = strings.TrimSpace(c.Role)
	}
	res.SourceURL = strings.TrimSpace(c.SourceURL)
	res.SourceDocument = strings.TrimSpace(c.SourceDocument)
	res.EvidenceDate = c.SourceDate
	res.Provenance = firstNonEmpty(c.SourceURL, c.SourceDocument, c.VerificationStatus)
	res.Confidence = c.Confidence
	res.Suitability = c.RecipientCommercialSuitability
	res.Suppressed = c.Blocked
	res.Bounced = c.Bounced
	res.OptOut = c.DoNotContact
	res.DNC = c.DoNotContact
}

func classifyRecipientCandidate(acc *models.OutreachAccount, c *models.OutreachContactCandidate, now time.Time) *pilotBlock {
	if c == nil {
		return &pilotBlock{Code: "recipient_missing", Reason: "Candidato ausente.", Remediation: "Publique um contato no extra-cli."}
	}
	if c.DoNotContact {
		return &pilotBlock{Code: "recipient_opt_out", Reason: "O destinatário solicitou não receber contato.", Remediation: "Mantenha a supressão."}
	}
	if c.Bounced {
		return &pilotBlock{Code: "recipient_hard_bounce", Reason: "O destinatário possui hard bounce registrado.", Remediation: "Valide outro endereço corporativo."}
	}
	if c.Blocked {
		code := "recipient_suppressed"
		if strings.Contains(strings.ToLower(c.BlockReason), "provenance") {
			code = "provenance_tainted"
		}
		return &pilotBlock{Code: code, Reason: "O destinatário está bloqueado.", Remediation: "Revise a fonte ou escolha outro contato."}
	}
	if strings.TrimSpace(c.Email) == "" {
		if provenPersonName(c) && provenRole(c) && !isGenericRecipient(c) {
			return &pilotBlock{
				Code:        "named_human_manual_channel",
				Reason:      "Pessoa nomeada sem e-mail direto validado.",
				Remediation: "Abordar no canal manual publicado. Não promover para envio automático.",
			}
		}
		return &pilotBlock{Code: "recipient_missing", Reason: "Não há email publicado para este candidato.", Remediation: "Publique um email corporativo com fonte e data."}
	}
	if isRoleMailbox(c) && !provenPersonName(c) {
		return &pilotBlock{
			Code:        "role_mailbox",
			Reason:      "A caixa é funcional oficial; não prova uma pessoa.",
			Remediation: "Decida a exceção manualmente. Não aprovar como humano validado.",
		}
	}
	if blk := validatePilotRecipient(c, now); blk != nil {
		// Remap generic policy-allowed into EXCEPTION, never VALIDATED.
		if blk.Code == "generic_mailbox_not_allowed" || isGenericRecipient(c) {
			return &pilotBlock{
				Code:        "generic_mailbox",
				Reason:      "A caixa é genérica ou funcional; não prova uma pessoa.",
				Remediation: "Publique um destinatário humano nomeado. Não promover caixa genérica.",
			}
		}
		return blk
	}
	if isGenericRecipient(c) {
		return &pilotBlock{
			Code:        "generic_mailbox",
			Reason:      "A caixa é genérica ou funcional; não prova uma pessoa.",
			Remediation: "Publique um destinatário humano nomeado. Não promover caixa genérica.",
		}
	}
	if !provenPersonName(c) {
		return &pilotBlock{
			Code:        "name_unproven",
			Reason:      "O nome da pessoa não está comprovado na evidência.",
			Remediation: "Publique o nome com fonte corporativa. Não inventar.",
		}
	}
	if !provenRole(c) {
		return &pilotBlock{
			Code:        "role_unproven",
			Reason:      "A função da pessoa não está comprovada na evidência.",
			Remediation: "Publique o cargo com fonte corporativa. Não inventar.",
		}
	}
	suit := strings.ToUpper(strings.TrimSpace(c.RecipientCommercialSuitability))
	switch suit {
	case "", "UNKNOWN", "SUITABLE_GENERIC", "AMBIGUOUS":
		return &pilotBlock{
			Code:        "suitability_ambiguous",
			Reason:      "A adequação comercial do destinatário é ambígua.",
			Remediation: "Confirme suitability no extra-cli ou decida a exceção.",
		}
	}
	if acc != nil && c.SourceDate == nil && acc.MomentObservedAt == nil {
		// date already required by validatePilotRecipient
	}
	_ = acc
	return nil
}

func provenPersonName(c *models.OutreachContactCandidate) bool {
	if c == nil {
		return false
	}
	name := strings.TrimSpace(c.Name)
	if name == "" {
		return false
	}
	low := foldASCII(name)
	switch low {
	case "contato", "equipe", "fulano", "pessoa historica", "pessoa histórica", "n/a", "na", "desconhecido", "unknown":
		return false
	}
	if isGenericRecipient(c) {
		return false
	}
	return true
}

func provenRole(c *models.OutreachContactCandidate) bool {
	if c == nil {
		return false
	}
	role := strings.TrimSpace(c.Role)
	if role == "" {
		return false
	}
	low := foldASCII(role)
	switch low {
	case "desconhecido", "unknown", "n/a", "na", "contato", "equipe":
		return false
	}
	return true
}

func emailDomain(email string) string {
	email = canonicalPilotEmail(email)
	i := strings.LastIndex(email, "@")
	if i < 0 || i+1 >= len(email) {
		return ""
	}
	return email[i+1:]
}

func humanDecisionFor(code string) string {
	switch code {
	case "generic_mailbox":
		return "Esta caixa genérica deve ser usada mesmo sem pessoa nomeada? (padrão: não)"
	case "recipient_conflict", "recipient_conflict_identity":
		return "Qual candidato é o destinatário autoritativo?"
	case "role_unproven", "name_unproven", "ownership_uncertain":
		return "A identidade publicada está correta e comprovada?"
	case "suitability_ambiguous":
		return "Este contato é adequado para abordagem comercial?"
	case "recipient_evidence_stale":
		return "A evidência envelhecida ainda vale, ou precisa revalidação?"
	default:
		return "O que falta para um destinatário comercial comprovado?"
	}
}
