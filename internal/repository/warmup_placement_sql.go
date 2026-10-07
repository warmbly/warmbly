package repository

import (
	"fmt"
)

// recipientGroupSQL is models.WarmupRecipientGroup over an email_accounts
// alias; the two must agree.
func recipientGroupSQL(alias string) string {
	return fmt.Sprintf(`CASE
		WHEN %[1]s.mail_host IN ('google_workspace', 'gmail') THEN 'google'
		WHEN %[1]s.mail_host IN ('microsoft365', 'outlook') THEN 'microsoft'
		WHEN %[1]s.mail_host IN ('yahoo', 'aol') THEN 'yahoo'
		WHEN %[1]s.mail_host = '' AND %[1]s.provider = 'gmail' THEN 'google'
		WHEN %[1]s.mail_host = '' AND %[1]s.provider = 'outlook' THEN 'microsoft'
		ELSE 'other'
	END`, alias)
}

// majorRecipientSQL holds for a spam report filed at Google, Microsoft or
// Yahoo, the only placements held against a sender.
var majorRecipientSQL = fmt.Sprintf(`EXISTS (
	SELECT 1 FROM email_accounts rec
	WHERE rec.id = sr.reporter_account_id AND %s <> 'other')`, recipientGroupSQL("rec"))

// placementEvidenceSQL selects models.WarmupPlacementEvidence for one sender
// since a time, read through its verified receipts; both are SQL expressions.
func placementEvidenceSQL(sender, since string) string {
	return fmt.Sprintf(`
		SELECT
			COUNT(*) FILTER (WHERE NOT other) AS major,
			COUNT(*) FILTER (WHERE NOT other AND spam) AS major_spam,
			COUNT(*) FILTER (WHERE other) AS other,
			COUNT(*) FILTER (WHERE other AND spam) AS other_spam
		FROM (
			SELECT %[3]s = 'other' AS other, (wr.first_landing = 'spam' OR wr.landed_spam OR sr.id IS NOT NULL) AS spam
			FROM warmup_received wr
			JOIN email_accounts rec ON rec.id = wr.email_account_id
			LEFT JOIN warmup_spam_reports sr
				ON sr.reporter_account_id = wr.email_account_id AND sr.message_id = wr.message_id
				AND sr.report_type = 'spam_placement'
			WHERE wr.sender_account_id = %[1]s AND wr.created_at >= %[2]s AND wr.message_id <> ''
			  AND (wr.first_landing IN ('inbox','tabs','spam') OR wr.landed_spam OR sr.id IS NOT NULL)
		) d`, sender, since, recipientGroupSQL("rec"))
}
