package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Task represents a task in the system
type Task struct {
	ID             uuid.UUID
	TaskType       string
	EmailAccountID uuid.UUID
	Status         string
	MessageID      string
	ScheduledAt    *time.Time
	CompletedAt    *time.Time
	CloudTaskName  *string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// CampaignTask represents campaign-specific task data
type CampaignTask struct {
	TaskID     uuid.UUID
	CampaignID *uuid.UUID
	ContactID  *uuid.UUID
	SequenceID *uuid.UUID
}

// WarmupTask represents warmup-specific task data
type WarmupTask struct {
	TaskID          uuid.UUID
	TargetAccountID *uuid.UUID
}

// EmailTask represents email-specific task data
type EmailTask struct {
	TaskID    uuid.UUID
	To        []string
	CC        []string
	BCC       []string
	InReplyTo []string
	Subject   string
	Body      string
	BodyHTML  string
	BodyPlain string
	ThreadID  *string
	SendMode  string
	Encrypted bool
}

// TaskFailure represents a task failure record
type TaskFailure struct {
	TaskID  uuid.UUID
	Title   string
	Message string
}

// ScheduledEmailItem is the join shape returned by
// ListScheduledForUser — task + email_task + sender mailbox columns,
// shaped for the dashboard's "Scheduled" view.
type ScheduledEmailItem struct {
	TaskID      uuid.UUID
	ScheduledAt time.Time
	CreatedAt   time.Time

	AccountID    uuid.UUID
	AccountEmail string
	AccountName  string

	To       []string
	CC       []string
	BCC      []string
	Subject  string
	Body     string
	BodyHTML string

	ThreadID *string
}

// TaskRepository defines methods for task data access
type TaskRepository interface {
	// Create operations
	CreateTask(ctx context.Context, task *Task) error
	CreateCampaignTask(ctx context.Context, campaignTask *CampaignTask) error
	CreateWarmupTask(ctx context.Context, warmupTask *WarmupTask) error
	CreateEmailTask(ctx context.Context, emailTask *EmailTask) error

	// Read operations
	GetTask(ctx context.Context, taskID uuid.UUID) (*Task, error)
	GetTaskByMessageID(ctx context.Context, messageID string) (*Task, error)
	GetCampaignTask(ctx context.Context, taskID uuid.UUID) (*CampaignTask, error)
	GetWarmupTask(ctx context.Context, taskID uuid.UUID) (*WarmupTask, error)
	GetEmailTask(ctx context.Context, taskID uuid.UUID) (*EmailTask, error)

	// Scheduling queries (CRITICAL for "next best time" calculation)
	CountEmailsSentToday(ctx context.Context, accountID uuid.UUID) (int, error)
	GetLastEmailTime(ctx context.Context, accountID uuid.UUID) (*time.Time, error)
	GetScheduledTasksForAccount(ctx context.Context, accountID uuid.UUID, date time.Time) ([]Task, error)
	GetScheduledTasksToday(ctx context.Context, accountID uuid.UUID) ([]Task, error)

	// Update operations
	UpdateTaskStatus(ctx context.Context, taskID uuid.UUID, status string) error
	UpdateTaskScheduledAt(ctx context.Context, taskID uuid.UUID, scheduledAt time.Time, cloudTaskName string) error
	RecordTaskFailure(ctx context.Context, taskID uuid.UUID, title, message string) error

	// Count only campaign tasks completed today (excludes warmup)
	CountCampaignEmailsSentToday(ctx context.Context, accountID uuid.UUID) (int, error)

	// Create user-initiated email task (transactional)
	CreateEmailTaskFull(ctx context.Context, task *Task, emailTask *EmailTask) error

	// Delete operations
	DeleteTask(ctx context.Context, taskID uuid.UUID) error

	// Task locking
	CreateTaskWithLock(ctx context.Context, task *Task, campaignTask *CampaignTask) (bool, error)
	CreateWarmupTaskWithLock(ctx context.Context, task *Task, warmupTask *WarmupTask) (bool, error)
	UpdateTaskStatusWithLock(ctx context.Context, taskID uuid.UUID, status string) error

	// Update campaign task with contact/sequence IDs (for tracking)
	UpdateCampaignTaskTracking(ctx context.Context, taskID, contactID, sequenceID uuid.UUID) error

	// ListScheduledForUser returns every pending email task scheduled
	// for the user's mailboxes, ordered by next-to-fire. Used by the
	// unibox "Scheduled" view.
	ListScheduledForUser(ctx context.Context, userID uuid.UUID, limit int) ([]ScheduledEmailItem, error)
	// ListScheduledForUserByThread is the same query scoped to a
	// single email thread. ThreadView uses it to render queued sends
	// inline alongside already-sent messages so the user can see (and
	// cancel) what's about to fire on the conversation they're
	// reading.
	ListScheduledForUserByThread(ctx context.Context, userID uuid.UUID, threadID string, limit int) ([]ScheduledEmailItem, error)
	// CountScheduledForUser returns the number of pending email tasks
	// currently scheduled (regardless of fire time). Used for the
	// scope-rail counter.
	CountScheduledForUser(ctx context.Context, userID uuid.UUID) (int64, error)
	// CancelScheduledByUser flips a pending email task to status
	// 'cancelled' only when (a) it belongs to a mailbox the user owns,
	// (b) it's still pending. Returns (cloudTaskName, ok, err) — the
	// Cloud Task resource name is included so the caller can issue a
	// best-effort DeleteTask to clean the queue. The handler still
	// short-circuits on a non-pending status, so a failed DeleteTask
	// just degrades to a harmless no-op dispatch — never a real send.
	// `ok` distinguishes 404 (no row updated) from 200.
	CancelScheduledByUser(ctx context.Context, taskID, userID uuid.UUID) (cloudTaskName *string, ok bool, err error)
}

type taskRepository struct {
	db *pgxpool.Pool
}

// NewTaskRepository creates a new task repository
func NewTaskRepository(db *pgxpool.Pool) TaskRepository {
	return &taskRepository{db: db}
}

// CreateTask creates a new task
func (r *taskRepository) CreateTask(ctx context.Context, task *Task) error {
	query := `
		INSERT INTO tasks (id, task_type, email_account_id, status, message_id, scheduled_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW(), NOW())
	`

	_, err := r.db.Exec(ctx, query,
		task.ID,
		task.TaskType,
		task.EmailAccountID,
		task.Status,
		task.MessageID,
		task.ScheduledAt,
	)

	return err
}

// CreateCampaignTask creates campaign-specific task data
func (r *taskRepository) CreateCampaignTask(ctx context.Context, campaignTask *CampaignTask) error {
	query := `
		INSERT INTO campaign_tasks (task_id, campaign_id, contact_id, sequence_id)
		VALUES ($1, $2, $3, $4)
	`

	_, err := r.db.Exec(ctx, query,
		campaignTask.TaskID,
		campaignTask.CampaignID,
		campaignTask.ContactID,
		campaignTask.SequenceID,
	)

	return err
}

// CreateWarmupTask creates warmup-specific task data
func (r *taskRepository) CreateWarmupTask(ctx context.Context, warmupTask *WarmupTask) error {
	query := `
		INSERT INTO warmup_tasks (task_id, target_account_id)
		VALUES ($1, $2)
	`

	_, err := r.db.Exec(ctx, query,
		warmupTask.TaskID,
		warmupTask.TargetAccountID,
	)

	return err
}

// CreateEmailTask creates email-specific task data
func (r *taskRepository) CreateEmailTask(ctx context.Context, emailTask *EmailTask) error {
	query := `
		INSERT INTO email_tasks (task_id, to_addrs, cc, bcc, in_reply_to, subject, body, body_html, body_plain, thread_id, send_mode, encrypted)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`

	sendMode := emailTask.SendMode
	if sendMode == "" {
		sendMode = "instant"
	}

	_, err := r.db.Exec(ctx, query,
		emailTask.TaskID,
		emailTask.To,
		emailTask.CC,
		emailTask.BCC,
		emailTask.InReplyTo,
		emailTask.Subject,
		emailTask.Body,
		emailTask.BodyHTML,
		emailTask.BodyPlain,
		emailTask.ThreadID,
		sendMode,
		emailTask.Encrypted,
	)

	return err
}

// GetTask retrieves a task by ID
func (r *taskRepository) GetTask(ctx context.Context, taskID uuid.UUID) (*Task, error) {
	query := `
		SELECT id, task_type, email_account_id, status, message_id,
		       scheduled_at, completed_at, cloud_task_name, created_at, updated_at
		FROM tasks
		WHERE id = $1
	`

	task := &Task{}
	err := r.db.QueryRow(ctx, query, taskID).Scan(
		&task.ID,
		&task.TaskType,
		&task.EmailAccountID,
		&task.Status,
		&task.MessageID,
		&task.ScheduledAt,
		&task.CompletedAt,
		&task.CloudTaskName,
		&task.CreatedAt,
		&task.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}

	return task, err
}

// GetTaskByMessageID retrieves the latest task by RFC Message-ID.
func (r *taskRepository) GetTaskByMessageID(ctx context.Context, messageID string) (*Task, error) {
	query := `
		SELECT id, task_type, email_account_id, status, message_id,
		       scheduled_at, completed_at, cloud_task_name, created_at, updated_at
		FROM tasks
		WHERE message_id = $1
		ORDER BY created_at DESC
		LIMIT 1
	`

	task := &Task{}
	err := r.db.QueryRow(ctx, query, messageID).Scan(
		&task.ID,
		&task.TaskType,
		&task.EmailAccountID,
		&task.Status,
		&task.MessageID,
		&task.ScheduledAt,
		&task.CompletedAt,
		&task.CloudTaskName,
		&task.CreatedAt,
		&task.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return task, err
}

// GetCampaignTask retrieves campaign task data
func (r *taskRepository) GetCampaignTask(ctx context.Context, taskID uuid.UUID) (*CampaignTask, error) {
	query := `
		SELECT task_id, campaign_id, contact_id, sequence_id
		FROM campaign_tasks
		WHERE task_id = $1
	`

	campaignTask := &CampaignTask{}
	err := r.db.QueryRow(ctx, query, taskID).Scan(
		&campaignTask.TaskID,
		&campaignTask.CampaignID,
		&campaignTask.ContactID,
		&campaignTask.SequenceID,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}

	return campaignTask, err
}

// GetWarmupTask retrieves warmup task data
func (r *taskRepository) GetWarmupTask(ctx context.Context, taskID uuid.UUID) (*WarmupTask, error) {
	query := `
		SELECT task_id, target_account_id
		FROM warmup_tasks
		WHERE task_id = $1
	`

	warmupTask := &WarmupTask{}
	err := r.db.QueryRow(ctx, query, taskID).Scan(
		&warmupTask.TaskID,
		&warmupTask.TargetAccountID,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}

	return warmupTask, err
}

// GetEmailTask retrieves email task data
func (r *taskRepository) GetEmailTask(ctx context.Context, taskID uuid.UUID) (*EmailTask, error) {
	query := `
		SELECT task_id, to_addrs, cc, bcc, in_reply_to, subject, body, body_html, body_plain, thread_id, send_mode, encrypted
		FROM email_tasks
		WHERE task_id = $1
	`

	emailTask := &EmailTask{}
	err := r.db.QueryRow(ctx, query, taskID).Scan(
		&emailTask.TaskID,
		&emailTask.To,
		&emailTask.CC,
		&emailTask.BCC,
		&emailTask.InReplyTo,
		&emailTask.Subject,
		&emailTask.Body,
		&emailTask.BodyHTML,
		&emailTask.BodyPlain,
		&emailTask.ThreadID,
		&emailTask.SendMode,
		&emailTask.Encrypted,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}

	return emailTask, err
}

// CountCampaignEmailsSentToday counts only campaign tasks completed today (excludes warmup)
func (r *taskRepository) CountCampaignEmailsSentToday(ctx context.Context, accountID uuid.UUID) (int, error) {
	query := `
		SELECT COUNT(*)
		FROM tasks
		WHERE email_account_id = $1
		  AND status = 'completed'
		  AND task_type = 'campaign'
		  AND DATE(completed_at) = CURRENT_DATE
	`

	var count int
	err := r.db.QueryRow(ctx, query, accountID).Scan(&count)
	return count, err
}

// CreateEmailTaskFull creates a task and email task entry in a single transaction
func (r *taskRepository) CreateEmailTaskFull(ctx context.Context, task *Task, emailTask *EmailTask) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Create task
	taskQuery := `
		INSERT INTO tasks (id, task_type, email_account_id, status, message_id, scheduled_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW(), NOW())
	`
	_, err = tx.Exec(ctx, taskQuery, task.ID, task.TaskType, task.EmailAccountID, task.Status, task.MessageID, task.ScheduledAt)
	if err != nil {
		return err
	}

	// Create email task
	sendMode := emailTask.SendMode
	if sendMode == "" {
		sendMode = "instant"
	}

	etQuery := `
		INSERT INTO email_tasks (task_id, to_addrs, cc, bcc, in_reply_to, subject, body, body_html, body_plain, thread_id, send_mode, encrypted)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`
	_, err = tx.Exec(ctx, etQuery,
		emailTask.TaskID,
		emailTask.To,
		emailTask.CC,
		emailTask.BCC,
		emailTask.InReplyTo,
		emailTask.Subject,
		emailTask.Body,
		emailTask.BodyHTML,
		emailTask.BodyPlain,
		emailTask.ThreadID,
		sendMode,
		emailTask.Encrypted,
	)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// CountEmailsSentToday counts emails sent today from an account
func (r *taskRepository) CountEmailsSentToday(ctx context.Context, accountID uuid.UUID) (int, error) {
	query := `
		SELECT COUNT(*)
		FROM tasks
		WHERE email_account_id = $1
		  AND status = 'completed'
		  AND DATE(completed_at) = CURRENT_DATE
	`

	var count int
	err := r.db.QueryRow(ctx, query, accountID).Scan(&count)
	return count, err
}

// GetLastEmailTime gets the last email send time for an account
func (r *taskRepository) GetLastEmailTime(ctx context.Context, accountID uuid.UUID) (*time.Time, error) {
	query := `
		SELECT MAX(completed_at)
		FROM tasks
		WHERE email_account_id = $1
		  AND status = 'completed'
	`

	var lastTime *time.Time
	err := r.db.QueryRow(ctx, query, accountID).Scan(&lastTime)

	if err == sql.ErrNoRows {
		return nil, nil
	}

	return lastTime, err
}

// GetScheduledTasksForAccount gets scheduled tasks for an account on a specific date
func (r *taskRepository) GetScheduledTasksForAccount(ctx context.Context, accountID uuid.UUID, date time.Time) ([]Task, error) {
	query := `
		SELECT id, task_type, email_account_id, status, message_id,
		       scheduled_at, completed_at, cloud_task_name, created_at, updated_at
		FROM tasks
		WHERE email_account_id = $1
		  AND status = 'pending'
		  AND DATE(scheduled_at) = DATE($2)
		ORDER BY scheduled_at ASC
	`

	rows, err := r.db.Query(ctx, query, accountID, date)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []Task
	for rows.Next() {
		task := Task{}
		err := rows.Scan(
			&task.ID,
			&task.TaskType,
			&task.EmailAccountID,
			&task.Status,
			&task.MessageID,
			&task.ScheduledAt,
			&task.CompletedAt,
			&task.CloudTaskName,
			&task.CreatedAt,
			&task.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}

	return tasks, rows.Err()
}

// GetScheduledTasksToday gets scheduled tasks for an account today
func (r *taskRepository) GetScheduledTasksToday(ctx context.Context, accountID uuid.UUID) ([]Task, error) {
	return r.GetScheduledTasksForAccount(ctx, accountID, time.Now())
}

// UpdateTaskStatus updates the status of a task
func (r *taskRepository) UpdateTaskStatus(ctx context.Context, taskID uuid.UUID, status string) error {
	query := `
		UPDATE tasks
		SET status = $1,
		    updated_at = NOW(),
		    completed_at = CASE WHEN $1 = 'completed' THEN NOW() ELSE completed_at END
		WHERE id = $2
	`

	_, err := r.db.Exec(ctx, query, status, taskID)
	return err
}

// UpdateTaskScheduledAt updates the scheduled time and cloud task name
func (r *taskRepository) UpdateTaskScheduledAt(ctx context.Context, taskID uuid.UUID, scheduledAt time.Time, cloudTaskName string) error {
	query := `
		UPDATE tasks
		SET scheduled_at = $1,
		    cloud_task_name = $2,
		    updated_at = NOW()
		WHERE id = $3
	`

	_, err := r.db.Exec(ctx, query, scheduledAt, cloudTaskName, taskID)
	return err
}

// RecordTaskFailure records a task failure
func (r *taskRepository) RecordTaskFailure(ctx context.Context, taskID uuid.UUID, title, message string) error {
	// First update task status to failed
	err := r.UpdateTaskStatus(ctx, taskID, "failed")
	if err != nil {
		return err
	}

	// Then insert failure record
	query := `
		INSERT INTO task_failures (task_id, title, message)
		VALUES ($1, $2, $3)
		ON CONFLICT (task_id) DO UPDATE
		SET title = EXCLUDED.title,
		    message = EXCLUDED.message
	`

	_, err = r.db.Exec(ctx, query, taskID, title, message)
	return err
}

// DeleteTask deletes a task and all related data
func (r *taskRepository) DeleteTask(ctx context.Context, taskID uuid.UUID) error {
	query := `DELETE FROM tasks WHERE id = $1`
	_, err := r.db.Exec(ctx, query, taskID)
	return err
}

// CreateTaskWithLock creates a campaign task with a PostgreSQL advisory lock.
// It returns false when the campaign already has a pending wakeup task.
func (r *taskRepository) CreateTaskWithLock(ctx context.Context, task *Task, campaignTask *CampaignTask) (bool, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)

	// Acquire advisory lock based on campaign ID
	if campaignTask != nil && campaignTask.CampaignID != nil {
		_, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('campaign_task_' || $1::text))`, *campaignTask.CampaignID)
		if err != nil {
			return false, err
		}

		var existing uuid.UUID
		err = tx.QueryRow(ctx, `
			SELECT t.id
			FROM tasks t
			INNER JOIN campaign_tasks ct ON ct.task_id = t.id
			WHERE ct.campaign_id = $1
			  AND t.status = 'pending'
			LIMIT 1
		`, *campaignTask.CampaignID).Scan(&existing)
		if err == nil {
			return false, nil
		}
		if err != pgx.ErrNoRows {
			return false, err
		}
	}

	// Create task
	taskQuery := `
		INSERT INTO tasks (id, task_type, email_account_id, status, message_id, scheduled_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW(), NOW())
	`
	_, err = tx.Exec(ctx, taskQuery, task.ID, task.TaskType, task.EmailAccountID, task.Status, task.MessageID, task.ScheduledAt)
	if err != nil {
		return false, err
	}

	// Create campaign task entry if provided
	if campaignTask != nil {
		ctQuery := `INSERT INTO campaign_tasks (task_id, campaign_id, contact_id, sequence_id) VALUES ($1, $2, $3, $4)`
		_, err = tx.Exec(ctx, ctQuery, campaignTask.TaskID, campaignTask.CampaignID, campaignTask.ContactID, campaignTask.SequenceID)
		if err != nil {
			return false, err
		}
	}

	return true, tx.Commit(ctx)
}

// CreateWarmupTaskWithLock creates one pending warmup wakeup per mailbox.
// It returns false when the account already has a pending warmup task.
func (r *taskRepository) CreateWarmupTaskWithLock(ctx context.Context, task *Task, warmupTask *WarmupTask) (bool, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('warmup_task_' || $1::text))`, task.EmailAccountID)
	if err != nil {
		return false, err
	}

	var existing uuid.UUID
	err = tx.QueryRow(ctx, `
		SELECT id
		FROM tasks
		WHERE email_account_id = $1
		  AND task_type = 'warmup'
		  AND status = 'pending'
		LIMIT 1
	`, task.EmailAccountID).Scan(&existing)
	if err == nil {
		return false, nil
	}
	if err != pgx.ErrNoRows {
		return false, err
	}

	taskQuery := `
		INSERT INTO tasks (id, task_type, email_account_id, status, message_id, scheduled_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW(), NOW())
	`
	_, err = tx.Exec(ctx, taskQuery, task.ID, task.TaskType, task.EmailAccountID, task.Status, task.MessageID, task.ScheduledAt)
	if err != nil {
		return false, err
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO warmup_tasks (task_id, target_account_id)
		VALUES ($1, $2)
	`, warmupTask.TaskID, warmupTask.TargetAccountID)
	if err != nil {
		return false, err
	}

	return true, tx.Commit(ctx)
}

// UpdateTaskStatusWithLock updates task status with an advisory lock to prevent race conditions
func (r *taskRepository) UpdateTaskStatusWithLock(ctx context.Context, taskID uuid.UUID, status string) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Acquire advisory lock on task ID
	_, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1::text))`, taskID)
	if err != nil {
		return err
	}

	// Update status
	query := `
		UPDATE tasks
		SET status = $1,
		    updated_at = NOW(),
		    completed_at = CASE WHEN $1 = 'completed' THEN NOW() ELSE completed_at END
		WHERE id = $2
	`
	_, err = tx.Exec(ctx, query, status, taskID)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// UpdateCampaignTaskTracking updates the campaign task with contact_id and sequence_id
// This is called when the task is processed and we know which contact/sequence to send to
// These IDs are needed for tracking pixel/click events to record progress
func (r *taskRepository) UpdateCampaignTaskTracking(ctx context.Context, taskID, contactID, sequenceID uuid.UUID) error {
	query := `
		UPDATE campaign_tasks
		SET contact_id = $2, sequence_id = $3
		WHERE task_id = $1
	`

	_, err := r.db.Exec(ctx, query, taskID, contactID, sequenceID)
	return err
}

// ListScheduledForUser returns user-initiated email tasks still in
// 'pending' state, ordered by scheduled_at. Joins tasks → email_tasks
// → email_accounts so callers don't need three lookups per row.
func (r *taskRepository) ListScheduledForUser(ctx context.Context, userID uuid.UUID, limit int) ([]ScheduledEmailItem, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	query := `
		SELECT
			t.id,
			t.scheduled_at,
			t.created_at,
			ea.id,
			ea.email,
			COALESCE(ea.name, ''),
			et.to_addrs,
			et.cc,
			et.bcc,
			et.subject,
			et.body_plain,
			et.body_html,
			et.thread_id
		FROM tasks t
		INNER JOIN email_tasks et ON et.task_id = t.id
		INNER JOIN email_accounts ea ON ea.id = t.email_account_id
		WHERE ea.user_id = $1
		  AND t.task_type = 'email'
		  AND t.status = 'pending'
		ORDER BY t.scheduled_at ASC NULLS LAST, t.created_at ASC
		LIMIT $2
	`
	rows, err := r.db.Query(ctx, query, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]ScheduledEmailItem, 0)
	for rows.Next() {
		var it ScheduledEmailItem
		var scheduledAt sql.NullTime
		if err := rows.Scan(
			&it.TaskID,
			&scheduledAt,
			&it.CreatedAt,
			&it.AccountID,
			&it.AccountEmail,
			&it.AccountName,
			&it.To,
			&it.CC,
			&it.BCC,
			&it.Subject,
			&it.Body,
			&it.BodyHTML,
			&it.ThreadID,
		); err != nil {
			return nil, err
		}
		if scheduledAt.Valid {
			it.ScheduledAt = scheduledAt.Time
		}
		items = append(items, it)
	}
	return items, rows.Err()
}

// ListScheduledForUserByThread is ListScheduledForUser scoped to a
// single thread. Same join + ownership enforcement, plus an extra
// thread_id filter. Empty threadID is treated as "no rows" so the
// caller can't accidentally fall back to the full list.
func (r *taskRepository) ListScheduledForUserByThread(ctx context.Context, userID uuid.UUID, threadID string, limit int) ([]ScheduledEmailItem, error) {
	if threadID == "" {
		return []ScheduledEmailItem{}, nil
	}
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	query := `
		SELECT
			t.id,
			t.scheduled_at,
			t.created_at,
			ea.id,
			ea.email,
			COALESCE(ea.name, ''),
			et.to_addrs,
			et.cc,
			et.bcc,
			et.subject,
			et.body_plain,
			et.body_html,
			et.thread_id
		FROM tasks t
		INNER JOIN email_tasks et ON et.task_id = t.id
		INNER JOIN email_accounts ea ON ea.id = t.email_account_id
		WHERE ea.user_id = $1
		  AND t.task_type = 'email'
		  AND t.status = 'pending'
		  AND et.thread_id = $2
		ORDER BY t.scheduled_at ASC NULLS LAST, t.created_at ASC
		LIMIT $3
	`
	rows, err := r.db.Query(ctx, query, userID, threadID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]ScheduledEmailItem, 0)
	for rows.Next() {
		var it ScheduledEmailItem
		var scheduledAt sql.NullTime
		if err := rows.Scan(
			&it.TaskID,
			&scheduledAt,
			&it.CreatedAt,
			&it.AccountID,
			&it.AccountEmail,
			&it.AccountName,
			&it.To,
			&it.CC,
			&it.BCC,
			&it.Subject,
			&it.Body,
			&it.BodyHTML,
			&it.ThreadID,
		); err != nil {
			return nil, err
		}
		if scheduledAt.Valid {
			it.ScheduledAt = scheduledAt.Time
		}
		items = append(items, it)
	}
	return items, rows.Err()
}

// CountScheduledForUser returns how many email tasks are pending across
// every mailbox the user owns. Cheap enough to fold into the overview
// payload.
func (r *taskRepository) CountScheduledForUser(ctx context.Context, userID uuid.UUID) (int64, error) {
	query := `
		SELECT COUNT(*)
		FROM tasks t
		INNER JOIN email_accounts ea ON ea.id = t.email_account_id
		WHERE ea.user_id = $1
		  AND t.task_type = 'email'
		  AND t.status = 'pending'
	`
	var n int64
	err := r.db.QueryRow(ctx, query, userID).Scan(&n)
	return n, err
}

// CancelScheduledByUser flips a single pending email task to
// 'cancelled', enforcing ownership through the email_accounts join.
// Returns the Cloud Task resource name (if the row had one) so the
// caller can issue a best-effort DeleteTask to clean up the GCP
// queue. ok=false when the task either doesn't exist, isn't owned by
// this user, isn't an email task, or already left the pending state.
func (r *taskRepository) CancelScheduledByUser(ctx context.Context, taskID, userID uuid.UUID) (*string, bool, error) {
	query := `
		UPDATE tasks t
		SET status = 'cancelled',
		    updated_at = NOW()
		FROM email_accounts ea
		WHERE t.id = $1
		  AND t.email_account_id = ea.id
		  AND ea.user_id = $2
		  AND t.task_type = 'email'
		  AND t.status = 'pending'
		RETURNING t.cloud_task_name
	`
	var cloudTaskName *string
	err := r.db.QueryRow(ctx, query, taskID, userID).Scan(&cloudTaskName)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return cloudTaskName, true, nil
}
