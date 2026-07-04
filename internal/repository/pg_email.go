package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/encrypt"
	"github.com/warmbly/warmbly/internal/utils"
	"github.com/warmbly/warmbly/internal/utils/paging"
	"github.com/warmbly/warmbly/internal/utils/validate"
)

// SMTPCredentials holds SMTP/IMAP server credentials
type SMTPCredentials struct {
	SMTPHost     string
	SMTPPort     int
	SMTPUser     string
	SMTPPassword string
	IMAPHost     string
	IMAPPort     int
	IMAPUser     string
	IMAPPassword string
}

// OAuthCredentials holds OAuth token credentials
type OAuthCredentials struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
}

type EmailRepository interface {
	Search(ctx context.Context, userID, search string, cursor, tag *string, limit int32, allowedAccountIDs []uuid.UUID) (*models.EmailsResult, *errx.Error)
	Get(ctx context.Context, userID, emailAccountID string) (*models.Email, *errx.Error)
	GetByID(ctx context.Context, emailAccountID uuid.UUID) (*models.Email, *errx.Error)
	GetByTags(ctx context.Context, userID string, tags []string) ([]models.Email, *errx.Error)
	// GetAllActiveByUser returns every active mailbox for a user (no tag/sender
	// filter) — the "all" sender pool used when a campaign picks neither tags nor
	// explicit accounts.
	GetAllActiveByUser(ctx context.Context, userID string) ([]models.Email, *errx.Error)
	// GetByCampaignSenders returns the active mailboxes in a campaign's explicit
	// sender pool, carrying each sender's rotation metadata (weight,
	// rotation_position, last_sent_at) for the scheduler's rotation modes.
	GetByCampaignSenders(ctx context.Context, userID string, campaignID uuid.UUID) ([]CampaignSenderAccount, *errx.Error)
	GetSMTPCredentials(ctx context.Context, emailAccountID uuid.UUID) (*SMTPCredentials, *errx.Error)
	GetOAuthCredentials(ctx context.Context, emailAccountID uuid.UUID) (*OAuthCredentials, *errx.Error)
	GetWorkerID(ctx context.Context, emailAccountID uuid.UUID) (*uuid.UUID, *errx.Error)
	SetWorkerID(ctx context.Context, emailAccountID, workerID uuid.UUID) *errx.Error
	Update(ctx context.Context, userID, emailAccountID string, udata *models.UpdateEmail) (*models.Email, *errx.Error)
	// SetWarmupLifecycle starts, pauses, resumes, or disables warmup for a
	// mailbox. "start"/"resume" preserve ramp progress (a paused mailbox
	// resumes where it left off); "pause" keeps progress; "disable" turns
	// warmup off entirely. The timestamp math runs in SQL so the transition
	// is atomic and idempotent.
	SetWarmupLifecycle(ctx context.Context, userID, emailAccountID, action string) (*models.Email, *errx.Error)
	UpdateTrackingDomain(ctx context.Context, userID, emailAccountID, domain string, verified bool, verifiedAt *time.Time) *errx.Error
	Delete(ctx context.Context, userID, emailAccountID string) *errx.Error

	NewOauthAccount(ctx context.Context, userID string, data models.NewOauthAccount) (*models.Email, *errx.Error)
	NewSMTPIMAPAccount(ctx context.Context, userID string, data models.NewSMTPIMAPAccount) (*models.Email, *errx.Error)
	RefreshBoxToken(ctx context.Context, id uuid.UUID, accessToken, refreshToken string, expiresAt time.Time) error

	// ExistsForUser checks whether the given (user_id, email) pair is already connected.
	ExistsForUser(ctx context.Context, userID, email string) (bool, *errx.Error)

	// CountForOrganization returns the number of email accounts attached to the
	// given organization. Used by the free-trial inbox cap.
	CountForOrganization(ctx context.Context, orgID uuid.UUID) (int, *errx.Error)

	// ListWarmupScheduleCandidates returns active mailboxes that should have a
	// running warmup chain but currently have no pending warmup task: either
	// actively warming, or backing a live campaign (the health-check lane).
	// Used by the warmup reconciler to (re)seed chains.
	ListWarmupScheduleCandidates(ctx context.Context, limit int) ([]uuid.UUID, error)

	// ListActiveWorkerAccounts returns the ids of every active mailbox. The
	// worker reconciler uses it to (re)load accounts onto their assigned workers
	// after onboarding, worker restarts, or reassignment.
	ListActiveWorkerAccounts(ctx context.Context) ([]uuid.UUID, error)
}

type emailRepository struct {
	DB      *db.DB
	Encrypt *encrypt.Encrypter
}

func NewEmailRepostory(db *db.DB) EmailRepository {
	return &emailRepository{
		DB: db,
	}
}

func (r *emailRepository) ExistsForUser(ctx context.Context, userID, email string) (bool, *errx.Error) {
	var exists bool
	query := `SELECT EXISTS(SELECT 1 FROM email_accounts WHERE user_id = $1 AND email = $2)`
	if err := r.DB.QueryRow(ctx, query, userID, email).Scan(&exists); err != nil {
		db.CaptureError(err, query, []any{userID, email}, "queryrow")
		return false, errx.InternalError()
	}
	return exists, nil
}

func (r *emailRepository) ListWarmupScheduleCandidates(ctx context.Context, limit int) ([]uuid.UUID, error) {
	query := `
		SELECT ea.id
		FROM email_accounts ea
		WHERE ea.status = 'active'
		  AND ea.worker_id IS NOT NULL
		  AND (
		    (ea.warmup IS NOT NULL AND ea.warmup_paused_at IS NULL)
		    OR EXISTS (
		      SELECT 1
		      FROM email_tags et
		      JOIN campaign_email_tags cet ON cet.tag_id = et.tag_id
		      JOIN campaigns c ON c.id = cet.campaign_id
		      WHERE et.email_id = ea.id AND c.status = 'active'
		    )
		  )
		  AND NOT EXISTS (
		    SELECT 1 FROM tasks t
		    WHERE t.email_account_id = ea.id
		      AND t.task_type = 'warmup'
		      AND t.status = 'pending'
		  )
		LIMIT $1`

	rows, err := r.DB.Query(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (r *emailRepository) ListActiveWorkerAccounts(ctx context.Context) ([]uuid.UUID, error) {
	const query = `SELECT id FROM email_accounts WHERE status = 'active'`
	rows, err := r.DB.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (r *emailRepository) CountForOrganization(ctx context.Context, orgID uuid.UUID) (int, *errx.Error) {
	var count int
	query := `SELECT COUNT(*) FROM email_accounts WHERE organization_id = $1`
	if err := r.DB.QueryRow(ctx, query, orgID).Scan(&count); err != nil {
		db.CaptureError(err, query, []any{orgID}, "queryrow")
		return 0, errx.InternalError()
	}
	return count, nil
}

func (r *emailRepository) NewOauthAccount(ctx context.Context, userID string, data models.NewOauthAccount) (*models.Email, *errx.Error) {
	if data.Provider == models.InboxProviderSMTPIMAP {
		sentry.CaptureException(errors.New("invalid inbox provider"))
		return nil, errx.InternalError()
	}

	tx, err := r.DB.Begin(ctx)
	if err != nil {
		db.CaptureError(err, "", nil, "begin")
		return nil, errx.InternalError()
	}
	defer tx.Rollback(ctx)

	sigplain := utils.GetSignaturePlain(data.Name)
	sightml := utils.GetSignatureHTML(data.Name)

	t := time.Now()
	id := uuid.New()

	// warmup_tag is the content segment (defaults to '' = generic). It used to
	// be seeded with a random RID, which silently broke segment-aware content
	// selection because a random tag never matches a real segment.
	query := `
		INSERT INTO email_accounts (id, user_id, organization_id, email, name, provider, signature_plain, signature_html, tracking_domain, last_synced_at, created_at, updated_at, warmup_tag)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $10, $10, $11)
	`

	params := []any{
		id,
		userID,
		data.OrganizationID,
		data.Email,
		data.Name,
		data.Provider,
		sigplain,
		sightml,
		"",
		t,
		"",
	}

	_, err = tx.Exec(
		ctx,
		query,
		params...,
	)
	if err != nil {
		db.CaptureError(err, query, nil, "queryrow")
		return nil, errx.InternalError()
	}

	query = `
		INSERT INTO email_accounts_oauth (email_account_id, access_token, refresh_token, expires_at)
		VALUES ($1, $2, $3, $4)
	`

	params = []any{
		id,
		data.AccessToken,
		data.RefreshToken,
		data.ExpiresAt,
	}

	_, err = tx.Exec(
		ctx,
		query,
		params...,
	)
	if err != nil {
		db.CaptureError(err, query, params, "exec")
		errx.InternalError()
	}

	if err := tx.Commit(ctx); err != nil {
		db.CaptureError(err, "", nil, "commit")
		return nil, errx.InternalError()
	}

	return &models.Email{
		ID:             id,
		UserID:         userID,
		OrganizationID: data.OrganizationID,
		Email:          data.Email,

		Name: data.Name,

		SignaturePlain: sigplain,
		SignatureHTML:  sightml,
		SignatureSync:  true,
		SignatureCode:  false,

		Provider: string(data.Provider),
		Status:   "active",

		LastSyncedAt: t,

		CampaignLimit: config.CampaignLimitDefault,
		MinWaitTime:   config.MinWaitTimeDefault,

		WarmupBase:      config.WarmupBaseDefault,
		WarmupMax:       config.WarmupMaxDefault,
		WarmupIncrease:  config.WarmupIncreaseDefault,
		WarmupStartTime: "08:00",
		WarmupEndTime:   "20:00",
		WarmupDays:      0,

		CreatedAt: t,
		UpdatedAt: t,
	}, nil
}

func (r *emailRepository) NewSMTPIMAPAccount(ctx context.Context, userID string, data models.NewSMTPIMAPAccount) (*models.Email, *errx.Error) {
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		db.CaptureError(err, "", nil, "begin")
		return nil, errx.InternalError()
	}
	defer tx.Rollback(ctx)

	sigplain := utils.GetSignaturePlain(data.Name)
	sightml := utils.GetSignatureHTML(data.Name)

	id := uuid.New()
	t := time.Now()

	query := `
		INSERT INTO email_accounts (id, user_id, organization_id, email, name, provider, signature_plain, signature_html, tracking_domain, last_synced_at, updated_at, created_at, warmup_tag)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $10, $10, $11)
	`
	params := []any{
		id,
		userID,
		data.OrganizationID,
		data.Email,
		data.Name,
		"smtp_imap",
		sigplain,
		sightml,
		"",
		t,
		"",
	}

	_, err = tx.Exec(
		ctx,
		query,
		params...,
	)
	if err != nil {
		db.CaptureError(err, query, nil, "exec")
		return nil, errx.InternalError()
	}

	smtphost, err := r.Encrypt.Encrypt(data.SMTP.Host)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}
	smtpuser, err := r.Encrypt.Encrypt(data.SMTP.Username)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}
	smtppass, err := r.Encrypt.Encrypt(data.SMTP.Password)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}

	imaphost, err := r.Encrypt.Encrypt(data.IMAP.Host)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}
	imapuser, err := r.Encrypt.Encrypt(data.IMAP.Username)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}
	imappass, err := r.Encrypt.Encrypt(data.IMAP.Password)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}

	query = `
		INSERT INTO email_accounts_smtp_imap (
		  email_account_id,
		  smtp_host, smtp_port, smtp_user, smtp_password,
		  imap_host, imap_port, imap_user, imap_password
		) VALUES (
		 $1, $2, $3, $4, $5, 
		 $6, $7, $8, $9)
	`

	params = []any{
		id, smtphost, data.SMTP.Port, smtpuser, smtppass,
		imaphost, data.IMAP.Port, imapuser, imappass,
	}

	_, err = tx.Exec(
		ctx,
		query,
		params...,
	)
	if err != nil {
		db.CaptureError(err, query, nil, "exec")
		return nil, errx.InternalError()
	}

	if err := tx.Commit(ctx); err != nil {
		db.CaptureError(err, "", nil, "commit")
		return nil, errx.InternalError()
	}

	return &models.Email{
		ID:             id,
		UserID:         userID,
		OrganizationID: data.OrganizationID,
		Email:          data.Email,

		Name: data.Name,

		SignaturePlain: sigplain,
		SignatureHTML:  sightml,
		SignatureSync:  true,
		SignatureCode:  false,

		Provider: "smtp_imap",
		Status:   "active",

		LastSyncedAt: t,

		CampaignLimit: config.CampaignLimitDefault,
		MinWaitTime:   config.MinWaitTimeDefault,

		WarmupBase:      config.WarmupBaseDefault,
		WarmupMax:       config.WarmupMaxDefault,
		WarmupIncrease:  config.WarmupIncreaseDefault,
		WarmupStartTime: "08:00",
		WarmupEndTime:   "20:00",
		WarmupDays:      0,

		CreatedAt: t,
		UpdatedAt: t,
	}, nil
}

func (r *emailRepository) Search(ctx context.Context, orgID, search string, cursor, tag *string, limit int32, allowedAccountIDs []uuid.UUID) (*models.EmailsResult, *errx.Error) {
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		db.CaptureError(err, "", nil, "begin")
		return nil, errx.InternalError()
	}
	// Read-only transaction — Commit is fine but Rollback at end is the
	// safety net. Pool only has 4 connections; a single leaked tx here
	// (under load) is enough to deadlock the whole backend, including
	// /auth/refresh which blocks waiting for a connection.
	defer tx.Rollback(ctx)

	query := `
		SELECT
		 ea.id, ea.email, ea.name, ea.signature_plain, ea.signature_html, ea.signature_sync, ea.signature_code,
	 	 ea.provider, ea.status, COALESCE(ea.last_synced_at, ea.created_at) AS last_synced_at, ea.last_id, ea.campaign_limit,
		 ea.min_wait_time, ea.reply_to, ea.tracking_domain, ea.tracking_domain_verified, ea.tracking_domain_verified_at, ea.warmup, ea.warmup_paused_at, ea.warmup_base,
		 ea.warmup_max, ea.warmup_increase, ea.warmup_start_time, ea.warmup_end_time, ea.warmup_days,
		 ea.created_at, ea.updated_at,
		 COALESCE(
			array_agg(eat.tag_id) FILTER (WHERE eat.tag_id IS NOT NULL), '{}'
		 ) AS tags
		FROM email_accounts ea
		LEFT JOIN email_tags eat ON eat.email_id = ea.id
		WHERE ea.organization_id = $1
		 AND ($2::uuid IS NULL OR (ea.created_at, ea.id) < (
		  SELECT created_at, id
		  FROM email_accounts
		  WHERE id = $2
		 ))
		 AND (ea.name ILIKE $3 OR ea.email ILIKE $3)
		 AND ($4::uuid IS NULL OR EXISTS (
		  SELECT 1 FROM email_tags cf WHERE cf.email_id = ea.id AND cf.tag_id = $4
		 ))
		 AND ($6::uuid[] IS NULL OR ea.id = ANY($6::uuid[]))
		GROUP BY ea.id
		ORDER BY ea.created_at DESC, ea.id DESC
		LIMIT $5
	`

	var allowedAccountParam any
	if len(allowedAccountIDs) > 0 {
		allowedAccountParam = allowedAccountIDs
	}
	params := []any{
		orgID,
		cursor,
		"%" + search + "%",
		tag,
		limit + 1,
		allowedAccountParam,
	}

	rows, err := tx.Query(ctx, query, params...)
	if err != nil {
		db.CaptureError(err, query, params, "query")
		return nil, errx.InternalError()
	}
	defer rows.Close()

	inboxes := make([]models.Email, 0)
	for rows.Next() {
		var i models.Email
		err := rows.Scan(
			&i.ID, &i.Email, &i.Name, &i.SignaturePlain, &i.SignatureHTML, &i.SignatureSync, &i.SignatureCode, &i.Provider, &i.Status,
			&i.LastSyncedAt, &i.LastID, &i.CampaignLimit, &i.MinWaitTime, &i.ReplyTo, &i.TrackingDomain, &i.TrackingDomainVerified, &i.TrackingDomainVerifiedAt,
			&i.Warmup, &i.WarmupPausedAt, &i.WarmupBase, &i.WarmupMax, &i.WarmupIncrease,
			&i.WarmupStartTime, &i.WarmupEndTime, &i.WarmupDays,
			&i.CreatedAt, &i.UpdatedAt, &i.Tags,
		)
		if err != nil {
			db.CaptureError(err, "", nil, "scan")
			return nil, errx.InternalError()
		}
		inboxes = append(inboxes, i)
	}

	var total *int64
	var nextCursor *string
	var hasMore bool

	if len(inboxes) > int(limit) {
		hasMore = true
		nextCursor = paging.EncodeUUID(inboxes[limit].ID)
		inboxes = inboxes[:limit]
	}

	if cursor == nil {
		query = `
			SELECT COUNT(DISTINCT ea.id)
			FROM email_accounts ea
			LEFT JOIN email_tags et ON et.email_id = ea.id
			WHERE ea.organization_id = $1
			  AND (ea.name ILIKE $2 OR ea.email ILIKE $2)
			  AND ($3::uuid IS NULL OR EXISTS (
				SELECT 1 FROM email_tags cf WHERE cf.email_id = ea.id AND cf.tag_id = $3
			  ))
			  AND ($4::uuid[] IS NULL OR ea.id = ANY($4::uuid[]))
		`

		params = []any{
			orgID,
			"%" + search + "%",
			tag,
			allowedAccountParam,
		}

		var tmp int64
		err := tx.QueryRow(
			ctx,
			query,
			params...,
		).Scan(&tmp)
		if err != nil {
			db.CaptureError(err, query, params, "queryrow")
			return nil, errx.InternalError()
		}
		total = &tmp
	}

	return &models.EmailsResult{
		Data: inboxes,
		Pagination: models.Pagination{
			Total:      total,
			NextCursor: nextCursor,
			HasMore:    hasMore,
		},
	}, nil
}

func (r *emailRepository) Get(ctx context.Context, orgID, emailAccountID string) (*models.Email, *errx.Error) {
	query := `
		SELECT
		ea.id, ea.email, ea.name, ea.signature_plain, ea.signature_html, ea.signature_sync, ea.signature_code,
		 ea.provider, ea.status, COALESCE(ea.last_synced_at, ea.created_at) AS last_synced_at, ea.last_id, ea.campaign_limit,
		 ea.min_wait_time, ea.reply_to, ea.tracking_domain, ea.tracking_domain_verified, ea.tracking_domain_verified_at, ea.warmup, ea.warmup_paused_at, ea.warmup_base,
		 ea.warmup_max, ea.warmup_increase, ea.warmup_start_time, ea.warmup_end_time, ea.warmup_days,
		 ea.created_at, ea.updated_at,
		 COALESCE(array_agg(eat.tag_id) FILTER (WHERE eat.tag_id IS NOT NULL), '{}') AS tags
		FROM email_accounts ea
		LEFT JOIN email_tags eat ON eat.email_id = ea.id
		WHERE ea.organization_id = $1 AND ea.id = $2
		GROUP BY ea.id
	`

	params := []any{
		orgID,
		emailAccountID,
	}

	var i models.Email
	err := r.DB.QueryRow(
		ctx,
		query,
		params...,
	).Scan(
		&i.ID, &i.Email, &i.Name, &i.SignaturePlain, &i.SignatureHTML, &i.SignatureSync, &i.SignatureCode, &i.Provider, &i.Status,
		&i.LastSyncedAt, &i.LastID, &i.CampaignLimit, &i.MinWaitTime, &i.ReplyTo, &i.TrackingDomain, &i.TrackingDomainVerified, &i.TrackingDomainVerifiedAt,
		&i.Warmup, &i.WarmupPausedAt, &i.WarmupBase, &i.WarmupMax, &i.WarmupIncrease,
		&i.WarmupStartTime, &i.WarmupEndTime, &i.WarmupDays,
		&i.CreatedAt, &i.UpdatedAt, &i.Tags,
	)
	if err != nil {
		db.CaptureError(err, query, params, "queryrow")
		return nil, errx.InternalError()
	}

	return &i, nil
}

func (r *emailRepository) Update(ctx context.Context, userID, emailAccountID string, udata *models.UpdateEmail) (*models.Email, *errx.Error) {
	setClauses := []string{}
	args := []any{userID, emailAccountID}
	argPos := 3

	if udata.Name != nil {
		if !validate.EmailName(udata.Name) {
			return nil, errx.ErrEmailName
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "name", argPos))
		args = append(args, *udata.Name)
		argPos++
	}
	if udata.SignaturePlain != nil {
		l := len(*udata.SignaturePlain)
		if l > 1000 {
			return nil, errx.ErrEmailSignaturePlain
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "signature_plain", argPos))
		args = append(args, *udata.SignaturePlain)
		argPos++
	}
	if udata.SignatureHTML != nil {
		l := len(*udata.SignatureHTML)
		if l > 1000 {
			return nil, errx.ErrEmailSignatureHTML
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "signature_html", argPos))
		args = append(args, *udata.SignatureHTML)
		argPos++
	}
	if udata.SignatureSync != nil {
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "signature_sync", argPos))
		args = append(args, *udata.SignatureSync)
		argPos++
	}
	if udata.SignatureCode != nil {
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "signature_code", argPos))
		args = append(args, *udata.SignatureCode)
		argPos++
	}
	if udata.Status != nil {
		// Validate status - must be one of: active, inactive, revoked
		status := *udata.Status
		if status != "active" && status != "inactive" && status != "revoked" {
			return nil, errx.ErrInvalid
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "status", argPos))
		args = append(args, status)
		argPos++
	}
	if udata.CampaignLimit != nil {
		if *udata.CampaignLimit < 0 || *udata.CampaignLimit > 100 {
			return nil, errx.ErrEmailCampaignLimit
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "campaign_limit", argPos))
		args = append(args, *udata.CampaignLimit)
		argPos++
	}
	if udata.MinWaitTime != nil {
		if *udata.MinWaitTime < 0 || *udata.MinWaitTime > 86400 {
			return nil, errx.ErrEmailMinWaitTime
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "min_wait_time", argPos))
		args = append(args, *udata.MinWaitTime)
		argPos++
	}
	if udata.ReplyTo != nil {
		*udata.ReplyTo = strings.TrimSpace(*udata.ReplyTo)
		if *udata.ReplyTo != "" && !validate.Email(*udata.ReplyTo) {
			return nil, errx.ErrEmail
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "reply_to", argPos))
		args = append(args, *udata.ReplyTo)
		argPos++
	}
	// Cross-field guard: starting volume must not exceed the ceiling. Only
	// enforced when both arrive together (the warmup form always sends both);
	// each is still independently clamped below.
	if udata.WarmupBase != nil && udata.WarmupMax != nil && *udata.WarmupBase > *udata.WarmupMax {
		return nil, errx.ErrEmailWarmupBase
	}
	if udata.Warmup != nil {
		var warmupTime *time.Time
		if *udata.Warmup {
			t := time.Now()
			warmupTime = &t
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "warmup", argPos))
		args = append(args, warmupTime)
		argPos++
		// A direct warmup on/off via PATCH always clears the pause marker so
		// state stays coherent (enable = fresh ramp, disable = off). Pause and
		// resume that preserve ramp progress go through the lifecycle endpoints.
		setClauses = append(setClauses, "warmup_paused_at = NULL")
	}
	if udata.WarmupBase != nil {
		if *udata.WarmupBase < 0 || *udata.WarmupBase > 100 {
			return nil, errx.ErrEmailWarmupBase
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "warmup_base", argPos))
		args = append(args, *udata.WarmupBase)
		argPos++
	}
	if udata.WarmupMax != nil {
		if *udata.WarmupMax < 0 || *udata.WarmupMax > 100 {
			return nil, errx.ErrEmailWarmupMax
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "warmup_max", argPos))
		args = append(args, *udata.WarmupMax)
		argPos++
	}
	if udata.WarmupIncrease != nil {
		if *udata.WarmupIncrease < 0 || *udata.WarmupIncrease > 100 {
			return nil, errx.ErrEmailWarmupIncrease
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "warmup_increase", argPos))
		args = append(args, *udata.WarmupIncrease)
		argPos++
	}
	if udata.WarmupReplyRate != nil {
		if *udata.WarmupReplyRate < 0 || *udata.WarmupReplyRate > 100 {
			return nil, errx.ErrEmailReplyRate
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "warmup_reply_rate", argPos))
		args = append(args, *udata.WarmupReplyRate)
		argPos++
	}
	if udata.WarmupTag != nil {
		// warmup_tag is the content segment: a lowercase slug (e.g. "saas",
		// "agency") that the segment-aware AI content bank is keyed on. Empty
		// = generic content. Reject anything that isn't a simple slug so it
		// can't smuggle arbitrary text into the content-selection path.
		seg := strings.ToLower(strings.TrimSpace(*udata.WarmupTag))
		if len(seg) > 32 {
			return nil, errx.ErrInvalid
		}
		for _, r := range seg {
			if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-') {
				return nil, errx.ErrInvalid
			}
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "warmup_tag", argPos))
		args = append(args, seg)
		argPos++
	}
	if udata.WarmupStartTime != nil {
		if err := validate.CampaignTime(*udata.WarmupStartTime); err != nil {
			return nil, err
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "warmup_start_time", argPos))
		args = append(args, *udata.WarmupStartTime)
		argPos++
	}
	if udata.WarmupEndTime != nil {
		if err := validate.CampaignTime(*udata.WarmupEndTime); err != nil {
			return nil, err
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "warmup_end_time", argPos))
		args = append(args, *udata.WarmupEndTime)
		argPos++
	}
	if udata.WarmupDays != nil {
		if *udata.WarmupDays < 0 || *udata.WarmupDays > 127 {
			return nil, errx.ErrInvalid
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "warmup_days", argPos))
		args = append(args, *udata.WarmupDays)
		argPos++
	}

	if argPos == 3 {
		return nil, errx.ErrNotEnough
	}

	setClauses = append(setClauses, "updated_at = now()")

	tx, err := r.DB.Begin(ctx)
	if err != nil {
		db.CaptureError(err, "", nil, "begin")
		return nil, errx.InternalError()
	}
	defer tx.Rollback(ctx)

	query := fmt.Sprintf(`
		UPDATE email_accounts
		SET %s
		WHERE user_id = $1 AND id = $2
		RETURNING id, organization_id, email, name, signature_plain, signature_html, signature_sync, signature_code, provider, status,
		          COALESCE(last_synced_at, created_at) AS last_synced_at, last_id, campaign_limit, min_wait_time, reply_to, tracking_domain, tracking_domain_verified, tracking_domain_verified_at,
		          warmup, warmup_paused_at, warmup_base, warmup_max, warmup_increase, warmup_reply_rate, warmup_tag, warmup_pool_type,
		          warmup_start_time, warmup_end_time, warmup_days, created_at, updated_at
	`, strings.Join(setClauses, ", "))

	var i models.Email
	err = tx.QueryRow(ctx, query, args...).Scan(
		&i.ID, &i.OrganizationID, &i.Email, &i.Name, &i.SignaturePlain, &i.SignatureHTML, &i.SignatureSync, &i.SignatureCode, &i.Provider, &i.Status,
		&i.LastSyncedAt, &i.LastID, &i.CampaignLimit, &i.MinWaitTime, &i.ReplyTo, &i.TrackingDomain, &i.TrackingDomainVerified, &i.TrackingDomainVerifiedAt,
		&i.Warmup, &i.WarmupPausedAt, &i.WarmupBase, &i.WarmupMax, &i.WarmupIncrease, &i.WarmupReplyRate, &i.WarmupTag, &i.WarmupPoolType,
		&i.WarmupStartTime, &i.WarmupEndTime, &i.WarmupDays,
		&i.CreatedAt, &i.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errx.ErrNotFound
		}
		db.CaptureError(err, query, args, "queryrow")
		return nil, errx.InternalError()
	}
	i.Tags = make([]string, 0)
	if udata.Tags != nil {
		var err *errx.Error
		i.Tags, err = SyncEmailTags(ctx, tx, emailAccountID, udata.Tags)
		if err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		db.CaptureError(err, "", nil, "commit")
		return nil, errx.InternalError()
	}

	return &i, nil
}

func (r *emailRepository) UpdateTrackingDomain(ctx context.Context, userID, emailAccountID, domain string, verified bool, verifiedAt *time.Time) *errx.Error {
	query := `
		UPDATE email_accounts
		SET tracking_domain = $1, tracking_domain_verified = $2, tracking_domain_verified_at = $3
		WHERE user_id = $4 AND id = $5
	`

	params := []any{
		domain,
		verified,
		verifiedAt,
		userID,
		emailAccountID,
	}

	cmd, err := r.DB.Exec(
		ctx,
		query,
		params...,
	)
	if err != nil {
		db.CaptureError(err, query, params, "exec")
		return errx.InternalError()
	}
	if cmd.RowsAffected() == 0 {
		return errx.ErrNotFound
	}
	return nil
}

func (r *emailRepository) Delete(ctx context.Context, userID, emailAccountID string) *errx.Error {
	query := `
		DELETE FROM email_accounts
		WHERE user_id = $1 AND id = $2
	`

	params := []any{
		userID,
		emailAccountID,
	}

	cmd, err := r.DB.Exec(
		ctx,
		query,
		params...,
	)
	if err != nil {
		db.CaptureError(err, query, params, "exec")
		return errx.InternalError()
	}
	if cmd.RowsAffected() == 0 {
		return errx.ErrNotFound
	}
	return nil
}

// GetByID retrieves an email account by ID without requiring userID (for internal service use)
// SetWarmupLifecycle applies a warmup lifecycle transition. The CASE math
// keeps every transition atomic and idempotent:
//
//   - start/resume: a fresh mailbox (warmup IS NULL) anchors at now(); a
//     paused mailbox shifts its anchor forward by the paused duration so the
//     ramp continues where it left off; an already-active mailbox is a no-op.
//   - pause: stamps warmup_paused_at only while actively warming.
//   - disable: clears warmup entirely (next restart begins a fresh ramp).
func (r *emailRepository) SetWarmupLifecycle(ctx context.Context, userID, emailAccountID, action string) (*models.Email, *errx.Error) {
	var setClause string
	switch action {
	case "start", "resume":
		setClause = `warmup = CASE
				WHEN warmup IS NULL THEN now()
				WHEN warmup_paused_at IS NOT NULL THEN warmup + (now() - warmup_paused_at)
				ELSE warmup
			END,
			warmup_paused_at = NULL`
	case "pause":
		setClause = `warmup_paused_at = CASE
				WHEN warmup IS NOT NULL AND warmup_paused_at IS NULL THEN now()
				ELSE warmup_paused_at
			END`
	case "disable", "stop":
		setClause = `warmup = NULL, warmup_paused_at = NULL`
	default:
		return nil, errx.ErrInvalid
	}

	query := fmt.Sprintf(`
		UPDATE email_accounts
		SET %s, updated_at = now()
		WHERE user_id = $1 AND id = $2
	`, setClause)

	tag, err := r.DB.Exec(ctx, query, userID, emailAccountID)
	if err != nil {
		db.CaptureError(err, query, []any{userID, emailAccountID}, "exec")
		return nil, errx.InternalError()
	}
	if tag.RowsAffected() == 0 {
		return nil, errx.ErrNotFound
	}

	return r.Get(ctx, userID, emailAccountID)
}

func (r *emailRepository) GetByID(ctx context.Context, emailAccountID uuid.UUID) (*models.Email, *errx.Error) {
	query := `
		SELECT
		 ea.id, ea.user_id, ea.organization_id, ea.email, ea.name, ea.signature_plain, ea.signature_html, ea.signature_sync, ea.signature_code,
		 ea.provider, ea.status, COALESCE(ea.last_synced_at, ea.created_at) AS last_synced_at, ea.last_id, ea.campaign_limit,
		 ea.min_wait_time, ea.reply_to, ea.tracking_domain, ea.tracking_domain_verified, ea.tracking_domain_verified_at, ea.warmup, ea.warmup_paused_at, ea.warmup_base,
		 ea.warmup_max, ea.warmup_increase, ea.warmup_reply_rate, ea.warmup_tag, ea.warmup_pool_type,
		 ea.warmup_start_time, ea.warmup_end_time, ea.warmup_days, ea.timezone,
		 ea.created_at, ea.updated_at,
		 COALESCE(array_agg(eat.tag_id) FILTER (WHERE eat.tag_id IS NOT NULL), '{}') AS tags
		FROM email_accounts ea
		LEFT JOIN email_tags eat ON eat.email_id = ea.id
		WHERE ea.id = $1
		GROUP BY ea.id
	`

	var i models.Email
	err := r.DB.QueryRow(ctx, query, emailAccountID).Scan(
		&i.ID, &i.UserID, &i.OrganizationID, &i.Email, &i.Name, &i.SignaturePlain, &i.SignatureHTML, &i.SignatureSync, &i.SignatureCode,
		&i.Provider, &i.Status, &i.LastSyncedAt, &i.LastID, &i.CampaignLimit,
		&i.MinWaitTime, &i.ReplyTo, &i.TrackingDomain, &i.TrackingDomainVerified, &i.TrackingDomainVerifiedAt, &i.Warmup, &i.WarmupPausedAt, &i.WarmupBase,
		&i.WarmupMax, &i.WarmupIncrease, &i.WarmupReplyRate, &i.WarmupTag, &i.WarmupPoolType,
		&i.WarmupStartTime, &i.WarmupEndTime, &i.WarmupDays, &i.Timezone,
		&i.CreatedAt, &i.UpdatedAt, &i.Tags,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errx.ErrNotFound
		}
		db.CaptureError(err, query, []any{emailAccountID}, "queryrow")
		return nil, errx.InternalError()
	}

	return &i, nil
}

// GetByTags retrieves email accounts matching any of the specified tags
func (r *emailRepository) GetByTags(ctx context.Context, userID string, tags []string) ([]models.Email, *errx.Error) {
	if len(tags) == 0 {
		return []models.Email{}, nil
	}

	query := `
		SELECT DISTINCT ON (ea.id)
		 ea.id, ea.user_id, ea.email, ea.name, ea.signature_plain, ea.signature_html, ea.signature_sync, ea.signature_code,
		 ea.provider, ea.status, COALESCE(ea.last_synced_at, ea.created_at) AS last_synced_at, ea.last_id, ea.campaign_limit,
		 ea.min_wait_time, ea.reply_to, ea.tracking_domain, ea.tracking_domain_verified, ea.tracking_domain_verified_at, ea.warmup, ea.warmup_paused_at, ea.warmup_base,
		 ea.warmup_max, ea.warmup_increase, ea.warmup_reply_rate, ea.warmup_tag,
		 ea.warmup_start_time, ea.warmup_end_time, ea.warmup_days, ea.timezone,
		 ea.created_at, ea.updated_at
		FROM email_accounts ea
		JOIN email_tags eat ON eat.email_id = ea.id
		WHERE ea.user_id = $1
		  AND eat.tag_id = ANY($2)
		  AND ea.status = 'active'
		ORDER BY ea.id
	`

	rows, err := r.DB.Query(ctx, query, userID, tags)
	if err != nil {
		db.CaptureError(err, query, []any{userID, tags}, "query")
		return nil, errx.InternalError()
	}
	defer rows.Close()

	var emails []models.Email
	for rows.Next() {
		var i models.Email
		err := rows.Scan(
			&i.ID, &i.UserID, &i.Email, &i.Name, &i.SignaturePlain, &i.SignatureHTML, &i.SignatureSync, &i.SignatureCode,
			&i.Provider, &i.Status, &i.LastSyncedAt, &i.LastID, &i.CampaignLimit,
			&i.MinWaitTime, &i.ReplyTo, &i.TrackingDomain, &i.TrackingDomainVerified, &i.TrackingDomainVerifiedAt, &i.Warmup, &i.WarmupPausedAt, &i.WarmupBase,
			&i.WarmupMax, &i.WarmupIncrease, &i.WarmupReplyRate, &i.WarmupTag,
			&i.WarmupStartTime, &i.WarmupEndTime, &i.WarmupDays, &i.Timezone,
			&i.CreatedAt, &i.UpdatedAt,
		)
		if err != nil {
			db.CaptureError(err, "", nil, "scan")
			return nil, errx.InternalError()
		}
		i.Tags = []string{} // Tags not fetched in this query
		emails = append(emails, i)
	}

	return emails, nil
}

// GetAllActiveByUser returns every active mailbox for a user (the "all" sender
// pool). Same projection as GetByTags, without the tag join.
func (r *emailRepository) GetAllActiveByUser(ctx context.Context, userID string) ([]models.Email, *errx.Error) {
	query := `
		SELECT
		 ea.id, ea.user_id, ea.email, ea.name, ea.signature_plain, ea.signature_html, ea.signature_sync, ea.signature_code,
		 ea.provider, ea.status, COALESCE(ea.last_synced_at, ea.created_at) AS last_synced_at, ea.last_id, ea.campaign_limit,
		 ea.min_wait_time, ea.reply_to, ea.tracking_domain, ea.tracking_domain_verified, ea.tracking_domain_verified_at, ea.warmup, ea.warmup_paused_at, ea.warmup_base,
		 ea.warmup_max, ea.warmup_increase, ea.warmup_reply_rate, ea.warmup_tag,
		 ea.warmup_start_time, ea.warmup_end_time, ea.warmup_days, ea.timezone,
		 ea.created_at, ea.updated_at
		FROM email_accounts ea
		WHERE ea.user_id = $1
		  AND ea.status = 'active'
		ORDER BY ea.id
	`

	rows, err := r.DB.Query(ctx, query, userID)
	if err != nil {
		db.CaptureError(err, query, []any{userID}, "query")
		return nil, errx.InternalError()
	}
	defer rows.Close()

	var emails []models.Email
	for rows.Next() {
		var i models.Email
		err := rows.Scan(
			&i.ID, &i.UserID, &i.Email, &i.Name, &i.SignaturePlain, &i.SignatureHTML, &i.SignatureSync, &i.SignatureCode,
			&i.Provider, &i.Status, &i.LastSyncedAt, &i.LastID, &i.CampaignLimit,
			&i.MinWaitTime, &i.ReplyTo, &i.TrackingDomain, &i.TrackingDomainVerified, &i.TrackingDomainVerifiedAt, &i.Warmup, &i.WarmupPausedAt, &i.WarmupBase,
			&i.WarmupMax, &i.WarmupIncrease, &i.WarmupReplyRate, &i.WarmupTag,
			&i.WarmupStartTime, &i.WarmupEndTime, &i.WarmupDays, &i.Timezone,
			&i.CreatedAt, &i.UpdatedAt,
		)
		if err != nil {
			db.CaptureError(err, "", nil, "scan")
			return nil, errx.InternalError()
		}
		i.Tags = []string{}
		emails = append(emails, i)
	}

	return emails, nil
}

// CampaignSenderAccount pairs an active sender mailbox with its per-campaign
// rotation metadata, so the scheduler's rotation modes (weighted / round_robin
// / least_recently_used) can pick among them without a second query.
type CampaignSenderAccount struct {
	Account          models.Email
	Weight           int
	RotationPosition int
	LastSentAt       *time.Time
}

// GetByCampaignSenders mirrors GetByTags but resolves accounts through the
// explicit campaign_senders pool instead of email tags. Only enabled senders
// backing an active mailbox are returned; the per-sender weight/cursor/last-send
// ride along for rotation.
func (r *emailRepository) GetByCampaignSenders(ctx context.Context, userID string, campaignID uuid.UUID) ([]CampaignSenderAccount, *errx.Error) {
	query := `
		SELECT
		 ea.id, ea.user_id, ea.email, ea.name, ea.signature_plain, ea.signature_html, ea.signature_sync, ea.signature_code,
		 ea.provider, ea.status, COALESCE(ea.last_synced_at, ea.created_at) AS last_synced_at, ea.last_id, ea.campaign_limit,
		 ea.min_wait_time, ea.reply_to, ea.tracking_domain, ea.tracking_domain_verified, ea.tracking_domain_verified_at, ea.warmup, ea.warmup_paused_at, ea.warmup_base,
		 ea.warmup_max, ea.warmup_increase, ea.warmup_reply_rate, ea.warmup_tag,
		 ea.warmup_start_time, ea.warmup_end_time, ea.warmup_days, ea.timezone,
		 ea.created_at, ea.updated_at,
		 cs.weight, cs.rotation_position, cs.last_sent_at
		FROM email_accounts ea
		JOIN campaign_senders cs ON cs.email_account_id = ea.id
		WHERE cs.campaign_id = $2
		  AND cs.enabled
		  AND ea.user_id = $1
		  AND ea.status = 'active'
		ORDER BY ea.id
	`

	rows, err := r.DB.Query(ctx, query, userID, campaignID)
	if err != nil {
		db.CaptureError(err, query, []any{userID, campaignID}, "query")
		return nil, errx.InternalError()
	}
	defer rows.Close()

	var out []CampaignSenderAccount
	for rows.Next() {
		var i models.Email
		var sender CampaignSenderAccount
		err := rows.Scan(
			&i.ID, &i.UserID, &i.Email, &i.Name, &i.SignaturePlain, &i.SignatureHTML, &i.SignatureSync, &i.SignatureCode,
			&i.Provider, &i.Status, &i.LastSyncedAt, &i.LastID, &i.CampaignLimit,
			&i.MinWaitTime, &i.ReplyTo, &i.TrackingDomain, &i.TrackingDomainVerified, &i.TrackingDomainVerifiedAt, &i.Warmup, &i.WarmupPausedAt, &i.WarmupBase,
			&i.WarmupMax, &i.WarmupIncrease, &i.WarmupReplyRate, &i.WarmupTag,
			&i.WarmupStartTime, &i.WarmupEndTime, &i.WarmupDays, &i.Timezone,
			&i.CreatedAt, &i.UpdatedAt,
			&sender.Weight, &sender.RotationPosition, &sender.LastSentAt,
		)
		if err != nil {
			db.CaptureError(err, "", nil, "scan")
			return nil, errx.InternalError()
		}
		i.Tags = []string{}
		sender.Account = i
		out = append(out, sender)
	}
	if err := rows.Err(); err != nil {
		db.CaptureError(err, "", nil, "rows")
		return nil, errx.InternalError()
	}

	return out, nil
}

// GetSMTPCredentials retrieves SMTP/IMAP credentials for an email account
func (r *emailRepository) GetSMTPCredentials(ctx context.Context, emailAccountID uuid.UUID) (*SMTPCredentials, *errx.Error) {
	query := `
		SELECT smtp_host, smtp_port, smtp_user, smtp_password,
		       imap_host, imap_port, imap_user, imap_password
		FROM email_accounts_smtp_imap
		WHERE email_account_id = $1
	`

	var creds SMTPCredentials
	var smtpHost, smtpUser, smtpPassword, imapHost, imapUser, imapPassword string

	err := r.DB.QueryRow(ctx, query, emailAccountID).Scan(
		&smtpHost, &creds.SMTPPort, &smtpUser, &smtpPassword,
		&imapHost, &creds.IMAPPort, &imapUser, &imapPassword,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errx.ErrNotFound
		}
		db.CaptureError(err, query, []any{emailAccountID}, "queryrow")
		return nil, errx.InternalError()
	}

	// Decrypt credentials
	var xerr error
	creds.SMTPHost, xerr = r.Encrypt.Decrypt(smtpHost)
	if xerr != nil {
		sentry.CaptureException(xerr)
		return nil, errx.InternalError()
	}
	creds.SMTPUser, xerr = r.Encrypt.Decrypt(smtpUser)
	if xerr != nil {
		sentry.CaptureException(xerr)
		return nil, errx.InternalError()
	}
	creds.SMTPPassword, xerr = r.Encrypt.Decrypt(smtpPassword)
	if xerr != nil {
		sentry.CaptureException(xerr)
		return nil, errx.InternalError()
	}
	creds.IMAPHost, xerr = r.Encrypt.Decrypt(imapHost)
	if xerr != nil {
		sentry.CaptureException(xerr)
		return nil, errx.InternalError()
	}
	creds.IMAPUser, xerr = r.Encrypt.Decrypt(imapUser)
	if xerr != nil {
		sentry.CaptureException(xerr)
		return nil, errx.InternalError()
	}
	creds.IMAPPassword, xerr = r.Encrypt.Decrypt(imapPassword)
	if xerr != nil {
		sentry.CaptureException(xerr)
		return nil, errx.InternalError()
	}

	return &creds, nil
}

// GetOAuthCredentials retrieves OAuth credentials for an email account
func (r *emailRepository) GetOAuthCredentials(ctx context.Context, emailAccountID uuid.UUID) (*OAuthCredentials, *errx.Error) {
	query := `
		SELECT access_token, refresh_token, expires_at
		FROM email_accounts_oauth
		WHERE email_account_id = $1
	`

	var accessToken, refreshToken string
	var expiresAt time.Time

	err := r.DB.QueryRow(ctx, query, emailAccountID).Scan(
		&accessToken, &refreshToken, &expiresAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errx.ErrNotFound
		}
		db.CaptureError(err, query, []any{emailAccountID}, "queryrow")
		return nil, errx.InternalError()
	}

	// Decrypt tokens
	var xerr error
	decryptedAccessToken, xerr := r.Encrypt.Decrypt(accessToken)
	if xerr != nil {
		sentry.CaptureException(xerr)
		return nil, errx.InternalError()
	}
	decryptedRefreshToken, xerr := r.Encrypt.Decrypt(refreshToken)
	if xerr != nil {
		sentry.CaptureException(xerr)
		return nil, errx.InternalError()
	}

	return &OAuthCredentials{
		AccessToken:  decryptedAccessToken,
		RefreshToken: decryptedRefreshToken,
		ExpiresAt:    expiresAt,
	}, nil
}

// GetWorkerID retrieves the worker ID assigned to an email account
func (r *emailRepository) GetWorkerID(ctx context.Context, emailAccountID uuid.UUID) (*uuid.UUID, *errx.Error) {
	query := `SELECT worker_id FROM email_accounts WHERE id = $1`

	var workerID *uuid.UUID
	err := r.DB.QueryRow(ctx, query, emailAccountID).Scan(&workerID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errx.ErrNotFound
		}
		db.CaptureError(err, query, []any{emailAccountID}, "queryrow")
		return nil, errx.InternalError()
	}

	return workerID, nil
}

// SetWorkerID assigns a worker to an email account
func (r *emailRepository) SetWorkerID(ctx context.Context, emailAccountID, workerID uuid.UUID) *errx.Error {
	query := `UPDATE email_accounts SET worker_id = $1, updated_at = NOW() WHERE id = $2`

	cmd, err := r.DB.Exec(ctx, query, workerID, emailAccountID)
	if err != nil {
		db.CaptureError(err, query, []any{workerID, emailAccountID}, "exec")
		return errx.InternalError()
	}
	if cmd.RowsAffected() == 0 {
		return errx.ErrNotFound
	}

	return nil
}
