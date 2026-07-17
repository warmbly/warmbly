# Warmbly Deliverability and Abuse Engineering Report: Warmup, Fraud Detection, and Cold Send

## Executive summary

- **Authentication is the single highest-leverage gap across all three areas.** `dnsauth.Check` exists (`internal/pkg/dnsauth/dnsauth.go:39`) but has exactly one caller, the on-demand dashboard endpoint (`internal/api/handler/email_authcheck.go:36`). SPF/DKIM/DMARC/PTR is never persisted, scheduled, or enforced. `email_accounts` has no auth columns. This is a hard Gmail/Yahoo/Outlook requirement (Outlook enforcing since 2025-05-05), the top driver in the 4,406-inbox study, and it gates warmup pool entry, cold send, and is itself a fraud signal. One persisted auth state plus a periodic check + a send gate is the biggest win.
- **A unified mailbox-health state machine already exists and is wired into three feedback loops**, but only on the warmup-pool-participant axis (`internal/app/warmup/service.go:637-787`, `evaluateMetrics`). The same banded model should be extended to (a) cold-only mailboxes that never joined a pool, and (b) a new per-org risk object. The pattern is proven; reuse it rather than build new engines.
- **Throttle-down is open-loop.** The warmup ramp is monotonic `+1/day` (`internal/scheduler/warmup_scheduler.go:66-75`); cold throttling is binary (quarantined skip, throttled halve, `internal/scheduler/campaign_scheduler.go:276-288`). Add proportional reductions on early signal (Postmark/SES "cut 25-30% and hold") and apply the existing `watch` band (0.7x) to cold volume, not just warmup volume.
- **Deliverability feedback is not auto-derived from real send results.** Bounce/complaint only enter via the customer-facing `IngestDeliverabilityEvent` API or warmup probes; the worker's SMTP 5xx/`5.7.515`/DSN/FBL never auto-populate the breaker, suppression, or warmup health. Bridging worker results over the existing Kafka path closes the loop with no schema change.
- **The human/account layer has almost no defense.** Signup is gated only by Turnstile + password policy + per-email cap; `RegistrationStart` receives `ipaddr` and discards it (`internal/app/auth/registration.go:17-88`). Trials auto-grant per fresh email (`internal/app/trial/service.go:51-97`). There is no disposable-email check, no velocity/cross-account correlation, no per-org risk score, no ATO signal. Every gap fills by reusing `BanScope`, the audit spine, and the health-state pattern.
- **Provider-aware routing and per-provider placement data already collected but unused.** `PoolSpamPlacementsByProvider` (`internal/repository/pg_warmup.go:699-725`) feeds only the admin summary; it is not fed back into `pickWeightedPartner`, so a mailbox landing in Outlook spam keeps being routed Outlook recipients until an aggregate band trips.
- **Several CLAUDE.md behaviors are documented but NOT enforced in code** (called out inline): auth gating, per-mailbox health independent of warmup pool, list-quality import gate, worker-level complaint producer, warmup-to-cold graduation gate, recipient-timezone send timing.
- **Already done, do not re-recommend:** RFC 8058 one-click unsubscribe is fully wired (`campaign_task.go:512`, `wmail/send.go:54-58`, org-wide suppression); spintax is expanded per-recipient at send (`campaign_task.go:421-423`, `spintax.go`); warmup content lint exists via `warmlint.Check` (warmup-only).

---

## 1. Making warmup more effective

### Where we are today

Warmbly's warmup is unusually mature and most of CLAUDE.md is genuinely implemented, not aspirational. Verified:

- The banded health state machine (healthy/watch/throttled/quarantined/blocked) is real (`internal/app/warmup/service.go:29-68` for thresholds, `:637-787` for `evaluateMetrics`) and drives warmup pool selection, cold-send throttling, and risk-band -> worker-pool segregation.
- Spam-folder placement is measured on arrival via `containsSpamFlag` and segmented per recipient provider/domain into `warmup_spam_reports` (`internal/app/consumer/event_new_email.go:158-164`).
- Partner diversity (`recentPartnerWindow = 72h`, `partnerMaxSharedWindow = 3`, `smallPoolWarnThreshold = 8`, all in `internal/tasks/email_task.go:347-371`), inverse-domain-frequency weighting, persona-biased engagement, and conversational replies all exist.
- Health dampening is real: `adjustmentFor` (`internal/scheduler/warmup_scheduler.go:30-43`) maps throttled -> 0.5x volume / 2x gap, watch -> 0.7x / 1.5x.

The warmup is strong at **protecting** reputation (quarantining bad mailboxes) but weaker at **building and convincingly simulating** it. Against 2025-2026 best practice the gaps are: open-loop monotonic ramp, no provider-aware routing despite collecting the data, content bounded by ~30 templates with shallow 1-2 message threads, reactive pool admission with no auth bar, no warmup-to-cold graduation gate, and small-pool degeneration with only a log warning.

### 1.1 Authentication-health pool-entry and send gate (reusing `dnsauth.Check`)

- **Problem:** `EnsurePoolMembershipWithRole` (`internal/app/warmup/service.go:199-218`) auto-admits any paid/trial mailbox with zero bar on auth, age, or reputation, and `HandleEmailTask` ensures membership for every eligible account (`internal/tasks/email_task.go:130-133`). `dnsauth.Check` is wired only to the dashboard (`internal/api/handler/email_authcheck.go:18-37`). **CLAUDE.md's re-entry checklist requires auth health but code does not enforce it.** Authentication is the #1 deliverability driver (4,406-inbox study) and a hard Gmail/Yahoo/Outlook requirement (Google sender guidelines, effective Feb 1 2024; Outlook `5.7.515` enforcement from May 5 2025).
- **Proposal:** Compute a periodic per-sending-domain auth result, persist it, and (a) refuse warmup pool sender participation and (b) block cold send for a mailbox whose domain fails SPF+DKIM presence + DMARC >= p=none alignment. Surface in the existing health/realtime surfaces.
- **Implementation:** Additive migration adding to `email_accounts`: `auth_state text CHECK IN ('unknown','passing','failing')`, `auth_spf/auth_dkim/auth_dmarc bool`, `auth_dmarc_policy text`, `auth_checked_at timestamptz`, `auth_reason text`. Run `dnsauth.Check` from a daily consumer sweep keyed by sending domain (alongside the hourly `EvaluateAllParticipants` in `internal/app/consumer/warmup_health_sweep.go:11-26`), cached per domain to avoid DNS spam. In `CanParticipate` (`internal/app/warmup/service.go:234-280`) return `(false, "auth_failed")` when `auth_state='failing'`, which auto-excludes from both the sender gate (`internal/tasks/email_task.go:135`) and recipient re-gate (`:486`). Add the same check beside the `GetHealthState` gate in `internal/scheduler/campaign_scheduler.go:276`. Emit the existing `AUDIT_CREATED` / account-health realtime path (web spine already maps `email_account`). DNS check runs in consumer/backend, never the worker, preserving the worker boundary.
- **Effort:** L  **Impact:** critical
- **Code anchors:** `internal/app/warmup/service.go:199-218,234-280`; `internal/api/handler/email_authcheck.go:18-37`; `internal/tasks/email_task.go:130-145`; `internal/scheduler/campaign_scheduler.go:276-288`; `internal/pkg/dnsauth/dnsauth.go:39`

### 1.2 Close the warmup ramp loop: proportional throttle-down on early adverse signal

- **Problem:** `warmupRampTarget` (`internal/scheduler/warmup_scheduler.go:66-75`) is monotonic: `min(base + daysWarming*increase, max)`. The only downward pressure is `adjustmentFor` (0.7x / 0.5x), which fires only AFTER `evaluateMetrics` crosses the 10%/15% placement or 0.03%/0.5% complaint bands. There is no Postmark/SES "metrics degraded, cut 25-30% and hold" behavior. Postmark explicitly recommends "decrease your volume by 25-30% until metrics begin to normalize" (soft heuristic, 2025-2026); SES says "send less if you see throttling" (AWS docs, current).
- **Proposal:** When a mailbox has any spam placement in the trailing 48h (even below the 20-send floor) or a non-zero placement rate under the watch band, hold `target_volume` at the current level and apply a one-step ~25% reduction for ~3 days, then resume `+1/day`. This sits between "no signal" and the existing watch band, reusing data already in `warmup_spam_reports`.
- **Implementation:** Add `warmupRepo.RecentPlacementSignal(ctx, accountID, since=48h)` returning `(placements, sends)`. In `CalculateNextWarmupTime` (`internal/scheduler/warmup_scheduler.go:149-198`), after `resolveHealthState`, if `placements>0` and state is still Healthy, multiply `targetVolume` by ~0.75 and freeze the ramp by clamping `daysWarming` so `base+daysWarming*increase` does not advance vs the last sent day (derive from `warmup_statistics.target_volume`, already persisted). Keep `warmupRampTarget` pure; put the multiplier in a new `softRampAdjust(placements, sends) float64` helper alongside `adjustmentFor`. No new band/state.
- **Effort:** M  **Impact:** high
- **Code anchors:** `internal/scheduler/warmup_scheduler.go:30-43,66-75,149-198`; `internal/repository/pg_warmup.go:699-725`

### 1.3 Provider-aware partner routing (feed per-provider placement back into selection)

- **Problem:** `PoolSpamPlacementsByProvider` (`internal/repository/pg_warmup.go:699-725`) and the per-(provider,domain) `RecordSpamPlacement` (`internal/app/consumer/event_new_email.go:158-164`) already capture exactly where each sender lands in spam, but that data only powers the admin summary. `pickWeightedPartner` (`internal/tasks/email_task.go:514-570`) weights only by inverse domain frequency, not by where THIS sender is currently failing. A mailbox landing in Outlook spam keeps being routed Outlook recipients until an aggregate band trips. MailReach methodology (vendor, 2025-2026) cites ESP-specific placement analytics + adaptive warmup as a core lever.
- **Proposal:** Add a per-sender, per-recipient-provider placement penalty to the partner weight. A sender with recent placements at provider X gets X recipients downweighted (not excluded, to keep the signal fresh) until placement recovers. Additive to the existing diversity + routing weighting.
- **Implementation:** Add `warmupRepo.SenderPlacementByProvider(ctx, senderAccountID, since 7d)` (mirror `PoolSpamPlacementsByProvider` filtered by `reported_account_id = sender`). In `selectWarmupPartner` (`internal/tasks/email_task.go:381-499`) load it once, pass into `pickWeightedPartner`, resolve candidate provider via existing `domainsByID`/`emailsByID` maps. In the weight loop multiply `w` by `1/(1 + k*placementRateForProvider)`. Best-effort with uniform fallback so a lookup error never stalls warmup. No schema change (`warmup_spam_reports` already stores `recipient_provider`).
- **Effort:** M  **Impact:** high
- **Code anchors:** `internal/tasks/email_task.go:381-499,514-570`; `internal/repository/pg_warmup.go:699-725`; `internal/app/consumer/event_new_email.go:158-164`

### 1.4 Warmup-to-cold graduation gate (no overnight spike from ceiling to full cap)

- **Problem:** Cold sending only reads warmup HEALTH (`internal/scheduler/campaign_scheduler.go:276-288`), never whether the mailbox warmed enough or placed in inbox. A freshly-warmed mailbox at `WarmupMaxDefault=40` can join a campaign and immediately send at `CampaignLimitDefault=50` (`internal/config/constants.go:9-13`). **CLAUDE.md and external sources warn against exactly this post-warmup spike**; SES and Postmark both say not to jump to full volume the moment warmup "completes" (AWS docs / Postmark guide, current).
- **Proposal:** Introduce a per-mailbox cold-readiness ceiling derived from warmup maturity + recent placement, folded into `effectiveCap` via `min()`. A mailbox graduates from warmup ceiling toward full cold cap over ~1-2 weeks of clean placement instead of jumping. Reuse `warmup_statistics` and `warmup_spam_reports`.
- **Implementation:** Add `coldReadinessCeiling(account, warmupAgeDays, recentPlacementRate) int` in the scheduler: for the first N days of campaign activity on a freshly-warmed mailbox, cap at warmup ceiling and step up ~+5/day toward `CampaignLimit` only while placement stays clean (mirror the idempotent-per-UTC-day pattern at `internal/scheduler/campaign_scheduler.go:98-149`). Wire as one more `min()` term in `effectiveCap` (`:227-267`). Track warmup age from the existing `account.Warmup` anchor (already used at `internal/scheduler/warmup_scheduler.go:153`) plus an additive `email_accounts.cold_ramp_started_at`. Keep warmup at a maintenance floor after graduation via the existing `health_check` lane (`activeCampaignWarmupCap=5`).
- **Effort:** M  **Impact:** high
- **Code anchors:** `internal/scheduler/campaign_scheduler.go:98-149,227-267`; `internal/scheduler/warmup_scheduler.go:60-75`; `internal/config/constants.go:9-13`

### 1.5 Protect small/early pools (scale volume down, not just log)

- **Problem:** `smallPoolWarnThreshold=8` (`internal/tasks/email_task.go:360`) only logs. Below ~8 participants, the preferred tier empties and falls back to today-unused partners (`:461`), collapsing diversity into near-uniform reciprocal reuse, the closed-loop graph signal warmup is meant to avoid. Separately, `targetVolume` is capped to eligible-recipient count (`internal/scheduler/warmup_scheduler.go:163-172`), so the ramp can never reach 40 on a small pool.
- **Proposal:** Turn the warning into a volume/behavior policy: below the diversity threshold, cap warmup volume lower, widen spacing, and suppress cold graduation (1.4) until pool depth recovers. Reuse the existing dampening machinery.
- **Implementation:** In `CalculateNextWarmupTime`, after `CountEligibleRecipients`, if `eligibleRecipients < smallPoolWarnThreshold` apply a pool-depth multiplier to `targetVolume` and a min-gap stretch (reuse the `healthAdjustment` shape at `:30-43`). Export `smallPoolWarnThreshold` from the tasks package or define a shared const. Control-plane scheduling only; no worker or schema change.
- **Effort:** S  **Impact:** medium
- **Code anchors:** `internal/tasks/email_task.go:357-399,450-473`; `internal/scheduler/warmup_scheduler.go:163-198`

### 1.6 Deepen conversation realism (multi-turn threads, reply-back drive)

- **Problem:** `shouldReply` is a per-send coin flip on `WarmupReplyRate` (`internal/tasks/email_task.go:171-212`); `GetLatestReplyCandidate` (`internal/repository/pg_warmup.go:1156-1189`) returns only the single latest send so threads rarely exceed 2 messages; there is no mechanism for a recipient to reply back to a fresh send. The AI bank supports `MaxMessagesPerThread=6` but the send path uses one description + one question. Genuine back-and-forth is what providers reward (MailReach, vendor, 2025-2026).
- **Proposal:** Let threads run 3-5 turns: on a verified receipt, occasionally schedule a reply-back from recipient to sender, and draw reply bodies by turn index through the same `Conversation.Messages` slice.
- **Implementation:** In `handleWarmupEmail` (`internal/app/consumer/event_new_email.go:117-169`), with some probability enqueue a reply-back warmup task (control-plane enqueue, reusing token threading via `ConversationID`/`ConversationTheme`). In `GenerateConversationEmail`, advance through `Conversation.Messages` by turn index rather than `message[0]`. Persist an additive `warmup_tokens.conversation_turn int default 0`. Cap at `MaxMessagesPerThread`. All content through `lintWarmupContent`. Worker boundary preserved.
- **Effort:** L  **Impact:** medium
- **Code anchors:** `internal/tasks/email_task.go:171-212`; `internal/repository/pg_warmup.go:1156-1189`; `internal/app/consumer/event_new_email.go:117-169`; `internal/tasks/warmup_content.go:57-93`

### 1.7 Naturalize intra-day send spacing

- **Problem:** `CalculateNextWarmupTime` distributes the day's sends evenly (`idealIntervalHours = hoursRemaining/remainingSlots`) with only +/-15min jitter, all inside one 08:00-20:00 window. An even cadence is itself a detectable pattern; consensus (Smartlead/MailReach/Puzzle, soft heuristics, 2025-2026) is bursty-then-quiet with wider variance, weighted Tue-Thu.
- **Proposal:** Draw a randomized inter-send gap from a distribution, widen jitter, add light weekday weighting. Keep `MinWaitTime` as the hard floor.
- **Implementation:** In `CalculateNextWarmupTime` steps 5-8, draw the gap as a randomized multiple of `idealIntervalHours` (e.g. uniform `[0.5x, 1.6x]`) floored by `minWaitSeconds`, widen jitter. Reuse the cold scheduler's `applyDistributionCurve` (`internal/scheduler/helpers.go:99-201`). Optionally weight weekends down in `warmupRampTarget`. Pure scheduling math; no schema/worker change.
- **Effort:** S  **Impact:** medium
- **Code anchors:** `internal/scheduler/warmup_scheduler.go:212-271`; `internal/scheduler/helpers.go:99-201`; `internal/config/constants.go:8`

### 1.8 Record placement without a Junk flag and don't lose it on unassigned recipients

- **Problem:** Placement is recorded only when `containsSpamFlag` is true (`internal/app/consumer/event_new_email.go:158-164`), and `performWarmupActions` returns early when the recipient mailbox has no worker. Gmail CATEGORY_* tab placement is not synced (Promotions reads as inbox); a mid-migration recipient yields no signal. The health model under-counts placements, weakening the very signal 1.2 and 1.3 depend on.
- **Proposal:** Separate receipt recording from worker-dependent engagement, emit an observability counter when `workerID` is nil, and classify Promotions/Updates as a softer, lower-weight non-inbox placement where labels are surfaced.
- **Implementation:** In `handleWarmupEmail`, keep receipt recording unconditional, promote the nil-worker log to a counter. Add a sibling `classifyPlacement` recognizing provider category labels; record `report_type='promo_placement'` with a smaller score delta and its own optional band in `evaluateMetrics`. Consumer-side, additive, degrades gracefully.
- **Effort:** M  **Impact:** medium
- **Code anchors:** `internal/app/consumer/event_new_email.go:149-167,288-300`; `internal/app/warmup/service.go:286-309,736-787`

---

## 2. Detecting malicious users better

### Where we are today

Warmbly has strong mailbox/sending-reputation defenses but almost nothing at the human/account layer, which is where signup/trial/payment fraud lives. Verified:

- Signup is gated ONLY by Turnstile + password policy + a per-email send cap. `RegistrationStart` receives `ipaddr` and discards it; the only email check is `mail.ParseAddress` (`internal/app/auth/registration.go:17-88`). No disposable/MX check, no normalization.
- Trials auto-grant with zero abuse coupling: `RegistrationConfirm` unconditionally calls `StartFreeTrialWithOrg`, which only checks `users.free_trial_used` per user (`internal/app/trial/service.go:51-97`). A fresh email = a fresh trial.
- Pre-send address verification exists (`internal/pkg/emailverify/emailverify.go`) and the send path drops `invalid`/`risky` (`internal/tasks/campaign_task.go:296-323`), but there is NO list-level scoring and no gate on import.
- Enforcement primitives are underused: `BanScope` bitmask (`internal/models/admin.go:114-125`) enforced at login, org create, and send.
- A banded health + `risk_band` exists for mailboxes and workers, with feedback. There is NO equivalent risk object for the user or org.
- The audit spine (`AuditEntityUser`/`AuditEntityOrganization` already defined) drives realtime invalidation, a ready-made delivery mechanism for risk-state changes.

Net: abuse is caught only after a mailbox starts sending and accrues per-mailbox negative signals. A captcha-solving actor can farm unlimited trials, share one IP/ASN across many orgs, upload scraped lists, and weaponize a taken-over org, all invisible until reactive bands trip. Every gap fills by reusing existing mechanisms.

### 2.1 Per-org risk state machine mirroring the mailbox health model

- **Problem:** Every abuse control is siloed per-mailbox/per-user/per-source. No object fuses signup, trial, payment, list-quality, velocity, and sending signals into one decision surface, so an actor bad on several weak axes is never escalated. **CLAUDE.md describes per-mailbox health but no org-level equivalent exists in code.** SaaS-fraud best practice (MyEmailVerifier/Clearout, 2025) is risk-score tiering that constrains rather than hard-blocks.
- **Proposal:** Org-level risk state (trusted/watch/restricted/suspended): watch = monitored, restricted = lower caps + no premium warmup pool + no shared-worker concentration, suspended = sets `BanScopeSend`. Drive it from signup/trial/velocity/list-quality signals and emit via the audit spine.
- **Implementation:** New migration adding to `organizations` (or a new `org_risk` table): `risk_state text CHECK IN ('trusted','watch','restricted','suspended') DEFAULT 'trusted'`, `risk_score int`, `risk_reason text`, `last_evaluated_at timestamptz`, `signals jsonb` (evidence blob; discriminator `risk_state` is a CHECKed column, evidence is read-then-display jsonb per the CLAUDE.md modeling rule). New `internal/app/orgrisk/service.go` modeled on `internal/app/warmup/service.go`: `EvaluateOrg(ctx, orgID)` computes a banded score. Wire `restricted` into `effectiveCap` (`internal/scheduler/campaign_scheduler.go:227`) as another `min()` input, into `resolveWarmupPoolType` (`internal/tasks/email_task.go:584`) to force the free pool, and `suspended` into the `BanScopeSend` check (`internal/app/emailsend/service.go:88`). On change call `auditOrg` with a new `AuditEntityOrgRisk` entity type (add to `internal/models/audit.go` AND the frontend spine map per CLAUDE.md).
- **Effort:** L  **Impact:** critical
- **Code anchors:** `internal/app/warmup/service.go:29-68,637-787`; `internal/scheduler/campaign_scheduler.go:227-288`; `internal/tasks/email_task.go:584-598`; `internal/app/emailsend/service.go:85-90`; `internal/models/audit.go:50-93`

### 2.2 Signup-time risk gate (disposable email + IP/ASN reputation + metadata capture)

- **Problem:** `RegistrationStart` accepts `ipaddr` and silently drops it; the only address check is `net/mail.ParseAddress`. ~33% of freemium signups use disposable domains and IPQS reports ~99.99% correlation between temp-email and abuse (Clearout/Castle, 2025). This is the cheapest, highest-ROI gate and is entirely missing.
- **Proposal:** Persist signup IP + UA, score the email against a maintained disposable-domain list (Castle's open-source list) plus cheap heuristics (temp/throwaway keywords, random-char localpart, risky TLDs, plus/dot Gmail normalization), and score the IP/ASN for proxy/VPN/datacenter. Do NOT hard-block on a single signal; feed the result into `risk_score`. A high-confidence disposable hit can start the org in `watch`.
- **Implementation:** Migration adds `users.signup_ip inet`, `users.signup_user_agent text`, `users.signup_email_risk int`. New `internal/pkg/signuprisk/signuprisk.go` (control-plane, no Postgres) exposing `Score(email, ip)` returning `{DisposableHit bool, IPRiskScore int, Reasons []string}`; embed a refreshable disposable set and use `net.LookupMX` (reuse `internal/pkg/emailverify/emailverify.go:214`). Call it in `RegistrationStart` right after captcha (`internal/app/auth/registration.go:27`) and persist on user create. Lean on Stripe Radar for raw card fraud. Keep the IP-reputation source pluggable.
- **Effort:** M  **Impact:** high
- **Code anchors:** `internal/app/auth/registration.go:17-88,122-158`; `internal/pkg/emailverify/emailverify.go:214-251`

### 2.3 Cross-account velocity + soft-link correlation (nightly batch)

- **Problem:** All rate limits are per single user/mailbox/source. No aggregate detector for bursts of signups, mass mailbox connections, or many orgs sharing one IP/ASN/payment fingerprint. Trial-cyclers and snowshoe rings stay under every per-entity threshold. Trials auto-grant (`internal/app/trial/service.go:51`) with nothing tying them to a device/IP/prior-account graph. Velocity checks across shared identifiers are the workhorse of coordinated-abuse detection (SEON, 2025).
- **Proposal:** Redis velocity counters at signup (signups per IP/ASN/hour, mailboxes per org/hour, imports per org/day) and a nightly batch clustering orgs by shared `signup_ip`/ASN, Stripe payment fingerprint, and near-identical patterns. A cluster of N orgs sharing an identifier raises every member's `risk_score`. Avoid graph-ML; a shared-identifier SQL self-join captures most value.
- **Implementation:** Reuse the Redis rate-limit plumbing (`internal/app/ratelimit/service.go:133-208`) for at-signup INCRs in `RegistrationStart` and at mailbox-add (`internal/app/worker/assignment.go`). New `internal/jobs/account_correlation.go` (runs alongside `internal/jobs/email_verification.go:19-40` in the consumer job loop) joining `users` by `signup_ip`/ASN and `subscriptions` by `stripe_customer_id`/fingerprint, writing linked ids into the risk `signals` jsonb. For snowshoe, query `email_accounts` for orgs connecting many freshly-registered lookalike domains each sending just under `CampaignLimitDefault(50)`.
- **Effort:** L  **Impact:** high
- **Code anchors:** `internal/app/trial/service.go:51-97`; `internal/app/ratelimit/service.go:133-208`; `internal/jobs/email_verification.go:19-40`; `internal/app/worker/assignment.go:75-195`

### 2.4 Promote address-level verification into a list-level quality gate on import

- **Problem:** `emailverify` verifies addresses and the send path drops `invalid`/`risky` (`internal/tasks/campaign_task.go:296-323`), but verification is reactive per-address on a slow batch. There is NO list-level scoring and no inspection on import (`internal/app/contact/import.go`, `internal/app/contact/handler.go:15`). A scraped list burns shared-pool reputation before the first bounce, and a high bad-address share is itself a strong bad-actor signal. SES: "even a small number of spamtrap hits triggers review" (AWS docs, current).
- **Proposal:** At import commit, compute a list-quality score (share of syntactically-invalid, disposable, role, catch-all, predicted-bounce) using the disposable check from 2.2 plus existing `emailverify` results. Surface to the user, quarantine sending on lists over threshold, and feed a high bad share into `risk_score`. Reuse the `verification_status` column so already-verified contacts cost nothing.
- **Implementation:** In the import commit path (`internal/app/contact/import.go`, `internal/app/contact/handler.go:15`), after parsing run a synchronous syntax+disposable+role+TLD pass (no SMTP), compute aggregate ratios, persist a per-import summary (new `contact_imports` table or jsonb summary), and emit an audit event. Block/flag when invalid+disposable share exceeds a constant (e.g. 25%). Keep heavy SMTP RCPT on the existing background job. Wire the ratio into `orgrisk.EvaluateOrg`. Document the threshold in `guides/` per CLAUDE.md.
- **Effort:** M  **Impact:** high
- **Code anchors:** `internal/app/contact/import.go`; `internal/app/contact/handler.go:15-43`; `internal/tasks/campaign_task.go:291-323`; `internal/pkg/emailverify/emailverify.go:147-208`

### 2.5 Account-takeover signals at login (impossible-travel, new-device/new-ASN) behind step-up auth

- **Problem:** No impossible-travel, new-device, or new-ASN detection. A hijacked org owns warmed mailboxes and DEK-decryptable secrets and can weaponize trusted infrastructure for phishing. Today password reset/change only revoke sessions reactively (`internal/app/auth/reset_password.go`) and 2FA is opt-in. `LoginConfirm` captures `ipaddr` + `userAgent` (`internal/app/auth/login.go:85,144`) but does nothing anomaly-aware. ATO is now the #1 US fraud type (Fingerprint, 2025-2026).
- **Proposal:** On successful login, compare new IP/ASN/geo against recent history; flag impossible-travel, new device/ASN, or VPN/proxy on a clean account, and require step-up re-auth (the existing TOTP pending-challenge flow). Also require re-auth for mailbox-credential changes and first mass-send from a new device. Feed repeated flags into `risk_score`.
- **Implementation:** Migration adds a `login_history` table (`user_id, ip inet, asn int, geo, user_agent, created_at`) or reuse the session store. In `LoginConfirm` (`internal/app/auth/login.go:117-147`), after the ban-scope check and before `GenerateSession`, call a new `internal/app/authrisk` check returning `{StepUp bool, Reasons []string}`; if `StepUp`, route into the existing single-use pending-2FA challenge path (`:117-130`). Record the anomaly via audit. All geo/ASN lookups on the control plane.
- **Effort:** M  **Impact:** high
- **Code anchors:** `internal/app/auth/login.go:85-147`; `internal/app/auth/reset_password.go:119-178`

### 2.6 Require a payment method (or risk-gated trial) and own SaaS-native payment signals

- **Problem:** `RegistrationConfirm` auto-grants a trial with no payment coupling (`internal/app/trial/service.go:51-97`). Faking a payment method is far harder than faking an email, so card-on-trial is the single biggest lever against trial-cycling, yet the free tier gates nothing on payment. No SaaS-native behavioral signals (paid-then-never-sent, subscribed-then-immediately-blasted, BIN-vs-IP mismatch). Requiring a card "dramatically reduces trial abuse" (Clearout/SEON, 2025).
- **Proposal:** Either require a Stripe payment method to start a trial, or make the trial risk-gated: low-risk orgs get the full trial; flagged orgs get a reduced trial or must add a card. Lean on Stripe Radar for raw card fraud; own the behavioral signals.
- **Implementation:** In `StartFreeTrialWithOrg` (`internal/app/trial/service.go:51`) read the org `risk_state` (2.1); if `watch`/`restricted`, skip auto-trial and require a card (Stripe SetupIntent via `internal/api/handler/subscription.go`) or grant a shortened trial. Add a behavioral signal in the campaign send path: a brand-new org's first campaign exceeding a large-list threshold within hours of paying bumps `risk_score` and throttles via the restricted cap. BIN-country vs signup-IP-country comes from Stripe webhook data joined to `users.signup_ip`. Document the trial-gating change in `guides/`.
- **Effort:** M  **Impact:** medium
- **Code anchors:** `internal/app/trial/service.go:51-97`; `internal/app/auth/registration.go:152-158`; `internal/api/handler/subscription.go`; `internal/models/subscriptions.go:16-30`

### 2.7 Fail-closed for risk-critical checks and capture evidence for admin review

- **Problem:** API rate limiting (`internal/api/middleware/ratelimit.go:14-87`), WS limiting, the worker sync limiter, and tracking dedupe all FAIL OPEN on Redis/DB error, and the API limiter is skipped entirely when there is no user id (unauthenticated routes). An attacker who induces a Redis hiccup or targets unauthenticated endpoints bypasses throttling exactly under load. Automated actions don't always record enough structured evidence for the admin review CLAUDE.md mandates.
- **Proposal:** For the small set of abuse-critical checks (signup gate, trial grant, suspension enforcement) prefer fail-closed/fail-to-conservative, and ensure every automated risk action writes structured evidence into the risk `signals` jsonb and the admin audit log.
- **Implementation:** In `RegistrationStart`/`StartFreeTrialWithOrg`, if the signup-risk or velocity backend errors, default to `watch` not `trusted`. Keep the high-throughput API/WS limiters fail-open (availability matters there) but add a fail-closed branch for auth-sensitive entry points. Ensure `orgrisk.EvaluateOrg` and any auto-suspend write the evidence blob and call the admin audit logger (`internal/app/admin/service.go` `logAction` pattern). Surface in admin user/org detail (`internal/api/handler/admin.go`). Control-plane only.
- **Effort:** S  **Impact:** medium
- **Code anchors:** `internal/api/middleware/ratelimit.go:14-87`; `internal/app/auth/registration.go:27-29`; `internal/app/admin/service.go:86-238`; `internal/api/handler/admin.go:68-141`

---

## 3. Sending cold emails more effectively

### Where we are today

The cold-send pipeline is mature on mechanics, weak on the closed-loop fundamentals providers gate on. Verified existing: a per-tick single-email Cloud-Tasks chain (`internal/tasks/campaign_task.go:23`), per-mailbox `effectiveCap` (`internal/scheduler/campaign_scheduler.go:227`, `min()` only), 600s min-gap enforced three ways, +/-20min jitter, a distribution curve, weighted/round_robin/LRU rotation, per-campaign ramp, suppression + invalid/risky pre-send gates, and a deliverability circuit breaker (`internal/app/advanced/service.go:1127`).

**Already done, do not re-recommend:** RFC 8058 one-click unsubscribe is fully wired (`campaign_task.go:512`, `publisher.go:122`, `wmail/send.go:54-58` emit both `List-Unsubscribe` and `List-Unsubscribe-Post`, `unsubscribe.go` writes org-wide suppression); spintax expands per-recipient at send (`campaign_task.go:421-423`, `spintax.go`).

The verified gaps for this dimension are below.

### 3.1 SPF/DKIM/DMARC/PTR as a persisted pre-send gate (shared with 1.1)

- **Problem:** `dnsauth.Check` has a single caller, the dashboard endpoint (`internal/api/handler/email_authcheck.go:36`). Auth is never stored, scheduled, or enforced; `email_accounts` has no auth columns, so a mailbox with broken alignment sends cold mail at full volume. This is the single biggest silent deliverability failure and a hard Gmail/Yahoo/Outlook requirement (Outlook `5.7.515` from 2025-05-05). **CLAUDE.md treats auth as required; code does not enforce it.**
- **Proposal:** Persist a per-mailbox auth state from a periodic `dnsauth` check, surface it, and gate cold send + warmup-pool entry on it (this is the same gate as recommendation 1.1, serving both dimensions).
- **Implementation:** See 1.1. The cold-send side adds the `auth_state='failing'` skip beside the `GetHealthState` gate at `internal/scheduler/campaign_scheduler.go:276`. Update `guides/` mailbox page; note in `api/endpoints` if a status field is exposed.
- **Effort:** L  **Impact:** critical
- **Code anchors:** `internal/pkg/dnsauth/dnsauth.go:39`; `internal/api/handler/email_authcheck.go:36`; `internal/scheduler/campaign_scheduler.go:276`; `internal/app/warmup/service.go:234`; `internal/app/consumer/warmup_health_sweep.go:11`

### 3.2 Auto-derive bounce/complaint events from real send results and DSN/FBL sync

- **Problem:** `IngestDeliverabilityEvent` (`internal/app/advanced/service.go:967`), the circuit breaker (`:1127`), and the warmup bounce/complaint bands only fire when a customer calls the external API. `recordSendOutcome` (`internal/app/worker/health_record.go:25`) updates worker counters but never feeds the org deliverability path, and `RecordComplaint` (`internal/app/worker/health.go:96`) has no producer. A campaign can keep blasting a bouncing list while the breaker, auto-suppression, and warmup health stay silent. **CLAUDE.md describes the worker complaint counter but it has no producer in code.**
- **Proposal:** Bridge the worker's actual SMTP result (5xx hard reject, `5.7.515` auth failure) and synced DSN/FBL messages into the existing `IngestDeliverabilityEvent` path so the breaker, suppression, and warmup health populate automatically with no customer wiring.
- **Implementation:** Keep the worker SQL-free: the worker already publishes send results over Kafka (`internal/app/worker/event_send_email.go:92`). In the consumer (`internal/app/consumer/event_email_error.go` or a new `event_send_result` handler), classify the result, mapping recipient-rejected/550 to a `bounce` event and `5.7.515` to an `auth_failure` that flips `auth_state` to `failing` (3.1) instead of a transient retry. Call the existing `advanced.IngestDeliverabilityEvent` with an idempotency key derived from `task_id` so the breaker, suppression, and warmup `ApplySpamReport` (`:1063`) all run unchanged. For async DSN/FBL, add a lightweight parser on the mailbox-sync inbound path (`event_new_email.go` region) recognizing provider bounce/FBL by headers, emitting the same event. No schema change beyond reusing `deliverability_events.idempotency_key`.
- **Effort:** L  **Impact:** high
- **Code anchors:** `internal/app/worker/health_record.go:25`; `internal/app/worker/event_send_email.go:92`; `internal/app/consumer/event_email_error.go:156`; `internal/app/advanced/service.go:967,1063,1127`

### 3.3 Campaign-body content lint as pre-send + editor check (extend `warmlint`)

- **Problem:** `warmlint.Check` runs ONLY on warmup content (`internal/tasks/content_lint.go:9-11`). Campaign subject/body are rendered and sent (`internal/tasks/campaign_task.go:421-423`) with no spam-trigger scoring, no link-count cap, no ALL-CAPS/image-ratio check. Cold campaigns can ship classic spam copy with zero feedback. Plain-text + <=2 links + no first-email attachments are soft heuristics (Mailforge/Folderly, 2025-2026); 3+ trigger words raise spam likelihood 67%.
- **Proposal:** Reuse `warmlint` to score campaign bodies. Surface a non-blocking score in the editor at save time; soft-block at send for egregious cases (>2 links, ALL-CAPS subject, first-step attachment).
- **Implementation:** Generalize `internal/pkg/warmlint/lint.go` into a scoring function returning findings + severity (it already detects ALL-CAPS, stacked punctuation, spam triggers, fake Re:/Fwd:). Add link-count and image/attachment heuristics. Call it (a) in the sequence create/update handler so the editor shows findings (additive response field), and (b) at `campaign_task.go` after `expandSpintax` (line 423) as a soft gate logging a `content_warning` campaign log and, for hard cases, pausing via the existing breaker path. Keep advisory by default (a setting like the existing Preflight checks in `internal/models/advanced_outreach.go:55`). Docs: `guides/` campaign page.
- **Effort:** M  **Impact:** high
- **Code anchors:** `internal/pkg/warmlint/lint.go`; `internal/tasks/content_lint.go:9`; `internal/tasks/campaign_task.go:421`; `internal/models/advanced_outreach.go:55`

### 3.4 Graded reputation-aware throttling (slow a still-'healthy' mailbox before a hard band)

- **Problem:** The only health input to cold volume is the warmup state, binary at the cold layer: quarantined/blocked skip, throttled halves budget (`internal/scheduler/campaign_scheduler.go:276-288`). A degrading mailbox keeps sending at full cap until it crosses quarantine/block, contradicting the watch/throttled intent in CLAUDE.md and Postmark/SES's "reduce 25-30% when metrics degrade" (current docs).
- **Proposal:** Apply the existing `watch` band (and a proportional reduction) to cold send volume, not just warmup. `adjustmentFor` already damps warmup per band (watch 0.7x, throttled 0.5x at `internal/scheduler/warmup_scheduler.go:30-43`); mirror that graded multiplier into the cold scheduler so a watch-state mailbox sends ~70% of cap before it ever quarantines.
- **Implementation:** In `internal/scheduler/campaign_scheduler.go` around the health switch (`:276-288`), add a `models.WarmupHealthWatch` case that multiplies `remaining` by 0.7. Extract the `adjustmentFor` multipliers into a shared helper so the two schedulers can't drift. No new columns: `GetHealthState` already returns the band and is already called here. Because warmup health now gets real bounce/complaint signal from 3.2, the watch band actually fires for cold-only mailboxes.
- **Effort:** S  **Impact:** high
- **Code anchors:** `internal/scheduler/campaign_scheduler.go:276`; `internal/scheduler/warmup_scheduler.go:25-43`; `internal/repository/pg_warmup.go:200`

### 3.5 Schedule cold sends in the RECIPIENT's timezone and widen jitter

- **Problem:** `OptimizeSendTime` (`internal/app/advanced/service.go:1191`) is opt-in, runs AFTER the send as a next-tick hint, and derives recipient TZ only from `contact.CustomFields['timezone']` with no inference. The hardcoded 8-20 window (`internal/scheduler/campaign_scheduler.go:291`) is the SENDING account's TZ. Best data favors Tue-Thu 8-10am in the recipient's local time (~20% engagement lift; soft heuristics, Puzzle/Smartlead, 2025-2026).
- **Proposal:** Move recipient-timezone optimization INTO the scheduling decision (before the slot is chosen) with a domain/region-based TZ inference fallback when `custom_fields['timezone']` is absent. Widen the configurable jitter.
- **Implementation:** Call `OptimizeSendTime` (or fold its hour-snapping) inside `CalculateNextCampaignTime` where the candidate slot is computed, not only in `campaign_task.go:699` after the send. Add a cheap recipient-TZ inference helper (ccTLD/known-provider region) used when `CustomFields['timezone']` is empty; best-effort UTC fallback. Make the jitter range (currently +/-20min, `campaign_scheduler.go:447`) a per-campaign/org setting toward the research's 60-120min spread. No worker change. Docs: `guides/` scheduling page.
- **Effort:** M  **Impact:** medium
- **Code anchors:** `internal/app/advanced/service.go:1191`; `internal/tasks/campaign_task.go:699`; `internal/scheduler/campaign_scheduler.go:291,447`

### 3.6 Pre-launch list-verification + projected-bounce gate

- **Problem:** Per-recipient verification gating exists at send time (`internal/tasks/campaign_task.go:296-328`), but there is no gate that checks list quality BEFORE a campaign launches. No `VerifyList`/projected-bounce/catch-all-share check; a scraped list reveals itself only after sends damage reputation. Keep hard-bounce <2% (soft heuristic, Litemail/Verified.email, 2025-2026); SES reviews at 5%, pauses at 10% (AWS docs, current).
- **Proposal:** Run a bulk verification pass over a campaign's recipient set at launch, compute projected hard-bounce / catch-all / disposable share, and refuse to start (or warn) when it exceeds a threshold, mirroring the per-mailbox send-time gate at the list level.
- **Implementation:** Add `BulkVerifyForCampaign` to `internal/app/emailverify/service.go` batching `GetUnverifiedContacts` (reuse `internal/repository/pg_contact.go:446`) and persisting `verification_status`. In the campaign activate handler, before flipping status to `active`, compute projected bounce = invalid share, block at >5% / warn at 2-3% (matching the breaker thresholds), returning an additive `errx` code (update `api/error-codes.mdx`). Catch-all/risky share already exists via `contact.is_catch_all` / `VerificationStatus='risky'`, so this is mostly orchestration. Keep SMTP probing in the existing pluggable provider, never the worker. Surface progress via an `auditOrg` event.
- **Effort:** M  **Impact:** high
- **Code anchors:** `internal/app/emailverify/service.go:51`; `internal/repository/pg_contact.go:412,446`; `internal/tasks/campaign_task.go:296`; `internal/app/advanced/service.go:1116`

### 3.7 Cold mailbox/domain lifecycle (active/resting/warming)

- **Problem:** Cold mailboxes send until they hit a warmup-health hard band; there is no graceful "rest" state pulling a fatiguing domain into warmup-only traffic to recover, and no inbox-rotation lifecycle. Domain reputation decays with sustained cold sending (92%->63% over 10 months) and resting recovers it (soft heuristic, Maildeck/Mailpool, 2025-2026). The `risk_band` model exists but only routes IP pools, not send-eligibility lifecycle.
- **Proposal:** Add a control-plane lifecycle state the campaign sender-resolution step honors: `resting` mailboxes are excluded from the active cold pool but keep warmup at a low maintenance volume, with transitions driven by the same rolling signals (now fed by 3.2).
- **Implementation:** Add `email_accounts.send_lifecycle text CHECK IN ('warming','active','resting','reserve') DEFAULT 'active'` in the 3.1 migration. In `campaign_scheduler.go` STEP 2 sender resolution (`:54-60`), filter out `lifecycle != 'active'`. Drive transitions in the existing hourly `risk_rebalancer.go` (consumer): on watch/throttled OR `auth_state='failing'`, set `resting` and let `warmup_scheduler` keep it at the base floor it never zeroes. Promote back to `active` on a clean probation sample, reusing the `CanParticipate` pattern. Reuses risk_band/health plumbing and the audit spine. Docs: `guides/` mailboxes/warmup pages.
- **Effort:** L  **Impact:** medium
- **Code anchors:** `internal/app/consumer/risk_rebalancer.go:54`; `internal/scheduler/campaign_scheduler.go:54`; `internal/scheduler/warmup_scheduler.go:175`; `internal/app/warmup/service.go:234`; `internal/models/risk.go:9`

---

## Cross-cutting leverage

Several recommendations are the same primitive serving multiple dimensions. Build these once.

- **One authentication gate, three payoffs.** 1.1 and 3.1 are the same persisted `dnsauth` state + send/pool gate; it is also a fraud signal (2.x feeds `auth_state='failing'` into `risk_score` and snowshoe detection). Build the migration + daily sweep + `CanParticipate`/`campaign_scheduler` gate once. This is the single highest-leverage change in the report. **Note: CLAUDE.md requires auth health for warmup re-entry and treats it as a bulk-sender contract requirement, but no code enforces it today.**
- **One banded health/risk state-machine pattern, three objects.** The proven `evaluateMetrics` shape (`internal/app/warmup/service.go:637-787`) already powers the mailbox-warmup axis. Extend the same pattern to (a) a per-org risk object (2.1) and (b) cold-only mailboxes that never joined a warmup pool. **CLAUDE.md's recommended generic per-mailbox `health`/`health_reason`/`last_health_score` column is realized only on the warmup-participant row; a cold-only mailbox is treated as clean by default.** Wiring 3.2 (real bounce/complaint signal) plus a health row for cold-only mailboxes closes that gap.
- **One signal pipeline feeds both warmup volume AND cold throttling.** Spam-placement (1.3, 1.8) and auto-derived bounce/complaint (3.2) should flow into the same health state that warmup ramp damping (1.2) and cold graded throttling (3.4) read. Today they read `GetHealthState` from the same source; the missing piece is populating it from real send results, after which the watch band fires for cold-only mailboxes too.
- **One IP/geo/velocity plumbing, three detectors.** Signup risk (2.2), velocity/correlation (2.3), and ATO (2.5) all reuse the same captured `signup_ip`/ASN/geo. Capture it once at `RegistrationStart`/`LoginConfirm`.
- **The audit spine is the realtime delivery mechanism for every new state.** Org-risk transitions, mailbox auth-state transitions, and lifecycle changes all ride the existing `AUDIT_CREATED` -> spine -> react-query invalidation path. New entity types (`AuditEntityOrgRisk`) need a matching frontend spine entry per CLAUDE.md.

---

## Prioritized roadmap

Ordered by impact-to-effort. Recommendation 1.1/3.1 is one build (authentication gate) counted once.

| # | Recommendation | Dimension | Effort | Impact | Phase |
|---|---|---|---|---|---|
| 1 | Persisted SPF/DKIM/DMARC/PTR pre-send + pool-entry gate (1.1 / 3.1) | Warmup + Cold | L | critical | **Now** |
| 2 | Graded `watch`-band throttle for cold volume (extract shared multipliers) (3.4) | Cold | S | high | **Now** |
| 3 | Auto-derive bounce/complaint from worker SMTP + DSN/FBL (3.2) | Cold | L | high | **Now** |
| 4 | Closed-loop warmup ramp: ~25% throttle-down on early signal (1.2) | Warmup | M | high | **Now** |
| 5 | Per-org risk state machine via the audit spine (2.1) | Abuse | L | critical | **Now** |
| 6 | Signup-time risk gate: disposable email + IP/ASN + metadata (2.2) | Abuse | M | high | **Next** |
| 7 | Provider-aware warmup partner routing (1.3) | Warmup | M | high | **Next** |
| 8 | Campaign-body content lint (extend `warmlint`) (3.3) | Cold | M | high | **Next** |
| 9 | List-level quality gate on import (2.4) | Abuse | M | high | **Next** |
| 10 | Pre-launch list-verification + projected-bounce gate (3.6) | Cold | M | high | **Next** |
| 11 | Warmup-to-cold graduation gate (1.4) | Warmup | M | high | **Next** |
| 12 | Cross-account velocity + soft-link correlation (2.3) | Abuse | L | high | **Next** |
| 13 | ATO signals at login behind step-up auth (2.5) | Abuse | M | high | Later |
| 14 | Small/early-pool volume policy (1.5) | Warmup | S | medium | Later |
| 15 | Fail-closed risk-critical checks + admin evidence (2.7) | Abuse | S | medium | Later |
| 16 | Recipient-timezone send timing + wider jitter (3.5) | Cold | M | medium | Later |
| 17 | Cold mailbox/domain lifecycle (active/resting/warming) (3.7) | Cold | L | medium | Later |
| 18 | Deeper conversation realism (multi-turn) (1.6) | Warmup | L | medium | Later |
| 19 | Risk-gated trial / card-on-trial + payment signals (2.6) | Abuse | M | medium | Later |
| 20 | Naturalize warmup intra-day spacing (1.7) | Warmup | S | medium | Later |
| 21 | Record placement without Junk flag / unassigned recipients (1.8) | Warmup | M | medium | Later |

**Now** (high impact, foundational, unblock the rest): items 1-5. Item 1 unblocks 2.x fraud signals, 3.7 lifecycle, and warmup re-entry; item 3 makes item 2's watch band actually fire for cold-only mailboxes.

**Next** (high impact, depend on the foundations or are independent mid-cost): items 6-12.

**Later** (medium impact or larger build for incremental polish): items 13-21.

### Status on this branch

Progress already landed on `research/warmup-abuse-cold-effectiveness`:

- **Item 1 (authentication) is shipped in observe-only mode.** A background consumer sweep persists each mailbox's SPF/DKIM/DMARC state (`email_accounts.auth_state` and siblings, migration `000051`) and surfaces it on the mailbox list/detail API, with `dnsauth` hardened to treat transient DNS errors as `unknown` rather than `failing`. The hard send/pool gate is deliberately NOT wired yet; flipping it on is a small follow-up against the persisted `auth_state`.
- **Item 2 (watch-band cold throttle) is shipped.** The warmup `watch` multiplier (0.7x) now also dampens cold campaign volume via the shared `adjustmentFor` helper, so a watch-state mailbox slows across both warmup and cold sending before it hard-quarantines.

Remaining items are unstarted.

### Provider-requirement vs heuristic note

Hard provider requirements driving the top of the roadmap (treat as contract, not tuning): SPF+DKIM+DMARC alignment, valid PTR, TLS, and RFC 8058 one-click unsubscribe for 5,000+/day senders (Google sender guidelines effective Feb 1 2024; Yahoo Sender Hub; Microsoft/Outlook `5.7.515` enforcement from May 5 2025). Complaint <0.10% target / never reach 0.30% (Gmail/Yahoo); SES review at 0.10% complaint / 5% bounce, pause at 0.50% / 10% (AWS docs, current). Everything else (volume bands, spacing, content lint thresholds, lifecycle rotation, conversation depth) is a soft heuristic from vendor data (Woodpecker/Smartlead/Postmark/MailReach/Maildeck, 2025-2026); use for direction, not as citable contract.

Verify before shipping: exact `OptimizeSendTime` hour-snapping reuse path, the `email_accounts` migration number sequence, and whether the worker already surfaces enough of the `5.7.515` SMTP response text to classify auth failures versus transient bounces.
