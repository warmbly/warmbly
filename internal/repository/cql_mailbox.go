package repository

import (
	"context"
	"time"

	"github.com/gocql/gocql"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/infrastructure/cdb"
	"github.com/warmbly/warmbly/internal/models"
)

type MailboxRepository interface {
	CreateEntry(ctx context.Context, userId, emailId uuid.UUID, mb *models.Mailbox) error
	GetMailbox(ctx context.Context, userId, emailId uuid.UUID, uidValidity uint32) (*models.Mailbox, error)
	ListMailboxes(ctx context.Context, userId, emailId uuid.UUID) ([]models.Mailbox, error)
	DeleteMailbox(ctx context.Context, userId, emailId uuid.UUID, uidValidity uint32) error
}

type mailboxRepository struct {
	db *cdb.Client
}

func NewMailboxRepostory(cdb *cdb.Client) MailboxRepository {
	return &mailboxRepository{
		db: cdb,
	}
}

func (r *mailboxRepository) CreateEntry(ctx context.Context, userId uuid.UUID, emailId uuid.UUID, mb *models.Mailbox) error {
	mb.UpdatedAt = time.Now()

	cql := `
		INSERT INTO mailboxes (
			email_id, mailbox,
			attributes, uid_validity, highestmodseq,
			created_at
		)
		VALUES (
			?, ?,
			?, ?, ?,
			?
		)
	`
	return r.db.Query(cql,
		emailId,
		mb.Name,
		mb.Attrs,
		mb.UIDValidity,
		mb.HighestModSeq,
		mb.UpdatedAt,
	).WithContext(ctx).Exec()
}

func (r *mailboxRepository) GetMailbox(ctx context.Context, userId, emailId uuid.UUID, uidvalidity uint32) (*models.Mailbox, error) {
	cql := `
		SELECT 
			mailbox, attributes,
			uid_validity, highestmodseq,
			updated_at
		FROM mailboxes
		WHERE user_id = ? AND email_id = ? AND uid_validity = ?
	`

	params := []any{
		userId,
		emailId,
		uidvalidity,
	}

	var mb models.Mailbox
	if err := r.db.Query(
		cql,
		params...,
	).WithContext(ctx).Scan(
		&mb.Name, &mb.Attrs,
		&mb.UIDValidity, &mb.HighestModSeq,
		&mb.UpdatedAt,
	); err != nil {
		return nil, err
	}

	return &mb, nil
}

func (r *mailboxRepository) ListMailboxes(ctx context.Context, userId, emailId uuid.UUID) ([]models.Mailbox, error) {
	cql := `
		SELECT
			mailbox, attributes,
			uid_validity, highestmodseq,
			updated_at
		FROM mailboxes
		WHERE user_id = ? AND email_id = ?
	`

	params := []any{
		userId,
		emailId,
	}

	iter := r.db.Query(
		cql,
		params...,
	).WithContext(ctx).Iter()

	var mailboxes []models.Mailbox
	var mb models.Mailbox
	for iter.Scan(
		&mb.Name,
		&mb.Attrs,
		&mb.UIDValidity,
		&mb.HighestModSeq,
		&mb.UpdatedAt,
	) {
		mailboxes = append(mailboxes, mb)
	}
	if err := iter.Close(); err != nil {
		return nil, err
	}

	return mailboxes, nil
}

func (r *mailboxRepository) DeleteMailboxes(ctx context.Context, userId, emailId uuid.UUID, uidValidities []uint32) error {
	batch := r.db.NewBatch(gocql.LoggedBatch).WithContext(ctx)

	cql := `
		DELETE FROM mailboxes WHERE userId = ? AND email_id = ? AND uid_validity = ?
	`

	for _, mbox := range uidValidities {
		params := []any{
			userId,
			emailId,
			mbox,
		}

		batch.Query(
			cql,
			params...,
		)
	}

	return r.db.ExecuteBatch(batch)
}

func (r *mailboxRepository) DeleteMailbox(ctx context.Context, userId, emailId uuid.UUID, uidValidity uint32) error {
	cql := `
		DELETE FROM mailboxes WHERE user_id = ? AND email_id = ? AND uid_validity = ?
	`

	params := []any{
		userId,
		emailId,
		uidValidity,
	}

	return r.db.Query(
		cql,
		params...,
	).WithContext(ctx).Exec()
}
