package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/emailverify"
)

// VerificationEvidenceRepository owns the evidence ledger behind a contact's
// verification verdict and the derived score on the contact row.
type VerificationEvidenceRepository interface {
	// Record stores one observation. Idempotent on (contact, kind, ref);
	// returns whether a new row was written. step names the campaign step the
	// observation came from, so an observation about an address the contact no
	// longer holds can be refused; a zero step means "not attributable to a
	// step", which is always recorded.
	Record(ctx context.Context, contactID uuid.UUID, step models.EvidenceStep, kind, ref, detail string, observedAt time.Time) (bool, error)
	// ListForContact returns the contact's evidence, newest first.
	ListForContact(ctx context.Context, contactID uuid.UUID) ([]models.ContactVerificationEvidence, error)
	// Verdict reads the contact's current check verdict for scoring.
	Verdict(ctx context.Context, contactID uuid.UUID) (ContactVerdict, error)
	// SetScore writes the derived status and confidence; a lapsed override falls back to the check's status.
	SetScore(ctx context.Context, contactID uuid.UUID, status string, confidence int, reason string, lastPositive time.Time, decisive bool) error
	// CreditCleanDeliveries records a delivered observation for every campaign
	// step sent at least `window` ago that never bounced and is not yet in
	// the ledger. Returns the contacts credited.
	CreditCleanDeliveries(ctx context.Context, window time.Duration, limit int) ([]uuid.UUID, error)
}

// ContactVerdict is a contact's last verdict (the check's own status) and any re-check waiting on it.
type ContactVerdict struct {
	emailverify.Verdict
	// Stored is verification_status as saved, after any override from real mail.
	Stored      emailverify.Status
	RequestedAt *time.Time
}

type verificationEvidenceRepository struct {
	DB *db.DB
}

func NewVerificationEvidenceRepository(database *db.DB) VerificationEvidenceRepository {
	return &verificationEvidenceRepository{DB: database}
}

func (r *verificationEvidenceRepository) Record(ctx context.Context, contactID uuid.UUID, step models.EvidenceStep, kind, ref, detail string, observedAt time.Time) (bool, error) {
	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
	}
	// A bounce or an open lands long after the mail that produced it, and
	// editing a contact's address in between does not make the old mailbox's
	// behaviour evidence about the new one: a delayed hard bounce for a typo
	// would otherwise mark the corrected address invalid and stop every send
	// to it. The refusal needs proof, so it only fires when the step is known
	// AND provably left before the address changed; anything else is recorded.
	query := `
		INSERT INTO contact_verification_evidence (contact_id, kind, ref, detail, observed_at)
		SELECT $1, $2, $3, $4, $5
		FROM contacts c
		WHERE c.id = $1
		  AND (c.verification_evidence_reset_at IS NULL OR NOT EXISTS (
		        SELECT 1 FROM campaign_contact_progress p
		        WHERE p.campaign_id = $6 AND p.contact_id = $1 AND p.sequence_id = $7
		          AND COALESCE(p.dispatched_at, p.sent_at) <= c.verification_evidence_reset_at))
		ON CONFLICT (contact_id, kind, ref) DO NOTHING
	`
	params := []any{contactID, kind, ref, detail, observedAt, step.CampaignID, step.SequenceID}
	cmd, err := r.DB.Exec(ctx, query, params...)
	if err != nil {
		db.CaptureError(err, query, params, "exec")
		return false, err
	}
	return cmd.RowsAffected() > 0, nil
}

func (r *verificationEvidenceRepository) ListForContact(ctx context.Context, contactID uuid.UUID) ([]models.ContactVerificationEvidence, error) {
	query := `
		SELECT kind, detail, observed_at
		FROM contact_verification_evidence
		WHERE contact_id = $1
		ORDER BY observed_at DESC
		LIMIT 50
	`
	rows, err := r.DB.Query(ctx, query, contactID)
	if err != nil {
		db.CaptureError(err, query, []any{contactID}, "query")
		return nil, err
	}
	defer rows.Close()
	out := []models.ContactVerificationEvidence{}
	for rows.Next() {
		var e models.ContactVerificationEvidence
		if err := rows.Scan(&e.Kind, &e.Detail, &e.ObservedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (r *verificationEvidenceRepository) Verdict(ctx context.Context, contactID uuid.UUID) (ContactVerdict, error) {
	var v ContactVerdict
	var stored, status, source, provider string
	var checked *time.Time
	// The check's own status only means something for a check; an imported or
	// manual verdict is its own status.
	query := `
		SELECT verification_status,
		       CASE WHEN verification_source IN ('probe', 'provider') AND verification_check_status <> ''
		            THEN verification_check_status ELSE verification_status END,
		       verification_source, verification_provider, verification_checked_at, verification_requested_at
		FROM contacts WHERE id = $1`
	if err := r.DB.QueryRow(ctx, query, contactID).Scan(&stored, &status, &source, &provider, &checked, &v.RequestedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return v, errx.ErrNotFound
		}
		db.CaptureError(err, query, []any{contactID}, "queryrow")
		return v, err
	}
	v.Stored, v.Status, v.Source, v.Provider = emailverify.Status(stored), emailverify.Status(status), source, provider
	if checked != nil {
		v.CheckedAt = *checked
	}
	return v, nil
}

func (r *verificationEvidenceRepository) SetScore(ctx context.Context, contactID uuid.UUID, status string, confidence int, reason string, lastPositive time.Time, decisive bool) error {
	var lp *time.Time
	if !lastPositive.IsZero() {
		lp = &lastPositive
	}
	// Decisive real mail sets the status; otherwise a lapsed override falls back
	// to the check's own status, read from the row so a check landing meanwhile wins.
	query := `
		UPDATE contacts
		SET verification_confidence = $2,
		    verification_evidence_at = COALESCE($3, verification_evidence_at),
		    verification_status = CASE WHEN $5 THEN $4
		        WHEN verification_source IN ('probe', 'provider') AND verification_check_status <> '' THEN verification_check_status
		        ELSE verification_status END,
		    verification_reason = CASE WHEN $5 THEN $6
		        WHEN verification_source IN ('probe', 'provider') AND verification_check_status <> ''
		             AND verification_check_status <> verification_status THEN $6
		        ELSE verification_reason END,
		    updated_at = NOW()
		WHERE id = $1
	`
	params := []any{contactID, confidence, lp, status, decisive, reason}
	if _, err := r.DB.Exec(ctx, query, params...); err != nil {
		db.CaptureError(err, query, params, "exec")
		return err
	}
	return nil
}

func (r *verificationEvidenceRepository) CreditCleanDeliveries(ctx context.Context, window time.Duration, limit int) ([]uuid.UUID, error) {
	if limit <= 0 {
		limit = 1000
	}
	// One evidence row per sent step; the ref is the step so a re-run of the
	// job is a no-op, and a step that bounces later is excluded here and
	// recorded as a bounce by the deliverability path instead.
	// The join to contacts is what keeps a corrected address from inheriting
	// the old mailbox's record: this credit is derived from every step ever
	// sent, with no lower bound of its own, so the evidence an address change
	// deletes would come straight back on the next pass. Steps sent before the
	// change went to a different mailbox and are not evidence about this one.
	// dispatched_at, not sent_at: sent_at is stamped when the worker's result
	// lands, which for a send already on the bus can be after the edit.
	query := `
		WITH due AS (
			SELECT p.contact_id, p.campaign_id, p.sequence_id, p.sent_at
			FROM campaign_contact_progress p
			JOIN contacts c ON c.id = p.contact_id
			WHERE p.sent_at IS NOT NULL AND p.bounced_at IS NULL
			  AND ` + progressIsEmailStep("p") + `
			  AND p.sent_at < NOW() - make_interval(secs => $1)
			  AND (c.verification_evidence_reset_at IS NULL OR COALESCE(p.dispatched_at, p.sent_at) > c.verification_evidence_reset_at)
			  AND NOT EXISTS (
			    SELECT 1 FROM contact_verification_evidence e
			    WHERE e.contact_id = p.contact_id AND e.kind = 'delivered'
			      AND e.ref = p.campaign_id::text || ':' || p.sequence_id::text
			  )
			ORDER BY p.sent_at
			LIMIT $2
		),
		ins AS (
			INSERT INTO contact_verification_evidence (contact_id, kind, ref, detail, observed_at)
			SELECT contact_id, 'delivered', campaign_id::text || ':' || sequence_id::text, '', sent_at FROM due
			ON CONFLICT (contact_id, kind, ref) DO NOTHING
			RETURNING contact_id
		)
		SELECT DISTINCT contact_id FROM ins
	`
	params := []any{window.Seconds(), limit}
	rows, err := r.DB.Query(ctx, query, params...)
	if err != nil {
		db.CaptureError(err, query, params, "query")
		return nil, err
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
