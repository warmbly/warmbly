package repository

// warmupStandingRankSQL orders health states worst first when sorted DESC.
func warmupStandingRankSQL(col string) string {
	return `CASE ` + col + `
			WHEN 'blocked' THEN 5
			WHEN 'quarantined' THEN 4
			WHEN 'throttled' THEN 3
			WHEN 'watch' THEN 2
			WHEN 'healthy' THEN 1
			ELSE 0
		END`
}

// warmupStandingSQL is a mailbox's worst warmup standing, at most one row:
// its local pool row, or the standing Warmbly Cloud last reported for a
// mailbox it warms (which has no local pool row). A standing past its term
// ranks below any live one, so it cannot mask one. Every read that gates or
// shows warmup health goes through it, so a cloud verdict holds here too.
// Columns: health_state, blocked_until, last_health_score,
// last_health_reason, last_health_evaluated_at.
func warmupStandingSQL(accountExpr string) string {
	unknown := `(clm.enrollment_state <> 'active' OR EXISTS (SELECT 1 FROM cloud_link WHERE instance_id = clm.instance_id AND disconnect_pending)
	 OR clm.health_state IS NULL OR clm.health_state NOT IN ('healthy', 'watch', 'throttled', 'quarantined', 'blocked') OR clm.standing_observed_at IS NULL
	 OR clm.standing_observed_at <= NOW() - INTERVAL '15 minutes' OR clm.standing_observed_at > NOW())
	 AND NOT (COALESCE(clm.health_state IN ('quarantined', 'blocked'), false)
	 AND (clm.blocked_until IS NULL OR clm.blocked_until > NOW()))`
	return `
		SELECT st.health_state, st.blocked_until, st.last_health_score,
		       st.last_health_reason, st.last_health_evaluated_at
		  FROM (
		        SELECT wpp.health_state::text AS health_state, wpp.blocked_until,
		               wpp.last_health_score, wpp.last_health_reason, wpp.last_health_evaluated_at
		          FROM warmup_pool_participants wpp
		         WHERE wpp.email_account_id = ` + accountExpr + `
		        UNION ALL
		        SELECT CASE WHEN ` + unknown + ` THEN 'blocked' ELSE clm.health_state END,
		               CASE WHEN ` + unknown + ` THEN NULL ELSE clm.blocked_until END,
		               clm.health_score, CASE WHEN ` + unknown + ` THEN 'cloud_evidence_unavailable' ELSE clm.health_reason END,
		               clm.health_evaluated_at
		          FROM cloud_link_mailboxes clm
		         WHERE clm.email_account_id = ` + accountExpr + `
		        UNION ALL
		        SELECT l.cloud_health_state, l.cloud_blocked_until,
		               l.cloud_health_score, l.cloud_health_reason, NULL::timestamptz
		          FROM warmup_reputation_ledger l JOIN email_accounts ledger_account
		            ON ledger_account.organization_id = l.organization_id AND lower(btrim(ledger_account.email)) = l.email
		         WHERE ledger_account.id = ` + accountExpr + ` AND l.cloud_health_state IS NOT NULL
		           AND (l.cloud_blocked_until IS NULL OR l.cloud_blocked_until > NOW())
		       ) st
		 ORDER BY COALESCE(st.blocked_until <= NOW(), false) ASC,
		          ` + warmupStandingRankSQL("st.health_state") + ` DESC,
		          (st.health_state = 'blocked' AND st.blocked_until IS NULL) DESC,
		          st.blocked_until DESC NULLS LAST
		 LIMIT 1`
}
