package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/utils"
)

type RelationSyncInput struct {
	Tx         pgx.Tx
	Ctx        context.Context
	Table      string // e.g. "email_tags"
	ColMain    string // e.g. "email"
	ColRelated string // e.g. "tag"
	MainID     any
	NewValues  []string
}

func SyncRelation(input RelationSyncInput) ([]string, *errx.Error) {
	querySelect := fmt.Sprintf(`SELECT %s FROM %s WHERE %s = $1`,
		input.ColRelated, input.Table, input.ColMain)

	params := []any{
		input.MainID,
	}

	rows, err := input.Tx.Query(
		input.Ctx,
		querySelect,
		params...,
	)
	if err != nil {
		db.CaptureError(err, querySelect, params, "query")
		return nil, errx.InternalError()
	}
	defer rows.Close()

	var current []string
	for rows.Next() {
		var val string
		if err := rows.Scan(&val); err != nil {
			db.CaptureError(err, "", nil, "scan")
			return nil, errx.InternalError()
		}
		current = append(current, val)
	}

	toInsert := utils.Difference(input.NewValues, current)
	toDelete := utils.Difference(current, input.NewValues)

	if len(toDelete) > 0 {
		queryDel := fmt.Sprintf(`DELETE FROM %s WHERE %s = $1 AND %s = ANY($2)`,
			input.Table, input.ColMain, input.ColRelated)

		params = []any{
			input.MainID,
			toDelete,
		}

		if _, err := input.Tx.Exec(
			input.Ctx,
			queryDel,
			params...,
		); err != nil {
			db.CaptureError(err, queryDel, params, "exec")
			return nil, errx.InternalError()
		}
	}

	if len(toInsert) > 0 {
		queryIns := fmt.Sprintf(`INSERT INTO %s (%s, %s)
                                 SELECT $1, unnest($2::text[])`,
			input.Table, input.ColMain, input.ColRelated)

		params = []any{
			input.MainID,
			toInsert,
		}

		if _, err := input.Tx.Exec(input.Ctx, queryIns, input.MainID, toInsert); err != nil {
			db.CaptureError(err, queryIns, params, "exec")
			return nil, errx.InternalError()
		}
	}

	// Construct final result
	final := utils.Filter(current, func(v string) bool {
		return !utils.Contains(toDelete, v)
	})

	final = append(final, toInsert...)

	return final, nil
}

func SyncEmailTags(ctx context.Context, tx pgx.Tx, emailAccountID string, newTags []string) ([]string, *errx.Error) {
	tags, err := SyncRelation(RelationSyncInput{
		Tx:         tx,
		Ctx:        ctx,
		Table:      "email_tags",
		ColMain:    "email_id",
		ColRelated: "tag_id",
		MainID:     emailAccountID,
		NewValues:  newTags,
	})
	if err != nil {
		return nil, err
	}
	return tags, nil
}

func SyncCampaignEmailTags(ctx context.Context, tx pgx.Tx, campaignID string, newTags []string) ([]string, *errx.Error) {
	tags, err := SyncRelation(RelationSyncInput{
		Tx:         tx,
		Ctx:        ctx,
		Table:      "campaign_email_tags",
		ColMain:    "campaign_id",
		ColRelated: "tag_id",
		MainID:     campaignID,
		NewValues:  newTags,
	})
	if err != nil {
		return nil, err
	}
	return tags, nil
}

func SyncCampaignFolders(ctx context.Context, tx pgx.Tx, campaignID string, newFolders []string) ([]string, *errx.Error) {
	folders, err := SyncRelation(RelationSyncInput{
		Tx:         tx,
		Ctx:        ctx,
		Table:      "campaign_folders",
		ColMain:    "campaign_id",
		ColRelated: "folder_id",
		MainID:     campaignID,
		NewValues:  newFolders,
	})
	if err != nil {
		return nil, err
	}
	return folders, nil
}
