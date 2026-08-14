package confenge

import (
	"net/mail"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

const pilotContactEvidenceMaxAge = 365 * 24 * time.Hour

type pilotBlock struct {
	Code        string
	Reason      string
	Remediation string
}

type primaryRecipient struct {
	Candidate *models.OutreachContactCandidate
	Warnings  []string
}

type scoredRecipient struct {
	candidate *models.OutreachContactCandidate
	score     int
}

// resolvePilotRecipient selects one persisted recipient only after all pilot gates pass.
func resolvePilotRecipient(candidates []models.OutreachContactCandidate, authoritativeRunID *uuid.UUID, now time.Time) (primaryRecipient, *pilotBlock) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if authoritativeRunID == nil || *authoritativeRunID == uuid.Nil {
		return primaryRecipient{}, &pilotBlock{Code: "recipient_snapshot_missing", Reason: "A conta não possui um snapshot de contatos autoritativo.", Remediation: "Sincronize novamente o feed antes de preparar a mensagem."}
	}
	if len(candidates) == 0 {
		return primaryRecipient{}, &pilotBlock{Code: "recipient_missing", Reason: "Nenhum destinatário foi encontrado para esta conta.", Remediation: "Adicione ao feed um contato corporativo com fonte e data verificáveis."}
	}
	current := make([]models.OutreachContactCandidate, 0, len(candidates))
	for i := range candidates {
		if candidates[i].LastImportRunID != nil && *candidates[i].LastImportRunID == *authoritativeRunID {
			current = append(current, candidates[i])
		}
	}
	if len(current) == 0 {
		return primaryRecipient{}, &pilotBlock{Code: "recipient_removed_current_snapshot", Reason: "Nenhum destinatário permanece no snapshot autoritativo atual.", Remediation: "Resolva um contato no feed atual; não reutilize contatos históricos."}
	}
	groups := make(map[string][]models.OutreachContactCandidate)
	for i := range current {
		key := canonicalPilotEmail(current[i].Email)
		if key == "" {
			key = "invalid:" + current[i].ID.String()
		}
		groups[key] = append(groups[key], current[i])
	}
	valid := make([]scoredRecipient, 0, len(groups))
	blocks := make([]pilotBlock, 0, len(candidates))
	for _, group := range groups {
		groupBlocked := false
		groupValid := make([]scoredRecipient, 0, len(group))
		for i := range group {
			candidate := &group[i]
			block := validatePilotRecipient(candidate, now)
			if block != nil {
				blocks = append(blocks, *block)
				groupBlocked = true
				continue
			}
			candidate.Email = canonicalPilotEmail(candidate.Email)
			groupValid = append(groupValid, scoredRecipient{candidate: candidate, score: pilotRecipientScore(candidate)})
		}
		// Suppression, DNC, bounce, invalid provenance, and tombstones dominate
		// duplicate active rows for the same canonical address.
		if groupBlocked || len(groupValid) == 0 {
			continue
		}
		sortScoredRecipients(groupValid)
		valid = append(valid, groupValid[0])
	}
	if len(valid) == 0 {
		sort.SliceStable(blocks, func(i, j int) bool {
			return pilotBlockPriority(blocks[i].Code) < pilotBlockPriority(blocks[j].Code)
		})
		return primaryRecipient{}, &blocks[0]
	}
	if len(valid) > 1 {
		sortScoredRecipients(valid)
		return primaryRecipient{}, &pilotBlock{Code: "recipient_conflict", Reason: "O snapshot atual contém mais de um endereço comercial válido para a conta.", Remediation: "Marque um único destinatário autoritativo e suprima ou desative os demais."}
	}
	sortScoredRecipients(valid)
	selected := valid[0].candidate
	warnings := make([]string, 0, 1)
	if isGenericRecipient(selected) {
		warnings = append(warnings, "generic_mailbox_allowed_by_policy")
	}
	return primaryRecipient{Candidate: selected, Warnings: warnings}, nil
}

func sortScoredRecipients(values []scoredRecipient) {
	sort.Slice(values, func(i, j int) bool {
		if values[i].score != values[j].score {
			return values[i].score > values[j].score
		}
		if !values[i].candidate.UpdatedAt.Equal(values[j].candidate.UpdatedAt) {
			return values[i].candidate.UpdatedAt.After(values[j].candidate.UpdatedAt)
		}
		return values[i].candidate.ID.String() < values[j].candidate.ID.String()
	})
}

func canonicalPilotEmail(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func validatePilotRecipient(candidate *models.OutreachContactCandidate, now time.Time) *pilotBlock {
	if candidate == nil {
		return &pilotBlock{Code: "recipient_missing", Reason: "Nenhum destinatário foi encontrado para esta conta.", Remediation: "Resolva um contato corporativo antes de preparar a mensagem."}
	}
	if candidate.DoNotContact {
		return &pilotBlock{Code: "recipient_opt_out", Reason: "O destinatário solicitou não receber contato.", Remediation: "Mantenha a supressão e selecione outra conta."}
	}
	if candidate.Bounced {
		return &pilotBlock{Code: "recipient_hard_bounce", Reason: "O destinatário possui hard bounce registrado.", Remediation: "Valide outro endereço corporativo antes de tentar novamente."}
	}
	if candidate.Blocked {
		code := "recipient_suppressed"
		if strings.Contains(strings.ToLower(candidate.BlockReason), "provenance") {
			code = "provenance_tainted"
		}
		return &pilotBlock{Code: code, Reason: "O destinatário está bloqueado pelos guardrails de contato.", Remediation: "Revise a fonte do contato ou selecione outro destinatário validado."}
	}
	if !candidate.CanEnroll() {
		code := "recipient_invalid"
		if isDemoOrFixtureEmail(candidate.Email) {
			code = "recipient_demo_or_fixture"
		}
		return &pilotBlock{Code: code, Reason: "O endereço não atende aos critérios mínimos de destinatário.", Remediation: "Informe um email corporativo real, verificável e não suprimido."}
	}
	if !validExactEmail(candidate.Email) {
		return &pilotBlock{Code: "recipient_invalid", Reason: "O endereço de email é sintaticamente inválido.", Remediation: "Corrija o endereço na fonte autoritativa e sincronize novamente."}
	}
	if isDemoOrFixtureEmail(candidate.Email) {
		return &pilotBlock{Code: "recipient_demo_or_fixture", Reason: "O endereço pertence a domínio demo, fixture ou example.", Remediation: "Substitua por um contato corporativo real e verificável."}
	}
	if !candidate.EmailSendReady {
		return &pilotBlock{Code: "recipient_not_send_ready", Reason: "O destinatário ainda não foi validado para email comercial.", Remediation: "Conclua a validação do destinatário no feed e sincronize novamente."}
	}
	if candidate.MailboxPurposeSendBlocked {
		return &pilotBlock{Code: "generic_mailbox_not_allowed", Reason: "A política atual bloqueia esta caixa genérica ou funcional.", Remediation: "Valide outro destinatário permitido pela política."}
	}
	if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(candidate.RecipientCommercialSuitability)), "UNSUITABLE") {
		return &pilotBlock{Code: "recipient_not_commercially_suitable", Reason: "A fonte marcou o destinatário como inadequado para abordagem comercial.", Remediation: "Resolva outro destinatário com adequação comercial comprovada."}
	}
	if !strings.EqualFold(strings.TrimSpace(candidate.OwnershipStatus), "COMPANY_OWNED") {
		return &pilotBlock{Code: "recipient_domain_unverified", Reason: "Não há comprovação de que a empresa controla o endereço.", Remediation: "Forneça evidência de domínio corporativo compatível com a empresa."}
	}
	if strings.TrimSpace(candidate.SourceURL) == "" && strings.TrimSpace(candidate.SourceDocument) == "" {
		return &pilotBlock{Code: "recipient_provenance_missing", Reason: "O destinatário não possui fonte auditável.", Remediation: "Inclua URL ou documento de origem no feed autoritativo."}
	}
	if candidate.SourceDate == nil {
		return &pilotBlock{Code: "recipient_evidence_date_missing", Reason: "A evidência do destinatário não possui data.", Remediation: "Atualize a fonte com a data de verificação e sincronize novamente."}
	}
	if candidate.SourceDate.After(now.Add(24 * time.Hour)) {
		return &pilotBlock{Code: "recipient_evidence_date_invalid", Reason: "A data da evidência do destinatário está no futuro.", Remediation: "Corrija o timestamp na fonte autoritativa."}
	}
	if now.Sub(candidate.SourceDate.UTC()) > pilotContactEvidenceMaxAge {
		return &pilotBlock{Code: "recipient_evidence_stale", Reason: "A evidência do destinatário expirou.", Remediation: "Revalide o contato em fonte corporativa atual."}
	}
	if candidate.SourceURL != "" && !sourceMatchesEmailDomain(candidate.SourceURL, candidate.Email) {
		return &pilotBlock{Code: "recipient_domain_mismatch", Reason: "O domínio do email não é compatível com a fonte corporativa informada.", Remediation: "Corrija a fonte ou valide outro endereço da empresa."}
	}
	return nil
}

func pilotRecipientScore(candidate *models.OutreachContactCandidate) int {
	score := 0
	if candidate.Recommended {
		score += 100
	}
	switch strings.ToUpper(strings.TrimSpace(candidate.RecipientCommercialSuitability)) {
	case "SUITABLE":
		score += 40
	case "SUITABLE_GENERIC":
		score += 25
	}
	switch strings.ToUpper(strings.TrimSpace(candidate.MailboxPurpose)) {
	case "COMERCIAL", "COMMERCIAL", "SALES", "VENDAS":
		score += 20
	case "GENERIC_CONTACT", "GENERIC":
		score += 10
	case "FINANCEIRO", "FINANCE":
		score -= 10
	}
	if candidate.VerificationStatus == models.OutreachVerifyOfficialSource {
		score += 10
	}
	return score
}

func validExactEmail(value string) bool {
	email := canonicalPilotEmail(value)
	for _, r := range email {
		if r > unicode.MaxASCII || unicode.IsSpace(r) {
			return false
		}
	}
	parsed, err := mail.ParseAddress(email)
	if err != nil || strings.ToLower(parsed.Address) != email {
		return false
	}
	parts := strings.Split(email, "@")
	return len(parts) == 2 && parts[0] != "" && strings.Contains(parts[1], ".")
}

func isDemoOrFixtureEmail(value string) bool {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(value)), "@")
	if len(parts) != 2 {
		return true
	}
	local, domain := parts[0], parts[1]
	return strings.Contains(local, "fixture") || strings.Contains(local, "synthetic") ||
		IsDemoOrFixtureDomain(domain) || domain == "example.com" || domain == "example.org" || domain == "example.net"
}

func sourceMatchesEmailDomain(source, email string) bool {
	parsed, err := url.Parse(strings.TrimSpace(source))
	if err != nil || parsed.Hostname() == "" {
		return false
	}
	parts := strings.Split(strings.ToLower(strings.TrimSpace(email)), "@")
	if len(parts) != 2 {
		return false
	}
	sourceHost := strings.TrimPrefix(strings.ToLower(parsed.Hostname()), "www.")
	emailHost := strings.TrimPrefix(parts[1], "www.")
	return sourceHost == emailHost || strings.HasSuffix(sourceHost, "."+emailHost) || strings.HasSuffix(emailHost, "."+sourceHost)
}

func isGenericMailboxPurpose(value string) bool {
	v := strings.ToUpper(strings.TrimSpace(value))
	return v == "GENERIC" || v == "GENERIC_CONTACT" || v == "UNKNOWN" ||
		v == "ROLE" || v == "ROLE_MAILBOX" || v == "FUNCTIONAL" || v == "INSTITUTIONAL"
}

func isGenericRecipient(candidate *models.OutreachContactCandidate) bool {
	if candidate == nil {
		return false
	}
	if isGenericMailboxPurpose(candidate.MailboxPurpose) {
		return true
	}
	parts := strings.Split(canonicalPilotEmail(candidate.Email), "@")
	if len(parts) != 2 {
		return false
	}
	local := strings.Trim(parts[0], "._-+")
	switch local {
	case "contato", "contact", "info", "comercial", "sales", "vendas", "atendimento", "sac", "financeiro", "administrativo", "licitacao", "licitacoes":
		return true
	}
	return false
}

func pilotBlockPriority(code string) int {
	priority := map[string]int{
		"recipient_opt_out": 1, "recipient_hard_bounce": 2, "provenance_tainted": 3,
		"recipient_suppressed": 4, "recipient_demo_or_fixture": 5, "recipient_invalid": 6,
		"generic_mailbox_not_allowed": 7, "recipient_not_commercially_suitable": 8,
		"recipient_domain_mismatch": 9, "recipient_domain_unverified": 10,
		"recipient_provenance_missing": 11, "recipient_evidence_date_missing": 12,
		"recipient_evidence_stale": 13, "recipient_not_send_ready": 14,
	}
	if value, ok := priority[code]; ok {
		return value
	}
	return 100
}
