package seed

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func seedEmailAccounts(ctx context.Context, pool *pgxpool.Pool, r *Result) error {
	type account struct {
		id        uuid.UUID
		userID    uuid.UUID
		orgID     uuid.UUID
		workerID  uuid.UUID
		email     string
		name      string
		provider  string
		warmupTag string
		warmupOn  bool
		poolType  string
	}
	accounts := []account{
		{
			id: EmailAcmeAliceID, userID: UserOwnerID, orgID: OrgAcmeID,
			workerID: WorkerDedicatedID,
			email:    "alice@acme.test", name: "Alice from Acme",
			provider: "gmail", warmupTag: "seed-warmup-acme-alice",
			warmupOn: true, poolType: "premium",
		},
		{
			id: EmailAcmeBobID, userID: UserOwnerID, orgID: OrgAcmeID,
			workerID: WorkerSharedID,
			email:    "bob@acme.test", name: "Bob from Acme",
			provider: "smtp_imap", warmupTag: "seed-warmup-acme-bob",
			warmupOn: true, poolType: "premium",
		},
		{
			id: EmailGlobexHansID, userID: UserFounderID, orgID: OrgGlobexID,
			workerID: WorkerFreeID,
			email:    "hans@globex.test", name: "Hans Globex",
			provider: "gmail", warmupTag: "seed-warmup-globex-hans",
			warmupOn: false, poolType: "free",
		},
		{
			id: EmailOwnerSelfID, userID: UserOwnerID, orgID: OrgAcmeID,
			workerID: WorkerSharedID,
			email:    "owner@warmbly.local", name: "Owner Inbox",
			provider: "smtp_imap", warmupTag: "seed-warmup-owner-self",
			warmupOn: false, poolType: "premium",
		},
	}

	acmeCount, globexCount := 0, 0
	for _, a := range accounts {
		_, err := pool.Exec(ctx, `
			INSERT INTO email_accounts (
				id, user_id, organization_id, worker_id, email, name,
				signature_plain, signature_html, signature_sync, signature_code,
				provider, status, campaign_limit, min_wait_time, reply_to,
				tracking_domain, timezone,
				warmup, warmup_base, warmup_max, warmup_increase, warmup_reply_rate,
				warmup_tag, warmup_start_time, warmup_end_time, warmup_days,
				warmup_pool_type,
				created_at, updated_at
			) VALUES (
				$1,$2,$3,$4,$5,$6,
				'-- Seeded sig --', '<p>-- Seeded sig --</p>', TRUE, FALSE,
				$7, 'active', 50, 600, '',
				'localhost:3000', 'UTC',
				CASE WHEN $8 THEN NOW() ELSE NULL END,
				10, 40, 1, 30,
				$9, '08:00', '20:00', 62,
				$10,
				NOW(), NOW()
			)
			ON CONFLICT (id) DO UPDATE SET
				worker_id = EXCLUDED.worker_id,
				warmup = EXCLUDED.warmup,
				warmup_pool_type = EXCLUDED.warmup_pool_type,
				status = 'active',
				updated_at = NOW()
		`,
			a.id, a.userID, a.orgID, a.workerID, a.email, a.name,
			a.provider, a.warmupOn, a.warmupTag, a.poolType,
		)
		if err != nil {
			return err
		}

		switch a.provider {
		case "gmail":
			_, err = pool.Exec(ctx, `
				INSERT INTO email_accounts_oauth (email_account_id, access_token, refresh_token, expires_at, created_at, updated_at)
				VALUES ($1, 'seed-fake-access-token', 'seed-fake-refresh-token', NOW() + INTERVAL '30 days', NOW(), NOW())
				ON CONFLICT (email_account_id) DO UPDATE SET
					access_token = EXCLUDED.access_token,
					refresh_token = EXCLUDED.refresh_token,
					expires_at = EXCLUDED.expires_at,
					updated_at = NOW()
			`, a.id)
		case "smtp_imap":
			_, err = pool.Exec(ctx, `
				INSERT INTO email_accounts_smtp_imap (
					email_account_id, smtp_host, smtp_port, smtp_user, smtp_password,
					imap_host, imap_port, imap_user, imap_password, updated_at
				) VALUES ($1, 'smtp.test.local', 587, $2, 'seed-fake-smtp-password',
					'imap.test.local', 993, $2, 'seed-fake-imap-password', NOW())
				ON CONFLICT (email_account_id) DO UPDATE SET
					smtp_host = EXCLUDED.smtp_host,
					smtp_user = EXCLUDED.smtp_user,
					imap_host = EXCLUDED.imap_host,
					updated_at = NOW()
			`, a.id, a.email)
		}
		if err != nil {
			return err
		}

		if a.orgID == OrgAcmeID {
			acmeCount++
		} else {
			globexCount++
		}
	}

	for i := range r.Organizations {
		if r.Organizations[i].ID == OrgAcmeID.String() {
			r.Organizations[i].Mailboxes = acmeCount
		}
		if r.Organizations[i].ID == OrgGlobexID.String() {
			r.Organizations[i].Mailboxes = globexCount
		}
	}
	return nil
}

func seedWarmupParticipants(ctx context.Context, pool *pgxpool.Pool, _ *Result) error {
	// Resolve pool IDs (the rows are inserted by migration 000010 but with
	// generated UUIDs, so look them up by pool_type).
	rows, err := pool.Query(ctx, `SELECT id, pool_type::text FROM warmup_pools`)
	if err != nil {
		return err
	}
	defer rows.Close()
	poolIDs := map[string]uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		var t string
		if err := rows.Scan(&id, &t); err != nil {
			return err
		}
		poolIDs[t] = id
	}

	join := func(poolType string, accountID uuid.UUID) error {
		pid, ok := poolIDs[poolType]
		if !ok {
			return nil
		}
		_, err := pool.Exec(ctx, `
			INSERT INTO warmup_pool_participants (pool_id, email_account_id, joined_at, spam_score)
			VALUES ($1, $2, NOW(), 0)
			ON CONFLICT (pool_id, email_account_id) DO NOTHING
		`, pid, accountID)
		return err
	}

	if err := join("premium", EmailAcmeAliceID); err != nil {
		return err
	}
	if err := join("premium", EmailAcmeBobID); err != nil {
		return err
	}
	return nil
}
