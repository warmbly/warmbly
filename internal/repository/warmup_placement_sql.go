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

// consumerAddressSQL holds for a free Gmail or Outlook.com address, which
// mailhost.Refine tells apart from a business domain on the same provider.
func consumerAddressSQL(alias string) string {
	return fmt.Sprintf(`(lower(split_part(%[1]s.email, '@', 2)) IN ('gmail.com', 'googlemail.com', 'msn.com', 'passport.com', 'windowslive.com')
		OR lower(split_part(%[1]s.email, '@', 2)) ~ '^(hotmail|live|outlook)\.[a-z]{2,3}(\.[a-z]{2})?$')`, alias)
}

// majorRecipientSQL holds for a spam report filed at Google, Microsoft or
// Yahoo by a trusted reporter, the only placements held against a sender.
var majorRecipientSQL = fmt.Sprintf(`EXISTS (
	SELECT 1 FROM email_accounts rec
	WHERE rec.id = sr.reporter_account_id AND %s <> 'other' AND %s)`, recipientGroupSQL("rec"), trustedReporterSQL("rec.id"))

// placementEvidenceSQL selects models.WarmupPlacementEvidence for one sender
// since a time, read through its verified receipts at recipients whose
// placement reports are trusted; both are SQL expressions.
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
			  AND %[4]s
			  AND (wr.first_landing IN ('inbox','tabs','spam') OR wr.landed_spam OR sr.id IS NOT NULL)
		) d`, sender, since, recipientGroupSQL("rec"), trustedReporterSQL("wr.email_account_id"))
}
