package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/models"
)

// WarmupPartnerCounts shares cohort scans while retaining each sender's eligibility and exclusions.
func (r *warmupRepository) WarmupPartnerCounts(ctx context.Context, poolType string, senderIDs []uuid.UUID) (map[uuid.UUID]int, error) {
	out := make(map[uuid.UUID]int, len(senderIDs))
	if len(senderIDs) == 0 {
		return out, nil
	}
	for _, id := range senderIDs {
		out[id] = 0
	}
	orgs, err := r.eligiblePartnerSenders(ctx, poolType, senderIDs)
	if err != nil || len(orgs) == 0 {
		return out, err
	}
	cands, err := r.partnerCohort(ctx, partnerCandidateColumns+`
		FROM warmup_pool_participants wpp
		JOIN warmup_pools wp ON wpp.pool_id = wp.id
		JOIN email_accounts ea ON ea.id = wpp.email_account_id
		WHERE wp.pool_type = $1 AND `+partnerEligibleSQL,
		poolType, models.WarmupPartnerOwnTier, poolType)
	if err != nil {
		return nil, err
	}
	borrowFrom, borrows := models.WarmupPoolBorrowsFrom(poolType)
	if borrows {
		borrowed, err := r.partnerCohort(ctx, partnerCandidateColumns+`
			FROM warmup_pool_participants wpp
			JOIN warmup_pools wp ON wpp.pool_id = wp.id
			JOIN email_accounts ea ON ea.id = wpp.email_account_id
			JOIN organizations o ON o.id = ea.organization_id
			WHERE wp.pool_type = $1 AND `+partnerEligibleSQL+` AND `+partnerProvenSQL,
			borrowFrom, models.WarmupPartnerBorrowed, borrowFrom, config.WarmupPoolFallbackMinAgeDays)
		if err != nil {
			return nil, err
		}
		cands = append(cands, borrowed...)
	}
	returnTo, returnsTo := models.WarmupPoolReturnsTo(poolType)
	if returnsTo {
		returns, err := r.partnerCohort(ctx, partnerCandidateColumns+`
			FROM warmup_pool_participants wpp
			JOIN warmup_pools wp ON wpp.pool_id = wp.id
			JOIN email_accounts ea ON ea.id = wpp.email_account_id
			WHERE wp.pool_type = $1 AND `+partnerEligibleSQL+`
			  AND wpp.email_account_id IN (
			      SELECT wr.sender_account_id FROM warmup_received wr
			      WHERE wr.email_account_id = ANY($2::uuid[])
			        AND wr.created_at >= NOW() - make_interval(days => $3))`,
			returnTo, models.WarmupPartnerReturn, returnTo, senderIDs, config.WarmupPoolReturnVisitDays)
		if err != nil {
			return nil, err
		}
		cands = append(cands, returns...)
	}
	// One cap over every cohort, so the counts are read once for the page.
	cands, err = r.capPartnerCandidates(ctx, poolType, cands)
	if err != nil {
		return nil, err
	}

	ownIDs := make(map[uuid.UUID]bool)
	returns := make(map[uuid.UUID]models.WarmupPartnerCandidate)
	// Bounces are rare, so each sender walks only the bounced few.
	var bounced []models.WarmupPartnerCandidate
	borrowedByOrg := make(map[uuid.UUID]int)
	own, borrowed := 0, 0
	for _, c := range cands {
		if len(c.BouncedBy) > 0 {
			bounced = append(bounced, c)
		}
		switch c.Origin {
		case models.WarmupPartnerOwnTier:
			own++
			ownIDs[c.ID] = true
		case models.WarmupPartnerBorrowed:
			borrowed++
			if c.OrganizationID != nil {
				borrowedByOrg[*c.OrganizationID]++
			}
		case models.WarmupPartnerReturn:
			returns[c.ID] = c
		}
	}
	for id, org := range orgs {
		out[id] = own
		if ownIDs[id] {
			out[id]--
		}
		if borrows {
			out[id] += borrowed - borrowedByOrg[org]
		}
		for _, c := range bounced {
			if !c.BouncedFrom(&org) {
				continue
			}
			switch {
			case c.Origin == models.WarmupPartnerOwnTier && c.ID != id:
				out[id]--
			case c.Origin == models.WarmupPartnerBorrowed && borrows && (c.OrganizationID == nil || *c.OrganizationID != org):
				out[id]--
			}
		}
	}
	if returnsTo {
		rows, err := resultDB(ctx, r.db).Query(ctx, `
			SELECT DISTINCT ea.id, wr.sender_account_id
			FROM warmup_received wr
			JOIN email_accounts ea ON ea.id = wr.email_account_id
			JOIN warmup_pool_participants wpp ON wpp.email_account_id = ea.id
			JOIN warmup_pools wp ON wp.id = wpp.pool_id
			JOIN organizations o ON o.id = ea.organization_id
			WHERE ea.id = ANY($3::uuid[]) AND wp.pool_type = $1 AND ea.status = 'active'
			  AND `+partnerProvenSQL+`
			  AND wr.created_at >= NOW() - make_interval(days => $4)`,
			poolType, config.WarmupPoolFallbackMinAgeDays, senderIDs, config.WarmupPoolReturnVisitDays)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var sender, partner uuid.UUID
			if err := rows.Scan(&sender, &partner); err != nil {
				return nil, err
			}
			if org, eligible := orgs[sender]; eligible {
				if c, ok := returns[partner]; ok && !c.BouncedFrom(&org) {
					out[sender]++
				}
			}
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (r *warmupRepository) eligiblePartnerSenders(ctx context.Context, poolType string, senderIDs []uuid.UUID) (map[uuid.UUID]uuid.UUID, error) {
	rows, err := resultDB(ctx, r.db).Query(ctx, `
		SELECT ea.id, ea.organization_id
		FROM warmup_pool_participants wpp
		JOIN warmup_pools wp ON wp.id = wpp.pool_id
		JOIN email_accounts ea ON ea.id = wpp.email_account_id
		WHERE ea.id = ANY($1::uuid[]) AND wp.pool_type = $2
		  AND wpp.participant_role = 'sender_receiver'
		  AND `+testSenderSQL+` AND `+poolEligibleSQL, senderIDs, poolType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[uuid.UUID]uuid.UUID)
	for rows.Next() {
		var id, org uuid.UUID
		if err := rows.Scan(&id, &org); err != nil {
			return nil, err
		}
		out[id] = org
	}
	return out, rows.Err()
}
