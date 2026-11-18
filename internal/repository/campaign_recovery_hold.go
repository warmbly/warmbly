package repository

const campaignRecoveryReason = "Waiting for send confirmation. Warmbly will continue this contact automatically when the outcome is confirmed."

// reviewPendingSQL is a lead in a review campaign that nobody has approved yet.
// Like the recovery hold it is its own column, so no other hold's write lifts it.
func reviewPendingSQL(alias string) string {
	return alias + ".review_pending_at IS NOT NULL"
}

func effectiveLeadHold(alias string) string {
	return "(" + liveHold(alias) + " OR " + recoveryPausedLeadSQL(alias) + " OR " + reviewPendingSQL(alias) + ")"
}

func recoveryLeadSinceSQL(alias string) string {
	return alias + ".send_outcome_pending_at"
}

// effectiveLeadHoldColumns projects the hold routing reads: a send recovery
// first, then waiting for review, then the hold slot.
func effectiveLeadHoldColumns(alias string) string {
	recovery := recoveryPausedLeadSQL(alias)
	review := reviewPendingSQL(alias)
	return `CASE WHEN ` + recovery + ` THEN ` + recoveryLeadSinceSQL(alias) + ` WHEN ` + review + ` THEN ` + alias + `.review_pending_at ELSE ` + alias + `.paused_at END,
		CASE WHEN ` + recovery + ` OR ` + review + ` THEN NULL::timestamptz ELSE ` + alias + `.paused_until END,
		CASE WHEN ` + recovery + ` THEN '` + campaignRecoveryReason + `' WHEN ` + review + ` THEN '' ELSE COALESCE(` + alias + `.pause_reason,'') END,
		CASE WHEN ` + recovery + ` THEN 'send_recovery' WHEN ` + review + ` THEN 'review' ELSE COALESCE(` + alias + `.pause_source,'') END`
}

func effectiveLeadHoldJSON(alias string) string {
	return `CASE WHEN ` + recoveryPausedLeadSQL(alias) + ` THEN json_build_object(
		'since',` + recoveryLeadSinceSQL(alias) + `,'until',NULL,'reason','` + campaignRecoveryReason + `','source','send_recovery')
		WHEN ` + reviewPendingSQL(alias) + ` THEN json_build_object('since',` + alias + `.review_pending_at,'until',NULL,'reason','','source','review')
		WHEN ` + liveHold(alias) + ` THEN json_build_object('since',` + alias + `.paused_at,'until',` + alias + `.paused_until,
		'reason',COALESCE(` + alias + `.pause_reason,''),'source',COALESCE(` + alias + `.pause_source,'')) END`
}
