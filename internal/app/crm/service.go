package crm

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type CRMService interface {
	// Notes
	CreateNote(ctx context.Context, orgID, contactID, userID uuid.UUID, data *models.CreateContactNote) (*models.ContactNote, *errx.Error)
	ListNotes(ctx context.Context, orgID, contactID uuid.UUID, limit int, cursor *uuid.UUID) (*models.ContactNotesResult, *errx.Error)
	UpdateNote(ctx context.Context, orgID, noteID uuid.UUID, data *models.UpdateContactNote) (*models.ContactNote, *errx.Error)
	DeleteNote(ctx context.Context, orgID, noteID uuid.UUID) *errx.Error

	// Activities
	ListActivities(ctx context.Context, orgID, contactID uuid.UUID, limit int, cursor *uuid.UUID) (*models.ContactActivitiesResult, *errx.Error)

	// Pipelines
	CreatePipeline(ctx context.Context, orgID uuid.UUID, data *models.CreatePipeline) (*models.Pipeline, *errx.Error)
	GetPipeline(ctx context.Context, orgID, pipelineID uuid.UUID) (*models.Pipeline, *errx.Error)
	ListPipelines(ctx context.Context, orgID uuid.UUID) ([]models.Pipeline, *errx.Error)
	UpdatePipeline(ctx context.Context, orgID, pipelineID uuid.UUID, data *models.UpdatePipeline) (*models.Pipeline, *errx.Error)
	DeletePipeline(ctx context.Context, orgID, pipelineID uuid.UUID) *errx.Error

	// Pipeline Stages
	CreateStage(ctx context.Context, orgID, pipelineID uuid.UUID, data *models.CreatePipelineStage) (*models.PipelineStage, *errx.Error)
	UpdateStage(ctx context.Context, orgID, stageID uuid.UUID, data *models.UpdatePipelineStage) (*models.PipelineStage, *errx.Error)
	DeleteStage(ctx context.Context, orgID, stageID uuid.UUID) *errx.Error

	// Deals
	CreateDeal(ctx context.Context, orgID uuid.UUID, data *models.CreateDeal) (*models.Deal, *errx.Error)
	GetDeal(ctx context.Context, orgID, dealID uuid.UUID) (*models.Deal, *errx.Error)
	ListDeals(ctx context.Context, orgID uuid.UUID, pipelineID, stageID *uuid.UUID, status *string, limit int, cursor *uuid.UUID) (*models.DealsResult, *errx.Error)
	SearchDeals(ctx context.Context, orgID uuid.UUID, filters models.SearchDeals, limit, offset int) (*models.DealsSearchResult, *errx.Error)
	DealsSummary(ctx context.Context, orgID uuid.UUID, filters models.SearchDeals) (*models.DealsSummary, *errx.Error)
	UpdateDeal(ctx context.Context, orgID, dealID uuid.UUID, userID *uuid.UUID, data *models.UpdateDeal) (*models.Deal, *errx.Error)
	DeleteDeal(ctx context.Context, orgID, dealID uuid.UUID) *errx.Error
	GetDealsByContact(ctx context.Context, orgID, contactID uuid.UUID) ([]models.Deal, *errx.Error)

	// CRM Tasks
	CreateCRMTask(ctx context.Context, orgID, userID uuid.UUID, data *models.CreateCRMTask) (*models.CRMTask, *errx.Error)
	GetCRMTask(ctx context.Context, orgID, taskID uuid.UUID) (*models.CRMTask, *errx.Error)
	ListCRMTasks(ctx context.Context, orgID uuid.UUID, contactID, dealID, assignedTo *uuid.UUID, status *string, limit int, cursor *uuid.UUID) (*models.CRMTasksResult, *errx.Error)
	SearchTasks(ctx context.Context, orgID uuid.UUID, filters models.SearchTasks, limit, offset int) (*models.TasksSearchResult, *errx.Error)
	TasksSummary(ctx context.Context, orgID uuid.UUID, filters models.SearchTasks) (*models.TasksSummary, *errx.Error)
	UpdateCRMTask(ctx context.Context, orgID, taskID uuid.UUID, userID *uuid.UUID, data *models.UpdateCRMTask) (*models.CRMTask, *errx.Error)
	DeleteCRMTask(ctx context.Context, orgID, taskID uuid.UUID) *errx.Error
	BulkUpdateCRMTasks(ctx context.Context, orgID uuid.UUID, userID *uuid.UUID, data *models.BulkUpdateTasks) (int64, *errx.Error)
	BulkDeleteCRMTasks(ctx context.Context, orgID uuid.UUID, sel models.TaskSelection) (int64, *errx.Error)

	// CRM Task Types (user-managed)
	ListTaskTypes(ctx context.Context, orgID uuid.UUID) ([]models.CRMTaskType, *errx.Error)
	CreateTaskType(ctx context.Context, orgID uuid.UUID, data *models.CreateCRMTaskType) (*models.CRMTaskType, *errx.Error)
	UpdateTaskType(ctx context.Context, orgID, typeID uuid.UUID, data *models.UpdateCRMTaskType) (*models.CRMTaskType, *errx.Error)
	DeleteTaskType(ctx context.Context, orgID, typeID uuid.UUID) *errx.Error
}

// maxTaskBulkBatch bounds an explicit id list in one bulk request body. A
// select-all is bounded separately, by models.MaxTaskBulkSelection.
const maxTaskBulkBatch = 1000

type crmService struct {
	repo repository.CRMRepository
}

func NewService(repo repository.CRMRepository) CRMService {
	return &crmService{repo: repo}
}

func toErrx(err error) *errx.Error {
	if err == nil {
		return nil
	}
	var bizErr *errx.Error
	if errors.As(err, &bizErr) {
		return bizErr
	}
	return errx.InternalError()
}

// =====================
// Notes
// =====================

func (s *crmService) CreateNote(ctx context.Context, orgID, contactID, userID uuid.UUID, data *models.CreateContactNote) (*models.ContactNote, *errx.Error) {
	if len(data.Content) == 0 {
		return nil, errx.New(errx.BadRequest, "content is required")
	}
	if len(data.Content) > 10000 {
		return nil, errx.New(errx.BadRequest, "content must be at most 10000 characters")
	}

	note, err := s.repo.CreateNote(ctx, orgID, contactID, userID, data.Content)
	if err != nil {
		return nil, toErrx(err)
	}

	// Record activity
	_ = s.repo.RecordActivity(ctx, orgID, contactID, &userID, models.ActivityNoteAdded, map[string]interface{}{
		"note_id": note.ID.String(),
	})

	return note, nil
}

func (s *crmService) ListNotes(ctx context.Context, orgID, contactID uuid.UUID, limit int, cursor *uuid.UUID) (*models.ContactNotesResult, *errx.Error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	result, err := s.repo.ListNotes(ctx, orgID, contactID, limit, cursor)
	if err != nil {
		return nil, toErrx(err)
	}
	return result, nil
}

func (s *crmService) UpdateNote(ctx context.Context, orgID, noteID uuid.UUID, data *models.UpdateContactNote) (*models.ContactNote, *errx.Error) {
	if data.Content == nil || len(*data.Content) == 0 {
		return nil, errx.New(errx.BadRequest, "content is required")
	}
	if len(*data.Content) > 10000 {
		return nil, errx.New(errx.BadRequest, "content must be at most 10000 characters")
	}

	note, err := s.repo.UpdateNote(ctx, orgID, noteID, *data.Content)
	if err != nil {
		return nil, toErrx(err)
	}

	// Record activity
	_ = s.repo.RecordActivity(ctx, orgID, note.ContactID, &note.UserID, models.ActivityNoteUpdated, map[string]interface{}{
		"note_id": note.ID.String(),
	})

	return note, nil
}

func (s *crmService) DeleteNote(ctx context.Context, orgID, noteID uuid.UUID) *errx.Error {
	if err := s.repo.DeleteNote(ctx, orgID, noteID); err != nil {
		return toErrx(err)
	}
	return nil
}

// =====================
// Activities
// =====================

func (s *crmService) ListActivities(ctx context.Context, orgID, contactID uuid.UUID, limit int, cursor *uuid.UUID) (*models.ContactActivitiesResult, *errx.Error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	result, err := s.repo.ListActivities(ctx, orgID, contactID, limit, cursor)
	if err != nil {
		return nil, toErrx(err)
	}
	return result, nil
}

// =====================
// Pipelines
// =====================

func (s *crmService) CreatePipeline(ctx context.Context, orgID uuid.UUID, data *models.CreatePipeline) (*models.Pipeline, *errx.Error) {
	if len(data.Name) == 0 || len(data.Name) > 255 {
		return nil, errx.New(errx.BadRequest, "pipeline name must be between 1 and 255 characters")
	}

	pipeline, err := s.repo.CreatePipeline(ctx, orgID, data)
	if err != nil {
		return nil, toErrx(err)
	}
	return pipeline, nil
}

func (s *crmService) GetPipeline(ctx context.Context, orgID, pipelineID uuid.UUID) (*models.Pipeline, *errx.Error) {
	pipeline, err := s.repo.GetPipeline(ctx, orgID, pipelineID)
	if err != nil {
		return nil, toErrx(err)
	}
	return pipeline, nil
}

func (s *crmService) ListPipelines(ctx context.Context, orgID uuid.UUID) ([]models.Pipeline, *errx.Error) {
	pipelines, err := s.repo.ListPipelines(ctx, orgID)
	if err != nil {
		return nil, toErrx(err)
	}
	return pipelines, nil
}

func (s *crmService) UpdatePipeline(ctx context.Context, orgID, pipelineID uuid.UUID, data *models.UpdatePipeline) (*models.Pipeline, *errx.Error) {
	if data.Name == nil || len(*data.Name) == 0 || len(*data.Name) > 255 {
		return nil, errx.New(errx.BadRequest, "pipeline name must be between 1 and 255 characters")
	}

	pipeline, err := s.repo.UpdatePipeline(ctx, orgID, pipelineID, *data.Name)
	if err != nil {
		return nil, toErrx(err)
	}
	return pipeline, nil
}

func (s *crmService) DeletePipeline(ctx context.Context, orgID, pipelineID uuid.UUID) *errx.Error {
	if err := s.repo.DeletePipeline(ctx, orgID, pipelineID); err != nil {
		return toErrx(err)
	}
	return nil
}

// =====================
// Pipeline Stages
// =====================

func (s *crmService) CreateStage(ctx context.Context, orgID, pipelineID uuid.UUID, data *models.CreatePipelineStage) (*models.PipelineStage, *errx.Error) {
	if len(data.Name) == 0 || len(data.Name) > 255 {
		return nil, errx.New(errx.BadRequest, "stage name must be between 1 and 255 characters")
	}

	stage, err := s.repo.CreateStage(ctx, orgID, pipelineID, data)
	if err != nil {
		return nil, toErrx(err)
	}
	return stage, nil
}

func (s *crmService) UpdateStage(ctx context.Context, orgID, stageID uuid.UUID, data *models.UpdatePipelineStage) (*models.PipelineStage, *errx.Error) {
	stage, err := s.repo.UpdateStage(ctx, orgID, stageID, data)
	if err != nil {
		return nil, toErrx(err)
	}
	return stage, nil
}

func (s *crmService) DeleteStage(ctx context.Context, orgID, stageID uuid.UUID) *errx.Error {
	if err := s.repo.DeleteStage(ctx, orgID, stageID); err != nil {
		return toErrx(err)
	}
	return nil
}

// =====================
// Deals
// =====================

func (s *crmService) CreateDeal(ctx context.Context, orgID uuid.UUID, data *models.CreateDeal) (*models.Deal, *errx.Error) {
	if len(data.Name) == 0 || len(data.Name) > 255 {
		return nil, errx.New(errx.BadRequest, "deal name must be between 1 and 255 characters")
	}

	deal, err := s.repo.CreateDeal(ctx, orgID, data)
	if err != nil {
		return nil, toErrx(err)
	}

	// Record activity if deal has a contact
	if deal.ContactID != nil {
		_ = s.repo.RecordActivity(ctx, orgID, *deal.ContactID, nil, models.ActivityDealCreated, map[string]interface{}{
			"deal_id":   deal.ID.String(),
			"deal_name": deal.Name,
		})
	}

	return deal, nil
}

func (s *crmService) GetDeal(ctx context.Context, orgID, dealID uuid.UUID) (*models.Deal, *errx.Error) {
	deal, err := s.repo.GetDeal(ctx, orgID, dealID)
	if err != nil {
		return nil, toErrx(err)
	}
	return deal, nil
}

func (s *crmService) ListDeals(ctx context.Context, orgID uuid.UUID, pipelineID, stageID *uuid.UUID, status *string, limit int, cursor *uuid.UUID) (*models.DealsResult, *errx.Error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	result, err := s.repo.ListDeals(ctx, orgID, pipelineID, stageID, status, limit, cursor)
	if err != nil {
		return nil, toErrx(err)
	}
	return result, nil
}

func (s *crmService) SearchDeals(ctx context.Context, orgID uuid.UUID, filters models.SearchDeals, limit, offset int) (*models.DealsSearchResult, *errx.Error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	result, err := s.repo.SearchDeals(ctx, orgID, filters, limit, offset)
	if err != nil {
		return nil, toErrx(err)
	}
	return result, nil
}

func (s *crmService) DealsSummary(ctx context.Context, orgID uuid.UUID, filters models.SearchDeals) (*models.DealsSummary, *errx.Error) {
	result, err := s.repo.DealsSummary(ctx, orgID, filters)
	if err != nil {
		return nil, toErrx(err)
	}
	return result, nil
}

func (s *crmService) UpdateDeal(ctx context.Context, orgID, dealID uuid.UUID, userID *uuid.UUID, data *models.UpdateDeal) (*models.Deal, *errx.Error) {
	// Get existing deal for activity recording
	existingDeal, _ := s.repo.GetDeal(ctx, orgID, dealID)

	deal, err := s.repo.UpdateDeal(ctx, orgID, dealID, data)
	if err != nil {
		return nil, toErrx(err)
	}

	// Record stage change activity
	if data.StageID != nil && existingDeal != nil && deal.ContactID != nil {
		if existingDeal.StageID != *data.StageID {
			_ = s.repo.RecordActivity(ctx, orgID, *deal.ContactID, userID, models.ActivityDealStageChange, map[string]interface{}{
				"deal_id":       deal.ID.String(),
				"deal_name":     deal.Name,
				"from_stage_id": existingDeal.StageID.String(),
				"to_stage_id":   data.StageID.String(),
			})
		}
	}

	// Record won/lost activity
	if data.Status != nil && deal.ContactID != nil {
		if *data.Status == "won" {
			_ = s.repo.RecordActivity(ctx, orgID, *deal.ContactID, userID, models.ActivityDealWon, map[string]interface{}{
				"deal_id":   deal.ID.String(),
				"deal_name": deal.Name,
			})
		} else if *data.Status == "lost" {
			_ = s.repo.RecordActivity(ctx, orgID, *deal.ContactID, userID, models.ActivityDealLost, map[string]interface{}{
				"deal_id":     deal.ID.String(),
				"deal_name":   deal.Name,
				"lost_reason": data.LostReason,
			})
		}
	}

	return deal, nil
}

func (s *crmService) DeleteDeal(ctx context.Context, orgID, dealID uuid.UUID) *errx.Error {
	if err := s.repo.DeleteDeal(ctx, orgID, dealID); err != nil {
		return toErrx(err)
	}
	return nil
}

func (s *crmService) GetDealsByContact(ctx context.Context, orgID, contactID uuid.UUID) ([]models.Deal, *errx.Error) {
	deals, err := s.repo.GetDealsByContact(ctx, orgID, contactID)
	if err != nil {
		return nil, toErrx(err)
	}
	return deals, nil
}

// =====================
// CRM Tasks
// =====================

func (s *crmService) CreateCRMTask(ctx context.Context, orgID, userID uuid.UUID, data *models.CreateCRMTask) (*models.CRMTask, *errx.Error) {
	if len(data.Title) == 0 || len(data.Title) > 255 {
		return nil, errx.New(errx.BadRequest, "task title must be between 1 and 255 characters")
	}

	task, err := s.repo.CreateCRMTask(ctx, orgID, userID, data)
	if err != nil {
		return nil, toErrx(err)
	}

	// Record activity on contact
	if task.ContactID != nil {
		_ = s.repo.RecordActivity(ctx, orgID, *task.ContactID, &userID, models.ActivityTaskCreated, map[string]interface{}{
			"task_id":    task.ID.String(),
			"task_title": task.Title,
		})
	}

	return task, nil
}

func (s *crmService) GetCRMTask(ctx context.Context, orgID, taskID uuid.UUID) (*models.CRMTask, *errx.Error) {
	task, err := s.repo.GetCRMTask(ctx, orgID, taskID)
	if err != nil {
		return nil, toErrx(err)
	}
	return task, nil
}

func (s *crmService) ListCRMTasks(ctx context.Context, orgID uuid.UUID, contactID, dealID, assignedTo *uuid.UUID, status *string, limit int, cursor *uuid.UUID) (*models.CRMTasksResult, *errx.Error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	result, err := s.repo.ListCRMTasks(ctx, orgID, contactID, dealID, assignedTo, status, limit, cursor)
	if err != nil {
		return nil, toErrx(err)
	}
	return result, nil
}

func (s *crmService) SearchTasks(ctx context.Context, orgID uuid.UUID, filters models.SearchTasks, limit, offset int) (*models.TasksSearchResult, *errx.Error) {
	if err := filters.Validate(); err != nil {
		return nil, errx.NewWithIdentifier(errx.BadRequest, "invalid_filter", err.Error())
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	result, err := s.repo.SearchCRMTasks(ctx, orgID, filters, limit, offset)
	if err != nil {
		return nil, toErrx(err)
	}
	return result, nil
}

func (s *crmService) TasksSummary(ctx context.Context, orgID uuid.UUID, filters models.SearchTasks) (*models.TasksSummary, *errx.Error) {
	if err := filters.Validate(); err != nil {
		return nil, errx.NewWithIdentifier(errx.BadRequest, "invalid_filter", err.Error())
	}
	result, err := s.repo.TasksSummary(ctx, orgID, filters)
	if err != nil {
		return nil, toErrx(err)
	}
	return result, nil
}

func (s *crmService) UpdateCRMTask(ctx context.Context, orgID, taskID uuid.UUID, userID *uuid.UUID, data *models.UpdateCRMTask) (*models.CRMTask, *errx.Error) {
	task, err := s.repo.UpdateCRMTask(ctx, orgID, taskID, data)
	if err != nil {
		return nil, toErrx(err)
	}

	// Record completion activity on contact
	if data.Status != nil && *data.Status == "completed" && task.ContactID != nil {
		_ = s.repo.RecordActivity(ctx, orgID, *task.ContactID, userID, models.ActivityTaskCompleted, map[string]interface{}{
			"task_id":    task.ID.String(),
			"task_title": task.Title,
		})
	}

	return task, nil
}

// checkTaskSelection validates what the request body says before it reaches the
// database. How many rows the selection actually resolves to is checked by the
// mutation itself (see tooLarge), because a count taken here could be stale by
// the time the write runs.
func (s *crmService) checkTaskSelection(sel models.TaskSelection) *errx.Error {
	if !sel.All {
		if len(sel.Tasks) == 0 {
			return errx.New(errx.BadRequest, "no tasks provided")
		}
		if len(sel.Tasks) > maxTaskBulkBatch {
			return errx.NewWithIdentifier(errx.BadRequest, "too_many_tasks",
				fmt.Sprintf("too many tasks, maximum is %d per batch", maxTaskBulkBatch))
		}
		return nil
	}
	if sel.Filters == nil {
		return errx.New(errx.BadRequest, "a select-all request must carry the filters it applies to")
	}
	if err := sel.Filters.Validate(); err != nil {
		return errx.NewWithIdentifier(errx.BadRequest, "invalid_filter", err.Error())
	}
	if len(sel.Exclude) > models.MaxTaskBulkSelection {
		return errx.NewWithIdentifier(errx.BadRequest, "too_many_tasks",
			fmt.Sprintf("too many exclusions, maximum is %d", models.MaxTaskBulkSelection))
	}
	return nil
}

// tooLarge turns the row count the mutation refused to act on into the error a
// caller sees. The statement wrote nothing, so the action is refused whole
// rather than half applied. The count stops at the cap plus one, which is all
// the mutation needs to look at to know the selection is over it.
func tooLarge(matched int64) *errx.Error {
	if matched <= models.MaxTaskBulkSelection {
		return nil
	}
	return errx.NewWithIdentifier(errx.BadRequest, "selection_too_large",
		fmt.Sprintf("that selection matches more than %d tasks; narrow it with a filter and try again", models.MaxTaskBulkSelection))
}

func (s *crmService) BulkUpdateCRMTasks(ctx context.Context, orgID uuid.UUID, userID *uuid.UUID, data *models.BulkUpdateTasks) (int64, *errx.Error) {
	if data == nil {
		return 0, errx.New(errx.BadRequest, "a bulk update needs a body")
	}
	if data.Status == nil && data.Priority == nil {
		return 0, errx.New(errx.BadRequest, "a bulk update needs a status or a priority")
	}
	if data.Status != nil && !models.ValidCRMTaskStatus(*data.Status) {
		return 0, errx.New(errx.BadRequest, "invalid task status")
	}
	if data.Priority != nil && !models.ValidCRMTaskPriority(*data.Priority) {
		return 0, errx.New(errx.BadRequest, "invalid task priority")
	}
	if xerr := s.checkTaskSelection(data.TaskSelection); xerr != nil {
		return 0, xerr
	}
	matched, affected, err := s.repo.BulkUpdateCRMTasks(ctx, orgID, userID, data.TaskSelection, data, models.MaxTaskBulkSelection)
	if err != nil {
		return 0, toErrx(err)
	}
	if xerr := tooLarge(matched); xerr != nil {
		return 0, xerr
	}
	return affected, nil
}

func (s *crmService) BulkDeleteCRMTasks(ctx context.Context, orgID uuid.UUID, sel models.TaskSelection) (int64, *errx.Error) {
	if xerr := s.checkTaskSelection(sel); xerr != nil {
		return 0, xerr
	}
	matched, affected, err := s.repo.BulkDeleteCRMTasks(ctx, orgID, sel, models.MaxTaskBulkSelection)
	if err != nil {
		return 0, toErrx(err)
	}
	if xerr := tooLarge(matched); xerr != nil {
		return 0, xerr
	}
	return affected, nil
}

func (s *crmService) ListTaskTypes(ctx context.Context, orgID uuid.UUID) ([]models.CRMTaskType, *errx.Error) {
	types, err := s.repo.ListTaskTypes(ctx, orgID)
	if err != nil {
		return nil, toErrx(err)
	}
	return types, nil
}

func (s *crmService) CreateTaskType(ctx context.Context, orgID uuid.UUID, data *models.CreateCRMTaskType) (*models.CRMTaskType, *errx.Error) {
	name := strings.TrimSpace(data.Name)
	if name == "" || len(name) > 60 {
		return nil, errx.New(errx.BadRequest, "task type name must be between 1 and 60 characters")
	}
	data.Name = name
	t, err := s.repo.CreateTaskType(ctx, orgID, data)
	if err != nil {
		return nil, toErrx(err)
	}
	return t, nil
}

func (s *crmService) UpdateTaskType(ctx context.Context, orgID, typeID uuid.UUID, data *models.UpdateCRMTaskType) (*models.CRMTaskType, *errx.Error) {
	if data.Name != nil {
		name := strings.TrimSpace(*data.Name)
		if name == "" || len(name) > 60 {
			return nil, errx.New(errx.BadRequest, "task type name must be between 1 and 60 characters")
		}
		data.Name = &name
	}
	t, err := s.repo.UpdateTaskType(ctx, orgID, typeID, data)
	if err != nil {
		return nil, toErrx(err)
	}
	return t, nil
}

func (s *crmService) DeleteTaskType(ctx context.Context, orgID, typeID uuid.UUID) *errx.Error {
	if err := s.repo.DeleteTaskType(ctx, orgID, typeID); err != nil {
		return toErrx(err)
	}
	return nil
}

func (s *crmService) DeleteCRMTask(ctx context.Context, orgID, taskID uuid.UUID) *errx.Error {
	if err := s.repo.DeleteCRMTask(ctx, orgID, taskID); err != nil {
		return toErrx(err)
	}
	return nil
}
