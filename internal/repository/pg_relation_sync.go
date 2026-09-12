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

	// ScopeTable is the label registry the related ids must come from
	// ("tags" / "folders" / "categories"), and OrgID the workspace that must
	// own them. The FK only says the row exists, so without this an id from
	// another workspace links cleanly; ids that fail the check are dropped,
	// not refused, matching how every other label write treats a stale id.
	ScopeTable string
	OrgID      any
}

// SyncRelation diffs the desired related-id set against what's stored and
// applies the minimal insert/delete. All relation join tables here key on
// uuid↔uuid (campaign_id/email_id ↔ tag_id/folder_id), and Postgres has NO
// implicit text→uuid assignment cast, so the id params (sent by pgx as text)
// are cast to uuid explicitly. The SELECT casts the related id back to ::text
// so it scans cleanly into a Go string.
func SyncRelation(input RelationSyncInput) ([]string, *errx.Error) {
	// The current set is read WITHIN the scope, not just by parent id. A link
	// to another workspace's label (only a migration can leave one behind, but
	// the diff must not depend on that) is invisible here, so it is never
	// echoed back to the client as if it were part of the set.
	querySelect := fmt.Sprintf(`SELECT %s::text FROM %s WHERE %s = $1::uuid`,
		input.ColRelated, input.Table, input.ColMain)

	params := []any{
		input.MainID,
	}

	if input.ScopeTable != "" {
		querySelect = fmt.Sprintf(
			`SELECT r.%s::text FROM %s r JOIN %s g ON g.id = r.%s
			 WHERE r.%s = $1::uuid AND g.organization_id = $2::uuid`,
			input.ColRelated, input.Table, input.ScopeTable, input.ColRelated,
			input.ColMain)
		params = append(params, input.OrgID)
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
		queryDel := fmt.Sprintf(`DELETE FROM %s WHERE %s = $1::uuid AND %s = ANY($2::uuid[])`,
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

	// inserted, not toInsert: an id the scope check rejected never became a
	// row, so reporting it back would tell the client a link exists that does
	// not.
	var inserted []string
	if len(toInsert) > 0 {
		var queryIns string
		var params []any
		if input.ScopeTable != "" {
			queryIns = fmt.Sprintf(`INSERT INTO %s (%s, %s)
                                 SELECT $1::uuid, g.id FROM %s g
                                 WHERE g.id = ANY($2::uuid[]) AND g.organization_id = $3::uuid
                                 RETURNING %s::text`,
				input.Table, input.ColMain, input.ColRelated, input.ScopeTable, input.ColRelated)
			params = []any{input.MainID, toInsert, input.OrgID}
		} else {
			queryIns = fmt.Sprintf(`INSERT INTO %s (%s, %s)
                                 SELECT $1::uuid, unnest($2::uuid[])
                                 RETURNING %s::text`,
				input.Table, input.ColMain, input.ColRelated, input.ColRelated)
			params = []any{input.MainID, toInsert}
		}

		rows, err := input.Tx.Query(input.Ctx, queryIns, params...)
		if err != nil {
			db.CaptureError(err, queryIns, params, "query")
			return nil, errx.InternalError()
		}
		for rows.Next() {
			var val string
			if err := rows.Scan(&val); err != nil {
				rows.Close()
				db.CaptureError(err, "", nil, "scan")
				return nil, errx.InternalError()
			}
			inserted = append(inserted, val)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			db.CaptureError(err, queryIns, params, "rows")
			return nil, errx.InternalError()
		}
	}

	// Construct final result
	final := utils.Filter(current, func(v string) bool {
		return !utils.Contains(toDelete, v)
	})

	final = append(final, inserted...)

	return final, nil
}

func SyncEmailTags(ctx context.Context, tx pgx.Tx, orgID any, emailAccountID string, newTags []string) ([]string, *errx.Error) {
	tags, err := SyncRelation(RelationSyncInput{
		Tx:         tx,
		Ctx:        ctx,
		Table:      "email_tags",
		ColMain:    "email_id",
		ColRelated: "tag_id",
		MainID:     emailAccountID,
		NewValues:  newTags,
		ScopeTable: "tags",
		OrgID:      orgID,
	})
	if err != nil {
		return nil, err
	}
	return tags, nil
}

func SyncCampaignEmailTags(ctx context.Context, tx pgx.Tx, orgID any, campaignID string, newTags []string) ([]string, *errx.Error) {
	tags, err := SyncRelation(RelationSyncInput{
		Tx:         tx,
		Ctx:        ctx,
		Table:      "campaign_email_tags",
		ColMain:    "campaign_id",
		ColRelated: "tag_id",
		MainID:     campaignID,
		NewValues:  newTags,
		ScopeTable: "tags",
		OrgID:      orgID,
	})
	if err != nil {
		return nil, err
	}
	return tags, nil
}

func SyncCampaignFolders(ctx context.Context, tx pgx.Tx, orgID any, campaignID string, newFolders []string) ([]string, *errx.Error) {
	folders, err := SyncRelation(RelationSyncInput{
		Tx:         tx,
		Ctx:        ctx,
		Table:      "campaign_folders",
		ColMain:    "campaign_id",
		ColRelated: "folder_id",
		MainID:     campaignID,
		NewValues:  newFolders,
		ScopeTable: "folders",
		OrgID:      orgID,
	})
	if err != nil {
		return nil, err
	}
	return folders, nil
}
