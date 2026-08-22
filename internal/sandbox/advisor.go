package sandbox

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/warmbly/warmbly/internal/app/advisor"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/repository"
)

// The Advisor showcase.
//
// Sunrise Labs is otherwise a workspace doing everything right, which makes it
// a poor demo for a feature whose whole job is finding what is wrong. So this
// file introduces a small, deliberate set of realistic problems: three of the
// twenty senders drift out of shape, and one new campaign is written the way a
// first draft actually gets written.
//
// Two rules shape what is safe to break here:
//
//   - Nothing may stop the sandbox from sending. The simulator is the rest of
//     the demo. Caps move up rather than down, gaps shorten rather than widen,
//     and the deliberately-bad campaign is left paused so it never mails
//     anyone.
//   - The problems are real, not painted on. The complaint and bounce cards
//     come from actual task rows and actual deliverability events, so the
//     numbers on the card reconcile with the Deliverability page rather than
//     contradicting it.

// SQL-side UUID namespaces for the generated rows, in the seeder's existing
// "aaaa marks a sandbox row" scheme. Concatenated with a zero-padded index in
// the queries below.
const (
	advisorTaskNS    = `'bbbbbbbb-aaaa-0000-0001-'`
	advisorContactNS = `'66666666-aaaa-0000-0004-'`
)

// Both ids sit clear of every other seeder in this package: seedCampaigns uses
// campaigns 1-3 and steps 11-31, seedExtraCampaigns uses campaigns 4-5 and
// steps 41-51. An id collision here does not fail loudly, it silently rewrites
// somebody else's campaign through the ON CONFLICT clause, so these are worth
// checking against `SELECT id FROM sequences` before changing.
var (
	campaignQ4Draft = uuid.MustParse("44444444-aaaa-0000-0000-000000000006")
	stepQ4Draft     = uuid.MustParse("55555555-aaaa-0000-0000-000000000061")
)

// The three senders that carry the mailbox-side findings. Picked from the end
// of the roster so the sixteen mailboxes the rest of the demo leans on stay
// pristine.
var (
	// mailboxHotList is the one drawing complaints and bounces: real volume,
	// a list that has gone stale, and a cap raised above the safe band.
	mailboxHotList = sandboxMailboxes[17].id // liam.walsh@sunrise.test
	// mailboxUnauthed has never had DMARC published and sends in bursts.
	mailboxUnauthed = sandboxMailboxes[18].id // yuki.sato@sunrise.test
	// mailboxTooEager was connected last week and went straight to full volume
	// with warmup paused.
	mailboxTooEager = sandboxMailboxes[19].id // erik.lund@sunrise.test
)

// q4DraftContactCount sizes the audience for the deliberately-rough campaign: heavy
// on shared inboxes and consumer domains, with a third of it missing the first
// name the copy greets on. Those are the three list checks, and they need a
// hundred-plus contacts before any of them will say a word.
const q4DraftContactCount = 160

// seedAdvisor introduces the demo problems and is idempotent, so re-running
// `make sandbox-seed` converges rather than accumulating.
func seedAdvisor(ctx context.Context, pool *pgxpool.Pool) error {
	if err := seedAdvisorMailboxes(ctx, pool); err != nil {
		return err
	}
	if err := seedAdvisorSendVolume(ctx, pool); err != nil {
		return err
	}
	if err := seedAdvisorRoughCampaign(ctx, pool); err != nil {
		return err
	}
	return nil
}

// runAdvisor evaluates the seeded workspace once and prints what it found, so
// `make sandbox-seed` on its own proves the whole path works instead of leaving
// you to log in and hope. Narration and one-click fixes are left out here: the
// seeder has no LLM provider and no tool registry, and the deterministic copy
// is what a self-hosted install sees anyway. The running backend re-evaluates
// on its own schedule and fills in the rest.
func runAdvisor(ctx context.Context, pool *pgxpool.Pool) error {
	repo := repository.NewAdvisorRepository(&db.DB{Pool: pool})
	svc := advisor.NewService(repo, nil, nil, nil, nil, nil)

	summary, err := svc.Evaluate(ctx, sandboxOrg, "seed")
	if err != nil {
		return fmt.Errorf("advisor evaluate: %w", err)
	}

	findings, xerr := svc.List(ctx, sandboxOrg, repository.AdvisorFindingFilter{Limit: 50})
	if xerr != nil {
		return fmt.Errorf("advisor list: %w", xerr)
	}

	fmt.Printf("  advisor    score %d/100 - %d open (%d critical, %d high, %d medium, %d low)\n",
		summary.Score, summary.Total, summary.Critical, summary.High, summary.Medium, summary.Low)
	for _, f := range findings {
		fix := ""
		if f.Action != nil {
			fix = "  [one-click fix]"
		}
		subject := f.EntityLabel
		if subject == "" {
			subject = "workspace"
		}
		fmt.Printf("             %-8s %s - %s%s\n", f.Severity, subject, f.Title, fix)
	}
	return nil
}

func seedAdvisorMailboxes(ctx context.Context, pool *pgxpool.Pool) error {
	// First, make the fleet look like a workspace that knows what it is doing:
	// at the safe cap, at the default gap, authenticated, on its own tracking
	// domain. The base fixture leaves all twenty at cap 100 with no DNS and no
	// tracking domain, which is not a showcase, and it buries the three
	// problems below under sixty copies of the same card.
	if _, err := pool.Exec(ctx, `
		UPDATE email_accounts
		SET campaign_limit = 50,
		    min_wait_time = 600,
		    warmup_max = 40,
		    tracking_domain = 'track.sunrise.test',
		    tracking_domain_verified = TRUE,
		    tracking_domain_verified_at = NOW() - INTERVAL '30 days',
		    auth_state = 'passing',
		    auth_spf = TRUE, auth_dkim = TRUE, auth_dmarc = TRUE,
		    auth_dmarc_policy = 'quarantine', auth_reason = '',
		    auth_checked_at = NOW() - INTERVAL '6 hours'
		WHERE organization_id = $1`, sandboxOrg); err != nil {
		return fmt.Errorf("advisor healthy baseline: %w", err)
	}

	// The fixture's newest senders are still inside their first month. A
	// workspace ramping them properly holds them near 20/day, so that is where
	// they start; the one mailbox that skipped the ramp is set below.
	if _, err := pool.Exec(ctx, `
		UPDATE email_accounts
		SET campaign_limit = 20
		WHERE organization_id = $1 AND created_at > NOW() - INTERVAL '30 days'`, sandboxOrg); err != nil {
		return fmt.Errorf("advisor young mailbox caps: %w", err)
	}

	// A cap above the safe band and a gap under five minutes. Both raise
	// throughput, so the simulator keeps working.
	if _, err := pool.Exec(ctx, `
		UPDATE email_accounts
		SET campaign_limit = 120, min_wait_time = 600
		WHERE id = $1`, mailboxHotList); err != nil {
		return fmt.Errorf("advisor hot-list mailbox: %w", err)
	}

	// SPF and DKIM pass, DMARC was never published, so the advisor raises its
	// domain-authentication card. auth_failing_since stays NULL so the showcase
	// demonstrates the finding without ever being stopped from sending.
	if _, err := pool.Exec(ctx, `
		UPDATE email_accounts
		SET auth_state = 'failing',
		    auth_spf = TRUE, auth_dkim = TRUE, auth_dmarc = FALSE,
		    auth_dmarc_policy = '', auth_reason = 'no DMARC record found for sunrise.test',
		    auth_checked_at = NOW() - INTERVAL '4 hours',
		    min_wait_time = 90
		WHERE id = $1`, mailboxUnauthed); err != nil {
		return fmt.Errorf("advisor unauthenticated mailbox: %w", err)
	}

	// Connected six days ago, already at the full default cap, warmup paused.
	// Pausing preserves the ramp, so resuming from the card puts it straight
	// back where it was.
	if _, err := pool.Exec(ctx, `
		UPDATE email_accounts
		SET created_at = NOW() - INTERVAL '6 days',
		    campaign_limit = 50,
		    warmup_paused_at = NOW() - INTERVAL '3 days',
		    warmup_max = 20
		WHERE id = $1`, mailboxTooEager); err != nil {
		return fmt.Errorf("advisor new mailbox: %w", err)
	}

	return nil
}

// seedAdvisorSendVolume gives the hot-list mailbox a month of real sending, so
// its complaint and bounce rates are computed from the same rows the rest of
// the platform counts rather than asserted.
//
// 1,600 sends over 30 days with 1 complaint is `0.06%`: past the point where
// the Advisor starts watching, short of the `0.10%` Google treats as a
// ceiling. 72 bounces is `4.5%`: past the watch line, just under the `5%`
// where SES puts an account under review. Two different bands on one mailbox,
// which is what makes the card worth reading.
func seedAdvisorSendVolume(ctx context.Context, pool *pgxpool.Pool) error {
	// Deterministic ids built the same way the rest of the seeder builds them
	// (a fixed namespace plus a zero-padded index), so re-seeding converges.
	// uuid_generate_v5 would need uuid-ossp, which this database never installs.
	if _, err := pool.Exec(ctx, `
		INSERT INTO tasks (id, task_type, email_account_id, status, message_id, created_at, scheduled_at, completed_at)
		SELECT
			(`+advisorTaskNS+` || lpad(n::text, 12, '0'))::uuid,
			'campaign', $1, 'completed',
			'<sbx-advisor-' || n || '@sunrise.test>',
			NOW() - make_interval(mins => n * 27),
			NOW() - make_interval(mins => n * 27),
			NOW() - make_interval(mins => n * 27)
		FROM generate_series(1, 1600) AS n
		ON CONFLICT (id) DO NOTHING`, mailboxHotList); err != nil {
		return fmt.Errorf("advisor send volume: %w", err)
	}

	// Attribute the sends to the launch campaign so the campaign views agree
	// with the mailbox views.
	if _, err := pool.Exec(ctx, `
		INSERT INTO campaign_tasks (task_id, campaign_id)
		SELECT (`+advisorTaskNS+` || lpad(n::text, 12, '0'))::uuid, $1
		FROM generate_series(1, 1600) AS n
		ON CONFLICT (task_id) DO NOTHING`, campaignLaunch); err != nil {
		return fmt.Errorf("advisor campaign tasks: %w", err)
	}

	// 72 hard bounces spread across the month, each hung off one of those
	// tasks so the mailbox attribution join resolves.
	if _, err := pool.Exec(ctx, `
		INSERT INTO deliverability_events
			(organization_id, campaign_id, task_id, event_type, provider, recipient_email, reason, idempotency_key, created_at)
		SELECT
			$1, $2,
			(`+advisorTaskNS+` || lpad((n * 22)::text, 12, '0'))::uuid,
			'bounce', 'sandbox',
			'stale.' || n || '@dormant-domain.test',
			'550 5.1.1 recipient address rejected: user unknown',
			'sbx-advisor-bounce-' || n,
			NOW() - make_interval(mins => n * 22 * 27)
		FROM generate_series(1, 72) AS n
		ON CONFLICT (idempotency_key) DO NOTHING`, sandboxOrg, campaignLaunch); err != nil {
		return fmt.Errorf("advisor bounces: %w", err)
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO deliverability_events
			(organization_id, campaign_id, task_id, event_type, provider, recipient_email, reason, idempotency_key, created_at)
		VALUES (
			$1, $2,
			(`+advisorTaskNS+` || '000000000040')::uuid,
			'complaint', 'sandbox',
			'annoyed.reader@northwind.test',
			'marked as spam',
			'sbx-advisor-complaint-1',
			NOW() - INTERVAL '9 days'
		)
		ON CONFLICT (idempotency_key) DO NOTHING`, sandboxOrg, campaignLaunch); err != nil {
		return fmt.Errorf("advisor complaint: %w", err)
	}

	return nil
}

// q4DraftBody is a first draft written the way first drafts are: too long, too
// many links, phrases lifted from a template, and an {{if}} whose {{end}}
// never got typed. That last one is the point of the copy card: it does not
// fail the send, it silently degrades to literal text in front of a prospect.
const q4DraftBody = `Hi {{.FirstName}},

CONGRATULATIONS on a great year! I wanted to reach out because we have a LIMITED TIME offer that I think you will love.

{{if .Company}}I noticed {{.Company}} has been growing fast.

We help companies just like yours double your pipeline with no obligation and no strings attached. Our platform is 100% free to try, and I can guarantee you will see results in the first week. This is not spam, and there is no risk free trial required to get started.

Here is everything you need to know:

- Our overview deck: https://example.test/deck
- A case study from a company in your space: https://example.test/case-study
- Pricing, which starts lower than you would expect: https://example.test/pricing
- And you can book time with me directly here: https://example.test/book

I know you are busy, so I will keep this short, but I really do think this is a once in a lifetime opportunity for a team like yours to get ahead of the competition before the end of the quarter. Most of the teams we talk to tell us they wish they had started sooner, and the ones who move now are the ones who see the biggest gains over the following twelve months.

Let me know what you think and we can set something up. Act now and I can include the onboarding package at no extra cost, which is something we do not normally offer to new accounts at this stage.

Best regards,
The Sunrise team`

func seedAdvisorRoughCampaign(ctx context.Context, pool *pgxpool.Pool) error {
	// Paused, not active: the copy and shape problems are all the Advisor
	// needs, and a paused campaign cannot mail the synthetic list.
	if _, err := pool.Exec(ctx, `
		INSERT INTO campaigns (
			id, user_id, organization_id, name, description,
			status, days, start_time, end_time, timezone,
			open_tracking, link_tracking, unsubscribe_header,
			updated_at, created_at
		) VALUES (
			$1, $2, $3, 'Q4 outbound (first draft)',
			'Sandbox showcase: a campaign the Advisor has things to say about',
			'paused', 127, '09:00', '17:00', 'UTC',
			TRUE, TRUE, FALSE,
			NOW(), NOW() - INTERVAL '4 days'
		)
		ON CONFLICT (id) DO UPDATE SET
			name = EXCLUDED.name,
			status = 'paused',
			unsubscribe_header = FALSE,
			updated_at = NOW()`, campaignQ4Draft, sandboxUser, sandboxOrg); err != nil {
		return fmt.Errorf("advisor draft campaign: %w", err)
	}

	// One step, no follow-up, and a subject that shouts and runs past the
	// point an inbox list truncates it.
	if _, err := pool.Exec(ctx, `
		INSERT INTO sequences (
			id, campaign_id, organization_id, name, subject,
			body_plain, body_html, wait_after, position, conditions
		) VALUES (
			$1, $2, $3, 'Opener',
			'DON''T MISS OUT!! Our LIMITED TIME offer for your team is closing soon!!',
			$4, $5, 0, 0, '{"branches":[]}'
		)
		ON CONFLICT (id) DO UPDATE SET
			subject = EXCLUDED.subject,
			body_plain = EXCLUDED.body_plain,
			body_html = EXCLUDED.body_html`,
		stepQ4Draft, campaignQ4Draft, sandboxOrg, q4DraftBody, plainToHTML(q4DraftBody)); err != nil {
		return fmt.Errorf("advisor draft step: %w", err)
	}

	// The audience: 25% shared inboxes, 50% consumer domains, 30% with no
	// first name for the copy to greet. Generated rather than listed, because
	// the list checks only speak above a hundred contacts and a hundred and
	// sixty hand-written fixtures would be noise in the diff.
	if _, err := pool.Exec(ctx, `
		INSERT INTO contacts (
			id, user_id, organization_id,
			first_name, last_name, email, company, phone,
			custom_fields, subscribed,
			verification_status, verification_reason, verification_checked_at,
			created_at, updated_at
		)
		SELECT
			(`+advisorContactNS+` || lpad(n::text, 12, '0'))::uuid,
			$1, $2,
			CASE WHEN mod(n, 10) < 3 THEN '' ELSE 'Prospect' END,
			'Number ' || n,
			CASE
				WHEN mod(n, 4) = 0 THEN (ARRAY['info','sales','support','hello'])[1 + mod(n, 4)] || '@company' || n || '.test'
				WHEN mod(n, 2) = 0 THEN 'buyer' || n || '@' || (ARRAY['gmail.com','outlook.com','yahoo.com','hotmail.com'])[1 + mod(n, 4)]
				ELSE 'buyer' || n || '@company' || n || '.test'
			END,
			'Company ' || n, '', '{}', TRUE,
			'valid', 'sandbox fixture address', NOW(),
			NOW() - INTERVAL '4 days', NOW()
		FROM generate_series(1, $3) AS n
		ON CONFLICT (id) DO NOTHING`, sandboxUser, sandboxOrg, q4DraftContactCount); err != nil {
		return fmt.Errorf("advisor draft contacts: %w", err)
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO campaign_leads (campaign_id, contact_id)
		SELECT $1, (`+advisorContactNS+` || lpad(n::text, 12, '0'))::uuid
		FROM generate_series(1, $2) AS n
		ON CONFLICT DO NOTHING`, campaignQ4Draft, q4DraftContactCount); err != nil {
		return fmt.Errorf("advisor draft leads: %w", err)
	}

	return nil
}
