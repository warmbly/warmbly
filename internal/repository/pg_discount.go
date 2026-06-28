package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/utils/paging"
)

// Sentinel errors so the service layer can map redemption capacity failures to
// friendly, user-facing messages instead of a generic internal error.
var (
	ErrDiscountExhausted       = errors.New("discount code redemption limit reached")
	ErrDiscountAlreadyRedeemed = errors.New("discount code already redeemed by organization")
)

const discountCodeColumns = `
	id, code, description, type, percent_off, amount_off, currency, trial_extension_days,
	duration, duration_in_months, max_redemptions, times_redeemed, per_account_limit,
	applies_to_all_plans, status, starts_at, expires_at, created_by, created_at, updated_at
`

// DiscountCodeRepository owns the discount_codes table and its plan eligibility
// join table.
type DiscountCodeRepository interface {
	Create(ctx context.Context, code *models.DiscountCode) error
	Update(ctx context.Context, code *models.DiscountCode) error
	GetByID(ctx context.Context, id uuid.UUID) (*models.DiscountCode, error)
	GetByCode(ctx context.Context, code string) (*models.DiscountCode, error)
	List(ctx context.Context, search *models.AdminDiscountSearch) (*models.AdminDiscountsResult, error)
	Delete(ctx context.Context, id uuid.UUID) error
}

type discountCodeRepository struct {
	db *pgxpool.Pool
}

func NewDiscountCodeRepository(db *pgxpool.Pool) DiscountCodeRepository {
	return &discountCodeRepository{db: db}
}

func (r *discountCodeRepository) Create(ctx context.Context, code *models.DiscountCode) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	now := time.Now()
	_, err = tx.Exec(ctx, `
		INSERT INTO discount_codes (
			id, code, description, type, percent_off, amount_off, currency, trial_extension_days,
			duration, duration_in_months, max_redemptions, times_redeemed, per_account_limit,
			applies_to_all_plans, status, starts_at, expires_at, created_by, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20
		)
	`,
		code.ID, code.Code, code.Description, code.Type, code.PercentOff, code.AmountOff, code.Currency, code.TrialExtensionDays,
		code.Duration, code.DurationInMonths, code.MaxRedemptions, code.TimesRedeemed, code.PerAccountLimit,
		code.AppliesToAllPlans, code.Status, code.StartsAt, code.ExpiresAt, code.CreatedBy, now, now,
	)
	if err != nil {
		return err
	}

	for _, planID := range code.PlanIDs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO discount_code_plans (discount_code_id, plan_id) VALUES ($1, $2)
			ON CONFLICT DO NOTHING
		`, code.ID, planID); err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

func (r *discountCodeRepository) Update(ctx context.Context, code *models.DiscountCode) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `
		UPDATE discount_codes SET
			description = $2,
			percent_off = $3,
			amount_off = $4,
			currency = $5,
			trial_extension_days = $6,
			duration = $7,
			duration_in_months = $8,
			max_redemptions = $9,
			per_account_limit = $10,
			applies_to_all_plans = $11,
			status = $12,
			starts_at = $13,
			expires_at = $14,
			updated_at = NOW()
		WHERE id = $1
	`,
		code.ID, code.Description, code.PercentOff, code.AmountOff, code.Currency, code.TrialExtensionDays,
		code.Duration, code.DurationInMonths, code.MaxRedemptions, code.PerAccountLimit,
		code.AppliesToAllPlans, code.Status, code.StartsAt, code.ExpiresAt,
	)
	if err != nil {
		return err
	}

	// Replace plan eligibility wholesale.
	if _, err := tx.Exec(ctx, `DELETE FROM discount_code_plans WHERE discount_code_id = $1`, code.ID); err != nil {
		return err
	}
	for _, planID := range code.PlanIDs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO discount_code_plans (discount_code_id, plan_id) VALUES ($1, $2)
			ON CONFLICT DO NOTHING
		`, code.ID, planID); err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

func (r *discountCodeRepository) GetByID(ctx context.Context, id uuid.UUID) (*models.DiscountCode, error) {
	code, err := r.scanCode(ctx, `SELECT `+discountCodeColumns+` FROM discount_codes WHERE id = $1`, id)
	if err != nil || code == nil {
		return code, err
	}
	planIDs, err := r.planIDs(ctx, code.ID)
	if err != nil {
		return nil, err
	}
	code.PlanIDs = planIDs
	return code, nil
}

func (r *discountCodeRepository) GetByCode(ctx context.Context, codeStr string) (*models.DiscountCode, error) {
	code, err := r.scanCode(ctx, `SELECT `+discountCodeColumns+` FROM discount_codes WHERE code = $1`, codeStr)
	if err != nil || code == nil {
		return code, err
	}
	planIDs, err := r.planIDs(ctx, code.ID)
	if err != nil {
		return nil, err
	}
	code.PlanIDs = planIDs
	return code, nil
}

func (r *discountCodeRepository) scanCode(ctx context.Context, query string, args ...interface{}) (*models.DiscountCode, error) {
	row := r.db.QueryRow(ctx, query, args...)
	var c models.DiscountCode
	err := row.Scan(
		&c.ID, &c.Code, &c.Description, &c.Type, &c.PercentOff, &c.AmountOff, &c.Currency, &c.TrialExtensionDays,
		&c.Duration, &c.DurationInMonths, &c.MaxRedemptions, &c.TimesRedeemed, &c.PerAccountLimit,
		&c.AppliesToAllPlans, &c.Status, &c.StartsAt, &c.ExpiresAt, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	c.PlanIDs = []uuid.UUID{}
	return &c, nil
}

func (r *discountCodeRepository) planIDs(ctx context.Context, codeID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := r.db.Query(ctx, `SELECT plan_id FROM discount_code_plans WHERE discount_code_id = $1`, codeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (r *discountCodeRepository) List(ctx context.Context, search *models.AdminDiscountSearch) (*models.AdminDiscountsResult, error) {
	limit := search.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	// Aliased column list so EXISTS subqueries can reference dc.id cleanly
	// (discountCodeColumns is bare/unaliased and still used by GetByID/GetByCode).
	const dcCols = `dc.id, dc.code, dc.description, dc.type, dc.percent_off, dc.amount_off, dc.currency, dc.trial_extension_days,
		dc.duration, dc.duration_in_months, dc.max_redemptions, dc.times_redeemed, dc.per_account_limit,
		dc.applies_to_all_plans, dc.status, dc.starts_at, dc.expires_at, dc.created_by, dc.created_at, dc.updated_at`

	args := []interface{}{}
	argNum := 1
	whereClause := "WHERE 1=1"

	if search.Search != "" {
		whereClause += " AND (dc.code ILIKE $" + itoa(argNum) + " OR dc.description ILIKE $" + itoa(argNum) + ")"
		args = append(args, "%"+search.Search+"%")
		argNum++
	}
	// Enum facets: allowlist known values so a bogus value yields no clause
	// (returns all) rather than a Postgres enum-cast 500.
	switch search.Status {
	case "active", "disabled", "expired":
		whereClause += " AND dc.status = $" + itoa(argNum)
		args = append(args, search.Status)
		argNum++
	}
	switch search.Type {
	case "percent", "fixed", "trial_extension":
		whereClause += " AND dc.type = $" + itoa(argNum)
		args = append(args, search.Type)
		argNum++
	}
	switch search.Duration {
	case "once", "repeating", "forever":
		whereClause += " AND dc.duration = $" + itoa(argNum)
		args = append(args, search.Duration)
		argNum++
	}
	switch search.PlanScope {
	case "all":
		whereClause += " AND dc.applies_to_all_plans = TRUE"
	case "specific":
		whereClause += " AND dc.applies_to_all_plans = FALSE"
	}
	if search.PlanID != nil {
		whereClause += " AND EXISTS (SELECT 1 FROM discount_code_plans dcp WHERE dcp.discount_code_id = dc.id AND dcp.plan_id = $" + itoa(argNum) + ")"
		args = append(args, *search.PlanID)
		argNum++
	}
	if search.HasRedemptions {
		whereClause += " AND EXISTS (SELECT 1 FROM discount_redemptions dr WHERE dr.discount_code_id = dc.id AND dr.status IN ('pending','applied'))"
	}
	if search.HasMaxRedemptions {
		whereClause += " AND dc.max_redemptions IS NOT NULL"
	}
	if search.Exhausted {
		whereClause += " AND dc.max_redemptions IS NOT NULL AND dc.times_redeemed >= dc.max_redemptions"
	}
	if search.HasExpiry {
		whereClause += " AND dc.expires_at IS NOT NULL"
	}

	addInt := func(frag string, v *int) {
		if v != nil {
			whereClause += " AND " + fmt.Sprintf(frag, argNum)
			args = append(args, *v)
			argNum++
		}
	}
	addAfter := func(col string, v *time.Time) {
		if v != nil {
			whereClause += " AND " + col + " >= $" + itoa(argNum)
			args = append(args, *v)
			argNum++
		}
	}
	addBefore := func(col string, v *time.Time) {
		if v != nil {
			whereClause += " AND " + col + " < ($" + itoa(argNum) + " + INTERVAL '1 day')"
			args = append(args, *v)
			argNum++
		}
	}

	addInt(`dc.times_redeemed >= $%d`, search.TimesRedeemedMin)
	addInt(`dc.times_redeemed <= $%d`, search.TimesRedeemedMax)
	addInt(`dc.percent_off >= $%d`, search.PercentOffMin)
	addInt(`dc.percent_off <= $%d`, search.PercentOffMax)

	if search.CreatedWithin > 0 {
		whereClause += " AND dc.created_at >= NOW() - ($" + itoa(argNum) + "::int * INTERVAL '1 day')"
		args = append(args, search.CreatedWithin)
		argNum++
	}
	addAfter("dc.created_at", search.CreatedAfter)
	addBefore("dc.created_at", search.CreatedBefore)
	addAfter("dc.starts_at", search.StartsAfter)
	addBefore("dc.starts_at", search.StartsBefore)
	addAfter("dc.expires_at", search.ExpiresAfter)
	addBefore("dc.expires_at", search.ExpiresBefore)

	orderCol := "dc.created_at"
	switch search.SortBy {
	case "code":
		orderCol = "dc.code"
	case "status":
		orderCol = "dc.status::text"
	case "times_redeemed":
		orderCol = "dc.times_redeemed"
	case "expires_at":
		orderCol = "dc.expires_at"
	case "starts_at":
		orderCol = "dc.starts_at"
	case "updated_at":
		orderCol = "dc.updated_at"
	}
	orderDir := "DESC"
	if search.SortBy != "" && !search.SortDesc {
		orderDir = "ASC"
	}
	orderBy := "ORDER BY " + orderCol + " " + orderDir + ", dc.id DESC"

	offset := search.Offset
	if offset < 0 {
		offset = 0
	}

	// args holds only the WHERE filters here; snapshot them for the COUNT
	// before the pagination placeholders are appended. Offset paging keeps the
	// keyset correct for every sort column (a plain dc.id keyset did not match
	// the ORDER BY and skipped/duplicated rows on any non-id sort).
	countArgs := append([]interface{}{}, args...)

	limitArg := argNum
	offsetArg := argNum + 1
	args = append(args, limit+1, offset)

	query := `SELECT ` + dcCols + ` FROM discount_codes dc ` + whereClause + ` ` + orderBy +
		` LIMIT $` + itoa(limitArg) + ` OFFSET $` + itoa(offsetArg)

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	codes := []models.DiscountCode{}
	ids := []uuid.UUID{}
	for rows.Next() {
		var c models.DiscountCode
		err := rows.Scan(
			&c.ID, &c.Code, &c.Description, &c.Type, &c.PercentOff, &c.AmountOff, &c.Currency, &c.TrialExtensionDays,
			&c.Duration, &c.DurationInMonths, &c.MaxRedemptions, &c.TimesRedeemed, &c.PerAccountLimit,
			&c.AppliesToAllPlans, &c.Status, &c.StartsAt, &c.ExpiresAt, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		c.PlanIDs = []uuid.UUID{}
		codes = append(codes, c)
		ids = append(ids, c.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	result := &models.AdminDiscountsResult{
		Data:       codes,
		Pagination: models.Pagination{HasMore: len(codes) > limit},
	}
	if len(codes) > limit {
		result.Data = codes[:limit]
		ids = ids[:limit]
		result.Pagination.NextCursor = paging.EncodeOffset(offset + limit)
	}

	// Attach plan eligibility for the returned page in one query.
	if len(ids) > 0 {
		planRows, err := r.db.Query(ctx, `
			SELECT discount_code_id, plan_id FROM discount_code_plans WHERE discount_code_id = ANY($1)
		`, ids)
		if err != nil {
			return nil, err
		}
		defer planRows.Close()

		byCode := map[uuid.UUID][]uuid.UUID{}
		for planRows.Next() {
			var codeID, planID uuid.UUID
			if err := planRows.Scan(&codeID, &planID); err != nil {
				return nil, err
			}
			byCode[codeID] = append(byCode[codeID], planID)
		}
		if err := planRows.Err(); err != nil {
			return nil, err
		}
		for i := range result.Data {
			if pl := byCode[result.Data[i].ID]; pl != nil {
				result.Data[i].PlanIDs = pl
			}
		}
	}

	// Total count for the same filter, using the snapshot of WHERE-only args
	// (the live args slice now also carries the LIMIT/OFFSET placeholders).
	countQuery := `SELECT COUNT(*) FROM discount_codes dc ` + whereClause
	var total int64
	if err := r.db.QueryRow(ctx, countQuery, countArgs...).Scan(&total); err == nil {
		result.Pagination.Total = &total
	}

	return result, nil
}

func (r *discountCodeRepository) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.Exec(ctx, `DELETE FROM discount_codes WHERE id = $1`, id)
	return err
}

// DiscountRedemptionRepository owns the discount_redemptions table.
type DiscountRedemptionRepository interface {
	// ReserveRedemption atomically enforces the per-account and global caps and
	// inserts the redemption row (status taken from r.Status). Returns
	// ErrDiscountExhausted / ErrDiscountAlreadyRedeemed when a cap is hit. The
	// Stripe coupon / checkout-session IDs are typically attached afterward via
	// AttachStripeRefs so the slot is reserved before any Stripe call.
	ReserveRedemption(ctx context.Context, r *models.DiscountRedemption, maxRedemptions *int, perAccountLimit int) error
	AttachStripeRefs(ctx context.Context, redemptionID uuid.UUID, sessionID, couponID *string) error
	MarkAppliedBySession(ctx context.Context, sessionID string, subscriptionID *uuid.UUID) error
	CancelBySession(ctx context.Context, sessionID string) error
	CancelByID(ctx context.Context, redemptionID uuid.UUID) error
	CountActiveByCodeAndOrg(ctx context.Context, codeID, orgID uuid.UUID) (int, error)
	ListByCode(ctx context.Context, codeID uuid.UUID, offset, limit int) (*models.AdminDiscountRedemptionsResult, error)
	// ListByOrganization returns an org's redemptions (newest first), joined to
	// the code string, for the customer billing page.
	ListByOrganization(ctx context.Context, orgID uuid.UUID, limit int) ([]models.DiscountRedemption, error)
}

type discountRedemptionRepository struct {
	db *pgxpool.Pool
}

func NewDiscountRedemptionRepository(db *pgxpool.Pool) DiscountRedemptionRepository {
	return &discountRedemptionRepository{db: db}
}

func (r *discountRedemptionRepository) ReserveRedemption(ctx context.Context, red *models.DiscountRedemption, maxRedemptions *int, perAccountLimit int) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Per-account cap (pending + applied count for this org).
	var orgCount int
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*) FROM discount_redemptions
		WHERE discount_code_id = $1 AND organization_id = $2 AND status IN ('pending', 'applied')
	`, red.DiscountCodeID, red.OrganizationID).Scan(&orgCount); err != nil {
		return err
	}
	if perAccountLimit > 0 && orgCount >= perAccountLimit {
		return ErrDiscountAlreadyRedeemed
	}

	// Global cap: atomic conditional bump of the claimed counter.
	if maxRedemptions != nil {
		tag, err := tx.Exec(ctx, `
			UPDATE discount_codes
			SET times_redeemed = times_redeemed + 1, updated_at = NOW()
			WHERE id = $1 AND (max_redemptions IS NULL OR times_redeemed < max_redemptions)
		`, red.DiscountCodeID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrDiscountExhausted
		}
	} else {
		if _, err := tx.Exec(ctx, `
			UPDATE discount_codes SET times_redeemed = times_redeemed + 1, updated_at = NOW() WHERE id = $1
		`, red.DiscountCodeID); err != nil {
			return err
		}
	}

	if red.Status == "" {
		red.Status = models.DiscountRedemptionStatusPending
	}
	if red.ID == uuid.Nil {
		red.ID = uuid.New()
	}
	var appliedAt *time.Time
	if red.Status == models.DiscountRedemptionStatusApplied {
		now := time.Now()
		appliedAt = &now
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO discount_redemptions (
			id, discount_code_id, organization_id, redeemed_by, subscription_id, plan_id,
			stripe_coupon_id, stripe_checkout_session_id, type, percent_off, amount_off,
			currency, trial_extension_days, status, redeemed_at, applied_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, NOW(), $15
		)
	`,
		red.ID, red.DiscountCodeID, red.OrganizationID, red.RedeemedBy, red.SubscriptionID, red.PlanID,
		red.StripeCouponID, red.StripeCheckoutSessionID, red.Type, red.PercentOff, red.AmountOff,
		red.Currency, red.TrialExtensionDays, red.Status, appliedAt,
	)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// AttachStripeRefs sets the Stripe coupon / checkout-session IDs on a redemption
// after they're created. Either may be nil (e.g. plan changes have no session).
func (r *discountRedemptionRepository) AttachStripeRefs(ctx context.Context, redemptionID uuid.UUID, sessionID, couponID *string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE discount_redemptions
		SET stripe_checkout_session_id = COALESCE($2, stripe_checkout_session_id),
			stripe_coupon_id = COALESCE($3, stripe_coupon_id)
		WHERE id = $1
	`, redemptionID, sessionID, couponID)
	return err
}

// CancelByID releases a reserved redemption (e.g. when the Stripe call that the
// reservation was made for ultimately fails), freeing the global slot.
func (r *discountRedemptionRepository) CancelByID(ctx context.Context, redemptionID uuid.UUID) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var codeID uuid.UUID
	err = tx.QueryRow(ctx, `
		UPDATE discount_redemptions SET status = 'canceled'
		WHERE id = $1 AND status <> 'canceled'
		RETURNING discount_code_id
	`, redemptionID).Scan(&codeID)
	if err == pgx.ErrNoRows {
		return tx.Commit(ctx) // already canceled / not found; idempotent
	}
	if err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE discount_codes SET times_redeemed = GREATEST(times_redeemed - 1, 0), updated_at = NOW() WHERE id = $1
	`, codeID); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (r *discountRedemptionRepository) MarkAppliedBySession(ctx context.Context, sessionID string, subscriptionID *uuid.UUID) error {
	_, err := r.db.Exec(ctx, `
		UPDATE discount_redemptions
		SET status = 'applied', applied_at = NOW(), subscription_id = COALESCE($2, subscription_id)
		WHERE stripe_checkout_session_id = $1 AND status = 'pending'
	`, sessionID, subscriptionID)
	return err
}

func (r *discountRedemptionRepository) CancelBySession(ctx context.Context, sessionID string) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var codeID uuid.UUID
	err = tx.QueryRow(ctx, `
		UPDATE discount_redemptions SET status = 'canceled'
		WHERE stripe_checkout_session_id = $1 AND status = 'pending'
		RETURNING discount_code_id
	`, sessionID).Scan(&codeID)
	if err == pgx.ErrNoRows {
		return tx.Commit(ctx) // nothing pending for this session; idempotent
	}
	if err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE discount_codes SET times_redeemed = GREATEST(times_redeemed - 1, 0), updated_at = NOW() WHERE id = $1
	`, codeID); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (r *discountRedemptionRepository) CountActiveByCodeAndOrg(ctx context.Context, codeID, orgID uuid.UUID) (int, error) {
	var count int
	err := r.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM discount_redemptions
		WHERE discount_code_id = $1 AND organization_id = $2 AND status IN ('pending', 'applied')
	`, codeID, orgID).Scan(&count)
	return count, err
}

func (r *discountRedemptionRepository) ListByOrganization(ctx context.Context, orgID uuid.UUID, limit int) ([]models.DiscountRedemption, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := r.db.Query(ctx, `
		SELECT dr.id, dr.discount_code_id, dr.organization_id, dr.redeemed_by, dr.subscription_id, dr.plan_id,
			dr.stripe_coupon_id, dr.stripe_checkout_session_id, dr.type, dr.percent_off, dr.amount_off,
			dr.currency, dr.trial_extension_days, dr.status, dr.redeemed_at, dr.applied_at, dc.code
		FROM discount_redemptions dr
		JOIN discount_codes dc ON dc.id = dr.discount_code_id
		WHERE dr.organization_id = $1
		ORDER BY dr.redeemed_at DESC
		LIMIT $2
	`, orgID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []models.DiscountRedemption{}
	for rows.Next() {
		var d models.DiscountRedemption
		if err := rows.Scan(
			&d.ID, &d.DiscountCodeID, &d.OrganizationID, &d.RedeemedBy, &d.SubscriptionID, &d.PlanID,
			&d.StripeCouponID, &d.StripeCheckoutSessionID, &d.Type, &d.PercentOff, &d.AmountOff,
			&d.Currency, &d.TrialExtensionDays, &d.Status, &d.RedeemedAt, &d.AppliedAt, &d.Code,
		); err != nil {
			return nil, err
		}
		items = append(items, d)
	}
	return items, rows.Err()
}

func (r *discountRedemptionRepository) ListByCode(ctx context.Context, codeID uuid.UUID, offset, limit int) (*models.AdminDiscountRedemptionsResult, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	// Offset paging: rows are ordered by redeemed_at, so a dc.id keyset would
	// not match the sort. The (discount_code_id, redeemed_at DESC) index keeps
	// it fast even for a code with many redemptions.
	query := `
		SELECT id, discount_code_id, organization_id, redeemed_by, subscription_id, plan_id,
			stripe_coupon_id, stripe_checkout_session_id, type, percent_off, amount_off,
			currency, trial_extension_days, status, redeemed_at, applied_at
		FROM discount_redemptions
		WHERE discount_code_id = $1
		ORDER BY redeemed_at DESC, id DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := r.db.Query(ctx, query, codeID, limit+1, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []models.DiscountRedemption{}
	for rows.Next() {
		var d models.DiscountRedemption
		err := rows.Scan(
			&d.ID, &d.DiscountCodeID, &d.OrganizationID, &d.RedeemedBy, &d.SubscriptionID, &d.PlanID,
			&d.StripeCouponID, &d.StripeCheckoutSessionID, &d.Type, &d.PercentOff, &d.AmountOff,
			&d.Currency, &d.TrialExtensionDays, &d.Status, &d.RedeemedAt, &d.AppliedAt,
		)
		if err != nil {
			return nil, err
		}
		items = append(items, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	result := &models.AdminDiscountRedemptionsResult{
		Data:       items,
		Pagination: models.Pagination{HasMore: len(items) > limit},
	}
	if len(items) > limit {
		result.Data = items[:limit]
		result.Pagination.NextCursor = paging.EncodeOffset(offset + limit)
	}

	return result, nil
}
