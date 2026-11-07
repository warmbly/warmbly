package repository

const campaignRecoveryReason = "Waiting for send confirmation. Warmbly will continue this contact automatically when the outcome is confirmed."

func effectiveLeadHold(alias string) string {
	return "(" + liveHold(alias) + " OR " + recoveryPausedLeadSQL(alias) + ")"
}

func recoveryLeadSinceSQL(alias string) string {
	return alias + ".send_outcome_pending_at"
}

func effectiveLeadHoldColumns(alias string) string {
	recovery := recoveryPausedLeadSQL(alias)
	return `CASE WHEN ` + recovery + ` THEN ` + recoveryLeadSinceSQL(alias) + ` ELSE ` + alias + `.paused_at END,
		CASE WHEN ` + recovery + ` THEN NULL::timestamptz ELSE ` + alias + `.paused_until END,
		CASE WHEN ` + recovery + ` THEN '` + campaignRecoveryReason + `' ELSE COALESCE(` + alias + `.pause_reason,'') END,
		CASE WHEN ` + recovery + ` THEN 'send_recovery' ELSE COALESCE(` + alias + `.pause_source,'') END`
}

func effectiveLeadHoldJSON(alias string) string {
	return `CASE WHEN ` + recoveryPausedLeadSQL(alias) + ` THEN json_build_object(
		'since',` + recoveryLeadSinceSQL(alias) + `,'until',NULL,'reason','` + campaignRecoveryReason + `','source','send_recovery')
		WHEN ` + liveHold(alias) + ` THEN json_build_object('since',` + alias + `.paused_at,'until',` + alias + `.paused_until,
		'reason',COALESCE(` + alias + `.pause_reason,''),'source',COALESCE(` + alias + `.pause_source,'')) END`
}
