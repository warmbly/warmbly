package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

type MailboxRepository interface {
	CreateEntry(ctx context.Context, userId, emailId uuid.UUID, mb *models.Mailbox) error
	GetMailbox(ctx context.Context, userId, emailId uuid.UUID, uidValidity uint32) (*models.Mailbox, error)
	ListMailboxes(ctx context.Context, userId, emailId uuid.UUID) ([]models.Mailbox, error)
	DeleteMailbox(ctx context.Context, userId, emailId uuid.UUID, uidValidity uint32) error
}

type mailboxRepository struct {
	db *db.DB
}

func NewMailboxRepository(db *db.DB) MailboxRepository {
	return &mailboxRepository{db: db}
}

func (r *mailboxRepository) CreateEntry(ctx context.Context, userId, emailId uuid.UUID, mb *models.Mailbox) error {
	mb.UpdatedAt = time.Now()

	query := `
		INSERT INTO unibox_mailboxes (email_id, uid_validity, mailbox, attributes, highestmodseq, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (email_id, uid_validity) DO UPDATE SET
			mailbox = EXCLUDED.mailbox,
			attributes = EXCLUDED.attributes,
			highestmodseq = EXCLUDED.highestmodseq,
			updated_at = EXCLUDED.updated_at
	`

	_, err := r.db.Exec(ctx, query,
		emailId, mb.UIDValidity, mb.Name, mb.Attrs, mb.HighestModSeq, mb.UpdatedAt,
	)
	return err
}

func (r *mailboxRepository) GetMailbox(ctx context.Context, userId, emailId uuid.UUID, uidValidity uint32) (*models.Mailbox, error) {
	query := `
		SELECT mailbox, attributes, uid_validity, highestmodseq, updated_at
		FROM unibox_mailboxes
		WHERE email_id = $1 AND uid_validity = $2
	`

	var mb models.Mailbox
	err := r.db.QueryRow(ctx, query, emailId, uidValidity).Scan(
		&mb.Name, &mb.Attrs, &mb.UIDValidity, &mb.HighestModSeq, &mb.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	return &mb, nil
}

func (r *mailboxRepository) ListMailboxes(ctx context.Context, userId, emailId uuid.UUID) ([]models.Mailbox, error) {
	query := `
		SELECT mailbox, attributes, uid_validity, highestmodseq, updated_at
		FROM unibox_mailboxes
		WHERE email_id = $1
	`

	rows, err := r.db.Query(ctx, query, emailId)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var mailboxes []models.Mailbox
	for rows.Next() {
		var mb models.Mailbox
		if err := rows.Scan(&mb.Name, &mb.Attrs, &mb.UIDValidity, &mb.HighestModSeq, &mb.UpdatedAt); err != nil {
			return nil, err
		}
		mailboxes = append(mailboxes, mb)
	}

	return mailboxes, nil
}

func (r *mailboxRepository) DeleteMailbox(ctx context.Context, userId, emailId uuid.UUID, uidValidity uint32) error {
	_, err := r.db.Exec(ctx,
		`DELETE FROM unibox_mailboxes WHERE email_id = $1 AND uid_validity = $2`,
		emailId, uidValidity,
	)
	return err
}
