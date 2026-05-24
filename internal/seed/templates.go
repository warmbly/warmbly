package seed

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func seedReplyTemplates(ctx context.Context, pool *pgxpool.Pool, _ *Result) error {
	type tpl struct {
		id      uuid.UUID
		name    string
		subject string
		html    string
		plain   string
		pos     int
	}
	tpls := []tpl{
		{ReplyTemplateYesID, "Quick yes", "Re: {{originalSubject}}",
			"<p>Sounds great - sending a calendar invite for tomorrow.</p>",
			"Sounds great - sending a calendar invite for tomorrow.", 0},
		{ReplyTemplateNoID, "Polite no", "Re: {{originalSubject}}",
			"<p>Thanks for the note - not a fit right now, please remove me from this list.</p>",
			"Thanks for the note - not a fit right now, please remove me from this list.", 1},
	}
	for _, t := range tpls {
		_, err := pool.Exec(ctx, `
			INSERT INTO reply_templates (id, organization_id, user_id, name, subject, body_html, body_plain, position, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW(), NOW())
			ON CONFLICT (id) DO UPDATE SET
				name = EXCLUDED.name,
				subject = EXCLUDED.subject,
				body_html = EXCLUDED.body_html,
				body_plain = EXCLUDED.body_plain,
				position = EXCLUDED.position,
				updated_at = NOW()
		`, t.id, OrgAcmeID, UserOwnerID, t.name, t.subject, t.html, t.plain, t.pos)
		if err != nil {
			return err
		}
	}
	return nil
}
