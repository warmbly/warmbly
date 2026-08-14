package confenge

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
)

// TouchSummary is optional prior outreach context (no research).
type TouchSummary struct {
	Channel   string `json:"channel"`
	Direction string `json:"direction"`
	Subject   string `json:"subject,omitempty"`
	Snippet   string `json:"snippet,omitempty"`
	At        string `json:"at,omitempty"`
}

// GenerateInput is the full channel-aware dossier for draft generation.
type GenerateInput struct {
	Channel                string
	Account                *models.OutreachAccount
	Contact                *models.OutreachContactCandidate
	Evidence               []models.OutreachEvidence
	Touches                []TouchSummary
	RecentBodies           []string
	ServiceOverrideAudited bool
	PriorSubject           string
	PriorBody              string
	InboundReply           string
	AllowNearDupRegen      bool
}

// DraftGenerator produces structured outreach copy from staging inputs only.
type DraftGenerator interface {
	Generate(ctx context.Context, in GenerateInput) (DraftOutput, string, string, error)
}

// AIDraftGenerator uses generation.Provider.Complete only (no tools / research).
type AIDraftGenerator struct {
	Provider generation.Provider
	Model    string
}

// Generate asks the model for JSON only from structured inputs (no research).
// Strategy is planned first; the model only words within those constraints.
func (g *AIDraftGenerator) Generate(ctx context.Context, in GenerateInput) (DraftOutput, string, string, error) {
	channel := in.Channel
	if channel == "" {
		channel = ChannelEmailInitial
	}
	pb, _ := LoadPlaybook()
	st, plan := BuildOutboundPlan(pb, in.Account, in.Contact, in.Evidence, sequencePosFromChannel(channel, in.Touches))
	if plan.Messageability != MessageabilityReady {
		out := FailClosedDraft(plan, channel)
		out.ServiceOverrideAudited = in.ServiceOverrideAudited
		out.RiskFlags = appendUnique(out.RiskFlags, st.RiskFlags...)
		return out, "ai", "messageability_gate", nil
	}
	if g == nil || g.Provider == nil {
		// Gate already ran; READY + no provider is fail-closed (caller may template-fallback).
		return DraftOutput{}, "ai", "", generation.ErrNotConfigured
	}
	system := draftSystemPromptDoctrine(channel, plan)
	user := draftUserPromptWithPlan(in, plan)
	model := g.Model
	if model == "" {
		model = g.Provider.ModelForTier(true)
	}
	out, usedModel, err := g.completeOnce(ctx, system, user, model)
	if err != nil {
		return DraftOutput{}, g.Provider.Name(), model, err
	}
	out.Channel = channel
	out.ServiceOverrideAudited = in.ServiceOverrideAudited
	if out.ServiceCode == "" {
		out.ServiceCode = plan.ServiceCode
	}
	if out.CTA == "" {
		out.CTA = plan.CTA
	}
	out.RiskFlags = appendUnique(out.RiskFlags, st.RiskFlags...)
	out.RiskFlags = appendUnique(out.RiskFlags, "messageability_ready")

	if in.AllowNearDupRegen && len(in.RecentBodies) > 0 {
		if _, hit := NearDuplicate(out.BodyText, in.RecentBodies); hit {
			user2 := user + "\n" + VaryStructureHint
			out2, usedModel2, err2 := g.completeOnce(ctx, system, user2, model)
			if err2 == nil {
				out2.Channel = channel
				out2.ServiceOverrideAudited = in.ServiceOverrideAudited
				out2.RiskFlags = append(out2.RiskFlags, "near_dup_regenerated")
				out2.RiskFlags = appendUnique(out2.RiskFlags, st.RiskFlags...)
				return out2, g.Provider.Name(), usedModel2, nil
			}
			out.RiskFlags = append(out.RiskFlags, "near_dup_regen_failed")
		}
	}
	return out, g.Provider.Name(), usedModel, nil
}

func sequencePosFromChannel(channel string, touches []TouchSummary) int {
	if channel == ChannelEmailFollowup {
		n := len(touches) + 2
		if n > 5 {
			n = 5
		}
		return n
	}
	return 1
}

func (g *AIDraftGenerator) completeOnce(ctx context.Context, system, user, model string) (DraftOutput, string, error) {
	res, err := g.Provider.Complete(ctx, generation.CompletionRequest{
		System: system, Prompt: user, Model: model, MaxTokens: 1400,
		Temperature: generation.Deterministic(),
	})
	if err != nil {
		return DraftOutput{}, model, err
	}
	out, err := parseDraftJSON(res.Text)
	if err != nil {
		return DraftOutput{}, res.Model, err
	}
	return out, res.Model, nil
}

// TemplateGenerator is the safe fallback when AI is off.
type TemplateGenerator struct{}

func (TemplateGenerator) Generate(ctx context.Context, in GenerateInput) (DraftOutput, string, string, error) {
	_ = ctx
	ch := in.Channel
	if ch == "" {
		ch = ChannelEmailInitial
	}
	pb, _ := LoadPlaybook()
	st, plan := BuildOutboundPlan(pb, in.Account, in.Contact, in.Evidence, sequencePosFromChannel(ch, in.Touches))
	if plan.Messageability != MessageabilityReady {
		out := FailClosedDraft(plan, ch)
		out.ServiceOverrideAudited = in.ServiceOverrideAudited
		out.RiskFlags = appendUnique(out.RiskFlags, st.RiskFlags...)
		return out, "template", "messageability_gate", nil
	}
	out := ComposeFromPlan(plan, in.Account, in.Contact, ch)
	out.RiskFlags = appendUnique(out.RiskFlags, st.RiskFlags...)
	out.ServiceOverrideAudited = in.ServiceOverrideAudited
	if in.AllowNearDupRegen && len(in.RecentBodies) > 0 {
		if _, hit := NearDuplicate(out.BodyText, in.RecentBodies); hit {
			out.BodyText = varyTemplateHook(out.BodyText)
			out.RiskFlags = append(out.RiskFlags, "near_dup_regenerated")
		}
	}
	return out, "template", "deterministic", nil
}

func varyTemplateHook(body string) string {
	const from = "Sou da CONFENGE."
	const to = "Escrevo da CONFENGE."
	if strings.Contains(body, from) {
		return strings.Replace(body, from, to, 1)
	}
	if strings.Contains(body, "Pelo que está público") {
		return strings.Replace(body, "Pelo que está público", "Nas informações públicas", 1)
	}
	if strings.Contains(body, "Escrevo da CONFENGE") {
		return strings.Replace(body, "Escrevo da CONFENGE", "Falo da CONFENGE", 1)
	}
	if strings.HasPrefix(body, "Olá") {
		return strings.Replace(body, "Olá", "Bom dia", 1)
	}
	return body + "\n"
}

func draftSystemPrompt(channel string) string {
	base := `
Você redige mensagens comerciais curtas em português brasileiro para a CONFENGE (engenharia consultiva).
Você NÃO pesquisa e NÃO usa ferramentas. Use somente o PLANO DE MENSAGEM JSON do usuário.
Não transforme hipótese em fato. Hipótese de estrutura interna só como pergunta consultiva cuidadosa (nunca "sei que vocês não têm equipe").
Não invente número, contrato, data, órgão, nome ou cargo ausentes dos inputs.
Não use travessões. Não use frases de IA nem saudações clichê.
Não prometa resultado, crédito, dinheiro a receber, irregularidade ou urgência falsa.
Não diga "identificamos uma oportunidade" sem citar um fato concreto do dossiê.
Um serviço apenas, alinhado ao service_code do dossiê.
Cada claim em claims[] deve ter phrase/fact e evidence_ids que existam no dossiê.
rationale é interno (operador); nunca o inclua no body_text.
Responda APENAS com JSON válido no schema:
{"channel":"","subject":"","body_text":"","body_html":"","followups":[{"delay_days":3,"subject_mode":"same_thread","body_text":"","body_html":""}],"fact_used":"","evidence_ids":[],"claims":[{"phrase":"","fact":"","evidence_ids":[]}],"service_code":"","question":"","cta":"","risk_flags":[],"rationale":""}
`
	switch channel {
	case ChannelEmailFollowup:
		base += "\nCanal EMAIL_FOLLOWUP: mais curto que o inicial, mesmo fio, sem repetir o primeiro e-mail inteiro.\n"
	case ChannelWhatsAppInitial, ChannelWhatsAppContinuation:
		base += "\nCanal WHATSAPP: muito mais curto (~25-70 palavras). subject vazio. Uma referência, uma pergunta, sem colar o e-mail.\n"
	case ChannelReplyDraft:
		base += "\nCanal REPLY_DRAFT: resposta a lead que respondeu. Agradeça, 1 fato se houver, 1 pergunta e CTA. Não reenvie o pitch completo.\n"
	default:
		base += "\nCanal EMAIL_INITIAL: preferencialmente 70-140 palavras, 1-2 fatos, 1 serviço, 1 pergunta, 1 CTA. Sem parecer dossiê despejado.\n"
	}
	return strings.TrimSpace(base)
}

func draftUserPrompt(in GenerateInput) string {
	type payload struct {
		Channel                string           `json:"channel"`
		Company                any              `json:"company"`
		Contact                any              `json:"contact"`
		Moment                 any              `json:"moment"`
		WhyNow                 string           `json:"why_now"`
		Offer                  any              `json:"offer"`
		Messaging              any              `json:"messaging"`
		Evidence               any              `json:"evidence"`
		InternalHypothesis     []map[string]any `json:"internal_structure_hypothesis"`
		ClaimsToAvoid          []string         `json:"claims_to_avoid"`
		Touches                []TouchSummary   `json:"touch_history"`
		PriorSubject           string           `json:"prior_subject,omitempty"`
		PriorBody              string           `json:"prior_body,omitempty"`
		InboundReply           string           `json:"inbound_reply,omitempty"`
		ServiceOverrideAudited bool             `json:"service_override_audited"`
	}
	acc, cand, evidence := in.Account, in.Contact, in.Evidence
	company, moment, offer, messaging := map[string]any{}, map[string]any{}, map[string]any{}, map[string]any{}
	whyNow := ""
	claimsAvoid := []string{}
	var hypothesis []map[string]any
	if acc != nil {
		company = map[string]any{"razao_social": acc.RazaoSocial, "nome_fantasia": acc.NomeFantasia, "cnpj14": acc.CNPJ14, "municipio": acc.Municipio, "uf": acc.UF}
		moment = map[string]any{"code": acc.MomentCode, "summary": acc.MomentSummary, "confidence": acc.MomentConfidence, "evidence_ids": acc.MomentEvidenceIDs}
		whyNow = acc.MomentSummary
		offer = map[string]any{"service_code": acc.ServiceCode, "service_name": acc.ServiceName, "entry_offer": acc.EntryOffer, "rationale": acc.OfferRationale}
		messaging = map[string]any{"fact_to_mention": acc.FactToMention, "question_to_ask": acc.QuestionToAsk, "cta": acc.CTA, "claims_to_avoid": acc.ClaimsToAvoid}
		claimsAvoid = acc.ClaimsToAvoid
	}
	contact := map[string]any{}
	if cand != nil {
		contact = map[string]any{
			"name": cand.Name, "role": cand.Role, "email": cand.Email,
			"verification_status": cand.VerificationStatus, "generic_recipient": false,
		}
		if isGenericRecipient(cand) {
			contact["name"] = ""
			contact["role"] = ""
			contact["generic_recipient"] = true
		}
	}
	ev := make([]map[string]any, 0, len(evidence))
	for _, e := range evidence {
		ev = append(ev, map[string]any{"id": e.SourceEvidenceID, "title": e.Title, "synthesis": e.Synthesis, "excerpt": e.Excerpt, "epistemic_class": e.EpistemicClass, "url": e.URL})
		switch e.EpistemicClass {
		case models.OutreachEpistemicCommercialHypothesis, models.OutreachEpistemicWeakInference, models.OutreachEpistemicRequiresCompanyConfirm:
			hypothesis = append(hypothesis, map[string]any{"id": e.SourceEvidenceID, "synthesis": e.Synthesis, "epistemic_class": e.EpistemicClass, "usage": "only_as_careful_question"})
		}
	}
	ch := in.Channel
	if ch == "" {
		ch = ChannelEmailInitial
	}
	touches := in.Touches
	if touches == nil {
		touches = []TouchSummary{}
	}
	b, _ := json.MarshalIndent(payload{
		Channel: ch, Company: company, Contact: contact, Moment: moment, WhyNow: whyNow,
		Offer: offer, Messaging: messaging, Evidence: ev, InternalHypothesis: hypothesis,
		ClaimsToAvoid: claimsAvoid, Touches: touches, PriorSubject: in.PriorSubject,
		PriorBody: in.PriorBody, InboundReply: in.InboundReply, ServiceOverrideAudited: in.ServiceOverrideAudited,
	}, "", "  ")
	return "Gere a mensagem com base apenas nestes dados (sem pesquisa):\n" + string(b)
}

func parseDraftJSON(text string) (DraftOutput, error) {
	text = strings.TrimSpace(text)
	if i := strings.Index(text, "{"); i >= 0 {
		if j := strings.LastIndex(text, "}"); j > i {
			text = text[i : j+1]
		}
	}
	var out DraftOutput
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		return DraftOutput{}, fmt.Errorf("invalid draft JSON: %w", err)
	}
	out.Subject = strings.TrimSpace(out.Subject)
	out.BodyText = strings.TrimSpace(out.BodyText)
	out.FactUsed = strings.TrimSpace(out.FactUsed)
	out.Rationale = strings.TrimSpace(out.Rationale)
	out.Channel = strings.TrimSpace(out.Channel)
	if len(out.EvidenceIDs) == 0 {
		out.EvidenceIDs = collectEvidenceIDs(&out)
	}
	if len(out.Claims) == 0 && out.FactUsed != "" {
		out.Claims = claimsFromFact(out.FactUsed, out.EvidenceIDs)
	}
	return out, nil
}

// BuildGenerateInput assembles GenerateInput from staging models.
func BuildGenerateInput(channel string, acc *models.OutreachAccount, cand *models.OutreachContactCandidate, evidence []models.OutreachEvidence, recent []string) GenerateInput {
	if channel == "" {
		channel = ChannelEmailInitial
	}
	return GenerateInput{
		Channel: channel, Account: acc, Contact: cand, Evidence: evidence,
		RecentBodies: recent, AllowNearDupRegen: true,
	}
}
