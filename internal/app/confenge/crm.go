package confenge

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/replyclassify"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// Default CRM pipeline name (idempotent bootstrap).
const DefaultPipelineName = "CONFENGE Comercial"

// CRMAPI is the slice of CRM service used for pipeline/task/deal bootstrap.
type CRMAPI interface {
	ListPipelines(ctx context.Context, orgID uuid.UUID) ([]models.Pipeline, *errx.Error)
	CreatePipeline(ctx context.Context, orgID uuid.UUID, data *models.CreatePipeline) (*models.Pipeline, *errx.Error)
	CreateCRMTask(ctx context.Context, orgID, userID uuid.UUID, data *models.CreateCRMTask) (*models.CRMTask, *errx.Error)
	CreateDeal(ctx context.Context, orgID uuid.UUID, data *models.CreateDeal) (*models.Deal, *errx.Error)
	// Optional activity recording via underlying repo is not on the service
	// surface; tasks/deals create their own activity trails.
}

// WireCRM attaches the CRM control-plane service.
func (s *service) WireCRM(crm CRMAPI) {
	s.crm = crm
}

// confengeStages is the fixed stage list. Re-bootstrap never renames human edits
// when a pipeline with the same name already exists.
func confengeStages() []models.CreatePipelineStage {
	// Colors match dashboard slate/sky/amber/emerald conventions.
	return []models.CreatePipelineStage{
		{Name: "Novo", Color: "#94a3b8"},
		{Name: "Preparação", Color: "#64748b"},
		{Name: "Contato aprovado", Color: "#0ea5e9"},
		{Name: "Contatado", Color: "#0284c7"},
		{Name: "Respondeu", Color: "#8b5cf6"},
		{Name: "Reunião", Color: "#f59e0b"},
		{Name: "Diagnóstico", Color: "#d97706"},
		{Name: "Proposta", Color: "#ea580c"},
		{Name: "Negociação", Color: "#dc2626"},
		{Name: "Ganho", Color: "#16a34a"},
		{Name: "Perdido", Color: "#6b7280"},
		{Name: "Não contatar", Color: "#1f2937"},
	}
}

// BootstrapPipeline finds or creates the CONFENGE Comercial pipeline.
// Idempotent: existing pipeline by name is returned unchanged (preserves human edits).
func (s *service) BootstrapPipeline(ctx context.Context, orgID uuid.UUID) (*models.Pipeline, *errx.Error) {
	if xerr := s.requireEnabled(); xerr != nil {
		return nil, xerr
	}
	if s.crm == nil {
		return nil, errx.New(errx.ServiceUnavailable, "CRM service not wired")
	}
	list, xerr := s.crm.ListPipelines(ctx, orgID)
	if xerr != nil {
		return nil, xerr
	}
	for i := range list {
		if strings.EqualFold(list[i].Name, DefaultPipelineName) {
			return &list[i], nil
		}
	}
	created, xerr := s.crm.CreatePipeline(ctx, orgID, &models.CreatePipeline{
		Name:   DefaultPipelineName,
		Stages: confengeStages(),
	})
	if xerr != nil {
		return nil, xerr
	}
	// Persist pointer on org settings when available.
	if settings, err := s.repo.GetOrgSettings(ctx, orgID); err == nil {
		if settings == nil {
			settings = &models.OutreachOrgSettings{OrganizationID: orgID, CampaignName: DefaultCampaignName}
		}
		// reuse campaign_name field area; store pipeline id in raw via upsert only if we add column
		// For now pipeline is found by name — no extra column required.
		_ = settings
	}
	return created, nil
}

// MapReplyClass converts replyclassify / confenge commercial classes into
// outcome event type + optional CRM side effects (task/deal). Never auto-WON.
type ReplyCRMAction struct {
	OutcomeType string
	CreateTask  bool
	TaskTitle   string
	TaskType    string
	OpenDeal    bool // only on explicit positive interest path when contact known
	SuppressDNC bool
	QueueState  string
}

// ClassifyReplyForCRM maps classifier/commercial labels to CRM actions.
// replyClass is replyclassify class or confenge commercial class.
func ClassifyReplyForCRM(replyClass string) ReplyCRMAction {
	switch strings.ToLower(strings.TrimSpace(replyClass)) {
	case replyclassify.ClassPositive, "positive_interest":
		return ReplyCRMAction{
			OutcomeType: OutcomeReplied,
			CreateTask:  true,
			TaskTitle:   "CONFENGE: interesse positivo - acompanhar",
			TaskType:    "call",
			OpenDeal:    true,
			QueueState:  models.OutreachQueueReplied,
		}
	case replyclassify.ClassNeutral:
		return ReplyCRMAction{OutcomeType: OutcomeReplied, CreateTask: false, QueueState: models.OutreachQueueReplied}
	case "referral":
		return ReplyCRMAction{
			OutcomeType: OutcomeReplied,
			CreateTask:  true,
			TaskTitle:   "CONFENGE: encaminhamento - cadastrar contato indicado",
			TaskType:    "email",
			QueueState:  models.OutreachQueueReplied,
		}
	case "wrong_contact":
		return ReplyCRMAction{
			OutcomeType: OutcomeReplied,
			CreateTask:  true,
			TaskTitle:   "CONFENGE: contato errado - achar interlocutor",
			TaskType:    "email",
			QueueState:  models.OutreachQueueNeedsContact,
		}
	case "not_now":
		return ReplyCRMAction{
			OutcomeType: OutcomeReplied,
			CreateTask:  true,
			TaskTitle:   "CONFENGE: nao agora - follow-up futuro",
			TaskType:    "call",
			QueueState:  models.OutreachQueueReplied,
		}
	case replyclassify.ClassNegative, "no_interest":
		return ReplyCRMAction{OutcomeType: OutcomeReplied, QueueState: models.OutreachQueueSkipped}
	case replyclassify.ClassUnsubscribe, "do_not_contact", "dnc":
		return ReplyCRMAction{
			OutcomeType: OutcomeDoNotContact,
			SuppressDNC: true,
			QueueState:  models.OutreachQueueDoNotContact,
		}
	case replyclassify.ClassOutOfOffice, "ooo":
		return ReplyCRMAction{OutcomeType: OutcomeReplied, QueueState: ""} // no queue change
	case replyclassify.ClassAutoReply, "automated_reply", "bounce":
		// auto_reply covers OOO-adjacent headers; hard bounces use NoteBounce
		return ReplyCRMAction{OutcomeType: "", QueueState: ""}
	case "meeting":
		return ReplyCRMAction{
			OutcomeType: OutcomeMeeting,
			CreateTask:  true,
			TaskTitle:   "CONFENGE: reuniao marcada - preparar",
			TaskType:    "meeting",
			QueueState:  models.OutreachQueueMeeting,
		}
	case "proposal":
		return ReplyCRMAction{
			OutcomeType: OutcomeProposal,
			CreateTask:  true,
			TaskTitle:   "CONFENGE: proposta - acompanhar",
			TaskType:    "email",
			QueueState:  models.OutreachQueueProposal,
		}
	case "won", "ganho":
		return ReplyCRMAction{OutcomeType: OutcomeReplied, CreateTask: true, TaskTitle: "CONFENGE: interesse positivo - acompanhar (humano marca Ganho)", TaskType: "call", OpenDeal: true, QueueState: models.OutreachQueueReplied}
	case "question", "objection", "referral_to_other_person":
		// commercial intents
		title := "CONFENGE: resposta - acompanhar"
		if strings.ToLower(strings.TrimSpace(replyClass)) == "question" {
			title = "CONFENGE: pergunta do lead - responder com fatos do dossie"
		} else if strings.ToLower(strings.TrimSpace(replyClass)) == "objection" {
			title = "CONFENGE: objecao - responder com fatos (sem discutir juridicamente)"
		} else {
			title = "CONFENGE: encaminhamento - cadastrar contato indicado"
		}
		return ReplyCRMAction{OutcomeType: OutcomeReplied, CreateTask: true, TaskTitle: title, TaskType: "email", QueueState: models.OutreachQueueReplied}
	default:
		return ReplyCRMAction{OutcomeType: OutcomeReplied, QueueState: models.OutreachQueueReplied}
	}
}

// HandleClassifiedReply routes through unified handoff. Never auto-WON.
// Legacy entry without body — prefer HandleClassifiedReplyFull / OnClassifiedReply.
func (s *service) HandleClassifiedReply(ctx context.Context, orgID, actorID uuid.UUID, contactEmail, replyClass string, warmblyContactID *uuid.UUID) *errx.Error {
	return s.HandleClassifiedReplyFull(ctx, orgID, actorID, contactEmail, replyClass, warmblyContactID, "", "", nil)
}

// HandleClassifiedReplyFull includes subject/body so commercial lexicon (DNC, referral, OOO) runs.
func (s *service) HandleClassifiedReplyFull(ctx context.Context, orgID, actorID uuid.UUID, contactEmail, replyClass string, warmblyContactID *uuid.UUID, subject, bodyText string, headers map[string][]string) *errx.Error {
	if xerr := s.requireEnabled(); xerr != nil {
		return nil
	}
	email := strings.TrimSpace(strings.ToLower(contactEmail))
	if email == "" {
		return nil
	}
	idemExtra := replyClass
	if bodyText != "" {
		// Stable-ish key for content-bearing handoffs (minute bucket still limits spam).
		idemExtra = replyClass + ":" + fmt.Sprintf("%d", time.Now().UTC().Truncate(time.Minute).Unix())
	}
	_, xerr := s.ProcessInboundHandoff(ctx, orgID, InboundHandoff{
		Channel: models.OutreachChannelEmail, ContactEmail: email, PreClass: replyClass,
		Subject: subject, BodyText: bodyText, Headers: headers,
		IdempotencyKey:   fmt.Sprintf("classified:%s:%s:%s", orgID, email, idemExtra),
		WarmblyContactID: warmblyContactID, ActorID: actorID, OccurredAt: time.Now().UTC(),
	})
	return xerr
}

// applyReplyCRM creates tasks/deals. Never Ganho.
// When actorID is Nil (system inbound), resolves org owner so tasks still get created.
func (s *service) applyReplyCRM(ctx context.Context, orgID, actorID uuid.UUID, contactEmail, replyClass string, warmblyContactID *uuid.UUID, cand *models.OutreachContactCandidate, acc *models.OutreachAccount) {
	if s.crm == nil {
		return
	}
	action := ClassifyReplyForCRM(replyClass)
	contactID := warmblyContactID
	if contactID == nil && cand != nil {
		contactID = cand.WarmblyContactID
	}
	if contactID == nil {
		return
	}
	actor := actorID
	if actor == uuid.Nil {
		if id, err := s.repo.GetOrgOwnerUserID(ctx, orgID); err == nil && id != uuid.Nil {
			actor = id
		}
	}
	if action.CreateTask && actor != uuid.Nil {
		_, _ = s.crm.CreateCRMTask(ctx, orgID, actor, &models.CreateCRMTask{
			ContactID: contactID, Title: action.TaskTitle, Type: action.TaskType, Priority: "medium",
		})
	}
	if action.OpenDeal {
		pipe, xerr := s.BootstrapPipeline(ctx, orgID)
		if xerr == nil && pipe != nil {
			stageID := stageIDByName(pipe, "Respondeu")
			if stageID != uuid.Nil {
				name := "CONFENGE"
				if acc != nil {
					name = firstNonEmpty(acc.NomeFantasia, acc.RazaoSocial, "CONFENGE")
				}
				_, _ = s.crm.CreateDeal(ctx, orgID, &models.CreateDeal{
					PipelineID: pipe.ID, StageID: stageID, ContactID: contactID, Name: name, Currency: "BRL",
				})
			}
		}
	}
}

func stageIDByName(p *models.Pipeline, name string) uuid.UUID {
	if p == nil {
		return uuid.Nil
	}
	for _, st := range p.Stages {
		if strings.EqualFold(st.Name, name) {
			return st.ID
		}
	}
	return uuid.Nil
}
