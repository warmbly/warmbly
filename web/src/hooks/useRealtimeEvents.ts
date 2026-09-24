import { useCallback, useEffect, useRef } from 'react'
import toast from 'react-hot-toast'
import { useQueryClient, type QueryKey } from '@tanstack/react-query'
import { useSocket } from './context/socket'
import { useAppStore } from '@/stores'
import { useUserProfile } from './context/user'
import { markSelfMutation } from '@/lib/realtime/selfActivity'
import { announceCampaignDeleted } from '@/lib/realtime/campaignDeleted'
import { MAILBOX_REMOVAL_KEYS } from '@/lib/api/hooks/app/emails/invalidateAfterMailboxRemoval'

// Bridges realtime socket events into both the zustand store and react-query
// cache so list pages, detail panes, counters, and workflow states stay live.
export function useRealtimeEvents() {
  const { isConnected, subscribeToChannel } = useSocket()
  const { user } = useUserProfile()
  const myId = user?.id ?? null
  const queryClient = useQueryClient()
  const currentOrg = useAppStore((s) => s.currentOrganization)

  const updateCampaign = useAppStore((s) => s.updateCampaign)
  const addUniboxEmail = useAppStore((s) => s.addUniboxEmail)
  const updateDeal = useAppStore((s) => s.updateDeal)
  const setSubscription = useAppStore((s) => s.setSubscription)

  const invalidate = useCallback(
    (queryKeys: QueryKey[]) => {
      for (const queryKey of queryKeys) {
        void queryClient.invalidateQueries({ queryKey })
      }
    },
    [queryClient],
  )

  // Warmup deliveries arrive all day across a whole pool, so their refreshes
  // are coalesced: placement views every 15s, and the account statuses (a
  // per-mailbox fan-out whose 7-day rate barely moves) every 5 minutes.
  const placementTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const accountsTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const refreshPlacement = useCallback(() => {
    if (!placementTimer.current) {
      placementTimer.current = setTimeout(() => {
        placementTimer.current = null
        invalidate([['analytics', 'warmup', 'placement']])
      }, 15_000)
    }
    if (!accountsTimer.current) {
      accountsTimer.current = setTimeout(() => {
        accountsTimer.current = null
        invalidate([['analytics', 'accounts']])
      }, 300_000)
    }
  }, [invalidate])
  useEffect(() => () => {
    if (placementTimer.current) clearTimeout(placementTimer.current)
    if (accountsTimer.current) clearTimeout(accountsTimer.current)
  }, [])

  const handleRealtimeEvent = useCallback(
    (payload: Record<string, unknown>) => {
      const rawEvent = String(
        payload.event_type ?? payload.type ?? payload._event ?? '',
      )
      const event = rawEvent.replace(/[.:\s-]+/g, '_').toUpperCase()
      if (!event) return

      // Presence sync + throttle notices are handled by PresenceProvider /
      // the socket layer; they must never reach the default invalidation
      // (presence diffs fire on every teammate navigation).
      if (event.startsWith('PRESENCE') || event === 'RATE_LIMITED') return

      // Ephemeral live-collaboration frames (cursor moves, card drags) are
      // consumed by the canvas's own subscription; they carry no durable state
      // and must never trigger a react-query refetch.
      if (event.startsWith('LIVE_')) return

      const getString = (key: string) => {
        const value = payload[key]
        return typeof value === 'string' && value.length > 0 ? value : null
      }
      const includes = (...needles: string[]) =>
        needles.some((needle) => event.includes(needle))

      const campaignId = getString('campaign_id')
      const contactId = getString('contact_id')
      const dealId = getString('deal_id')
      const threadId = getString('thread_id')
      const emailId = getString('email_id') ?? getString('message_id')

      // A mailbox import moved: its job, the import list and the mailbox list
      // all refresh. Checked before the ACCOUNT/EMAIL branches below.
      if (event === 'MAILBOX_IMPORT_PROGRESS') {
        const importId = getString('import_id')
        // A finished row can move a mailbox off Google sign-in.
        invalidate([['emails', 'imports'], ['emails', 'list'], ['sending-domains'], ['mailbox-grants', 'migration'], ['pool-link']])
        if (importId) invalidate([['emails', 'imports', importId]])
        return
      }

      // An AI-suggested unibox reply was drafted and is awaiting human review.
      // Refresh the unibox (badge/overview) + the drafts list, and the specific
      // thread if present.
      if (includes('AI_DRAFT')) {
        invalidate([
          ['unibox'],
          ['unibox', 'overview'],
          ['unibox', 'agent-drafts'],
        ])
        if (threadId) invalidate([['unibox', 'thread', threadId]])
        return
      }

      // Inbox events ride the user channel as well as the org one, so the
      // owner gets each twice and a member of two workspaces gets the other
      // one's mail too. The unread badge is refetched rather than counted up
      // here: the server knows whether the message is unread, in the Inbox
      // and in this workspace, and an increment knew none of that.
      const inboxEvent = includes(
        'EMAIL_RECEIVED',
        'NEW_EMAIL',
        'INBOX_NEW',
        'EMAIL_UPDATED',
        'EMAIL_DELETED',
        'INBOX_UPDATE',
      )
      const eventOrg = getString('org_id')
      if (inboxEvent && eventOrg && currentOrg?.id && eventOrg !== currentOrg.id) return

      if (includes('EMAIL_RECEIVED', 'NEW_EMAIL', 'INBOX_NEW')) {
        addUniboxEmail(payload as any)
        invalidate([
          ['unibox'],
          ['analytics'],
          ['emails', 'list'],
        ])
        if (threadId) invalidate([['unibox', 'thread', threadId]])
        if (emailId) invalidate([['unibox', 'email', emailId]])
        return
      }

      // A message read or removed takes its reply notification with it
      // (server side), so the bell refreshes along with the inbox.
      if (includes('EMAIL_UPDATED', 'EMAIL_DELETED', 'INBOX_UPDATE')) {
        invalidate([['unibox'], ['analytics'], ['inbox-tagging'], ['notifications', 'feed']])
        if (threadId) invalidate([['unibox', 'thread', threadId]])
        if (emailId) invalidate([['unibox', 'email', emailId]])
        return
      }

      // AI credit balance dipped below the org's alert threshold (fired at
      // most once per day, gated on manage_billing server-side). Popup
      // reminder + refresh every credits view.
      if (includes('CREDITS_LOW')) {
        const balance = typeof payload.balance === 'number' ? payload.balance : null
        toast(
          balance !== null
            ? `AI credits are running low: ${balance} left. Top up or enable auto top-up in Billing.`
            : 'AI credits are running low. Top up or enable auto top-up in Billing.',
          { icon: '⚠️', duration: 8000, id: 'credits-low' },
        )
        invalidate([['subscription', 'credits']])
        return
      }

      // Any fresh AI credit debit (fired per charge, including scheduled
      // campaign/automation spends) — keep every credits view live so the
      // meter visibly moves when credits are used.
      if (includes('CREDITS_CHANGED')) {
        invalidate([['subscription', 'credits']])
        return
      }

      // A hosted form received a public submission: refresh form counters,
      // submission lists, and contacts (the submission may have created one).
      if (includes('FORM_SUBMISSION')) {
        invalidate([['forms'], ['contacts']])
        return
      }

      // AI contact research finished for one contact (batch progress arrives as
      // one of these per completion).
      if (includes('AI_RESEARCH', 'RESEARCH_PROGRESS')) {
        invalidate([['contacts']])
        if (contactId)
          invalidate([
            ['contacts', contactId],
            ['contacts', contactId, 'research'],
          ])
        return
      }

      if (includes('CONTACT')) {
        invalidate([
          ['contacts'],
          ['campaigns', 'list'],
          ['analytics'],
          ['organizations', 'limits'],
        ])
        if (contactId) invalidate([['contacts', contactId]])
        return
      }

      // A deleted campaign: its detail caches are dropped, not refetched (a
      // refetch only 404s). The deleter's own page has already left; a
      // teammate's open detail page is sent back by the announcement.
      if (event === 'CAMPAIGN_DELETED' && campaignId) {
        if (getString('user_id') !== myId) {
          announceCampaignDeleted({ id: campaignId, name: getString('name') ?? '' })
        }
        queryClient.removeQueries({ queryKey: ['campaigns', campaignId] })
        queryClient.removeQueries({ queryKey: ['analytics', 'campaigns', campaignId] })
        invalidate([['campaigns', 'list'], ['analytics'], ['contacts']])
        return
      }

      // A website page view for an identified contact: only that contact's
      // timeline moves.
      if (event === 'PAGE_HIT') {
        if (contactId) invalidate([['contacts', contactId, 'timeline']])
        return
      }

      if (event === 'DIRECT_EMAIL_OPENED' || event === 'DIRECT_EMAIL_CLICKED') {
        invalidate([['analytics'], ['analytics', 'direct']])
        return
      }

      if (
        includes(
          'CAMPAIGN',
          'EMAIL_SENT',
          'EMAIL_OPENED',
          'EMAIL_CLICKED',
          'EMAIL_REPLIED',
          'EMAIL_BOUNCED',
          'TASK_PROGRESS',
        )
      ) {
        if (campaignId) {
          const status = getString('status')
          updateCampaign(campaignId, status ? { status } : {})
          invalidate([
            ['campaigns', campaignId],
            ['campaigns', campaignId, 'logs'],
            ['analytics', 'campaigns', campaignId],
            ['analytics', 'campaigns', campaignId, 'daily'],
            ['analytics', 'campaigns', campaignId, 'hourly'],
          ])
        }
        invalidate([
          ['campaigns', 'list'],
          ['analytics'],
          ['contacts'],
        ])
        if (contactId) invalidate([['contacts', contactId]])
        return
      }

      // A user-addressed mailbox/send error (a compose or reply the worker
      // could not send, a mailbox that needs re-authorizing). Nothing else
      // would tell the user, so toast it, then refresh the mailbox views.
      if (event === 'ERROR') {
        const data = payload.data as Record<string, unknown> | undefined
        const title =
          typeof data?.title === 'string' && data.title
            ? data.title
            : (getString('message') ?? 'Something went wrong')
        const detail = typeof data?.message === 'string' ? data.message : ''
        toast.error(detail ? `${title}: ${detail}` : title, {
          id: `email-error-${getString('task_id') ?? getString('email_id') ?? 'general'}`,
          duration: 8000,
        })
        invalidate([['emails', 'list'], ['unibox']])
        return
      }

      if (event === 'WARMUP_PLACEMENT') {
        refreshPlacement()
        return
      }

      if (includes('ACCOUNT', 'EMAIL_STATUS', 'EMAIL_ERROR', 'WARMUP')) {
        // ACCOUNT_SYNC_STATE: the mailbox's import finished or fair use
        // started/stopped holding it; the drawer's sync card refetches.
        const accountId = getString('email_account_id')
        if (accountId) invalidate([['emails', accountId, 'sync']])
        invalidate([
          ['emails', 'list'],
          ['analytics', 'accounts'],
          ['analytics', 'warmup'],
          ['analytics', 'dashboard'],
        ])
        return
      }

      if (includes('DEAL')) {
        if (dealId) updateDeal(dealId, payload as any)
        invalidate([['crm', 'deals'], ['crm', 'pipelines'], ['contacts']])
        return
      }

      if (includes('PIPELINE', 'STAGE')) {
        invalidate([['crm', 'pipelines'], ['crm', 'deals']])
        return
      }

      if (includes('CRM_TASK', 'TASK')) {
        invalidate([['crm', 'tasks'], ['crm', 'deals']])
        return
      }

      if (includes('SUBSCRIPTION', 'PLAN', 'BILLING', 'LIMIT')) {
        setSubscription(payload as any)
        invalidate([
          ['subscription'],
          ['organizations', 'current'],
          ['organizations', 'limits'],
          ['auth', 'me'],
        ])
        return
      }

      if (includes('MEMBER', 'INVITATION', 'ORGANIZATION', 'SETTINGS')) {
        invalidate([
          ['organizations'],
          ['organizations', 'current'],
          ['organizations', 'invitations'],
          ['auth', 'me'],
        ])
        return
      }

      if (includes('API_KEY')) {
        invalidate([['api-keys']])
        return
      }

      // A meeting was booked / rescheduled / canceled (Calendly / Cal.com):
      // refresh the Meetings page list + summary, the integrations bookings
      // list, and the originating contact's timeline so the call appears live.
      if (includes('MEETING', 'BOOKING')) {
        invalidate([
          ['meetings'],
          ['meetings', 'summary'],
          ['integrations', 'bookings'],
        ])
        if (contactId) invalidate([['contacts', contactId, 'timeline']])
        return
      }

      // A new in-app notification for this user: refresh the bell feed live
      // (invalidate-only — the badge updates without a toast, to avoid burst spam).
      if (includes('NOTIFICATION')) {
        invalidate([['notifications', 'feed']])
        return
      }

      // An automation was created/updated/deleted or fired: refresh the list and,
      // for a specific automation, its detail + run history (live in the builder).
      if (includes('AUTOMATION')) {
        invalidate([['automations']])
        const automationId = getString('automation_id')
        if (automationId) invalidate([['automations', automationId], ['automations', automationId, 'runs']])
        return
      }

      if (includes('INTEGRATION', 'CONNECTION')) {
        invalidate([
          ['integrations', 'connections'],
          ['integrations', 'catalog'],
          ['integrations', 'bookings'],
        ])
        const connectionId = getString('connection_id')
        if (connectionId) invalidate([['integrations', 'connection', connectionId]])
        return
      }

      if (includes('TEMPLATE')) {
        invalidate([['templates']])
        return
      }

      // A webhook endpoint changed or a delivery was attempted/redelivered:
      // refresh the endpoints list and the live delivery log.
      if (includes('WEBHOOK')) {
        invalidate([['webhooks', 'list'], ['webhooks', 'deliveries']])
        return
      }

      // Audit spine: every audited mutation broadcasts AUDIT_CREATED with its
      // action/entity_type/entity_id org-wide, so one branch keeps every
      // teammate's lists fresh for surfaces that have no dedicated event.
      if (includes('AUDIT')) {
        invalidate([['audit']])
        const entityType = getString('entity_type') ?? ''
        const entityId = getString('entity_id')
        // When this mutation's actor is us, record it so collaborative editors
        // don't flag the user's own change (made on a list row, another tab, or
        // another device that round-trips back) as a teammate's edit.
        if (entityId && getString('user_id') === myId) {
          markSelfMutation(entityType, entityId)
        }
        const spine: Record<string, QueryKey[]> = {
          // Segment membership is computed from contact data, so a contact
          // change moves segment counts too.
          contact: [['contacts'], ['segments']],
          // A suppression entry changes who a campaign can reach, so the
          // list, the contact drawers and the audience counts all move.
          suppression: [['suppressions'], ['contacts'], ['analytics']],
          segment: [['segments'], ['contacts', 'list']],
          form: [['forms']],
          campaign: [['campaigns'], ['analytics']],
          // One lead paused or resumed inside one campaign: the Leads list and
          // its scope-chip counts move, and so does the contact drawer.
          campaign_lead: [['contacts'], ['campaigns']],
          step: [['campaigns']],
          // ['emails'] rather than ['emails', 'list']: it prefix-matches the
          // per-mailbox detail reads too (['emails', id, 'behavior'] and its
          // rolled plan), so a teammate retuning a mailbox's sending behaviour
          // refreshes everyone's open drawer instead of only the list row.
          // A mailbox write can move it off Google sign-in, so the migration list follows.
          email_account: [['emails'], ['analytics', 'accounts'], ['sending-domains'], ['mailbox-grants', 'migration'], ['pool-link']],
          // A mailbox import created, retried or cancelled by a teammate; it may move mailboxes onto a grant.
          mailbox_import: [['emails', 'imports'], ['mailbox-grants', 'migration']],
          // An admin grant added, re-checked or removed, and an inbox vendor
          // account connected, re-keyed or removed.
          mailbox_grant: [['mailbox-grants'], ['emails', 'list']],
          mailbox_vendor: [['mailbox-vendors'], ['sending-domains']],
          // A sending domain's root redirect or vendor forwarding changed. Its
          // tracking host is an email_account write, covered above.
          domain_redirect: [['sending-domains']],
          api_key: [['api-keys']],
          webhook: [['webhooks'], ['integrations', 'connections']],
          template: [['templates']],
          // The workspace image library the composer picks body images from.
          email_image: [['email-images']],
          organization: [['organizations']],
          // A risk transition changes send caps and warmup pool placement, so
          // the mailbox and analytics views move with it, not just the banner.
          org_risk: [['organizations'], ['emails'], ['analytics']],
          organization_member: [['organizations'], ['organizations', 'members']],
          invitation: [['organizations', 'invitations']],
          team: [['teams']],
          role: [['organizations']],
          automation: [['automations']],
          integration: [['integrations', 'connections']],
          lead_sync_source: [['lead-sync', 'sources']],
          meeting: [['meetings'], ['meetings', 'summary']],
          subscription: [['subscription'], ['organizations', 'limits']],
          referral: [['subscription', 'referral']],
          referral_credit: [['subscription', 'referral'], ['subscription']],
          // AI credits: a top-up purchase or a monthly allowance reset both
          // change the billing/credits view for every teammate.
          credit_purchase: [['subscription', 'credits'], ['subscription']],
          credit_grant: [['subscription', 'credits'], ['subscription']],
          // AI assistant sessions (per-user; refreshes the session list).
          ai_session: [['ai', 'sessions']],
          // AI skills (org playbooks).
          ai_skill: [['ai', 'skills']],
          // Connected MCP servers (external tools).
          mcp_server: [['ai', 'connections']],
          // Advisor: a background evaluation that opened or resolved findings,
          // or a teammate applying/snoozing/dismissing one. Refreshes every
          // strip and every nav badge at once.
          advisor_finding: [['advisor']],
          // Org settings, including the autosaving website tracking page, so
          // a teammate's edit lands live.
          settings: [['organizations', 'current'], ['website-tracking']],
          // Workspace archives: an export starting or an import landing changes
          // both lists on the settings Data page.
          org_archive: [
            ['organizations', 'exports'],
            ['organizations', 'imports'],
          ],
          unibox: [['unibox']],
          crm_note: [['crm'], ['contacts']],
          crm_pipeline: [['crm', 'pipelines'], ['crm', 'deals']],
          crm_stage: [['crm', 'pipelines'], ['crm', 'deals']],
          crm_deal: [['crm', 'deals'], ['contacts']],
          crm_task: [['crm', 'tasks'], ['crm', 'deals']],
          warmup_routing_rule: [['analytics', 'warmup']],
          cloud_link: [['cloud-link'], ['emails']],
          pool_link: [['pool-link'], ['emails']],
          // Folders / tags / categories ride the user payload.
          folder: [['auth', 'me']],
          tag: [['auth', 'me']],
          category: [['auth', 'me'], ['contacts']],
        }
        const keys = spine[entityType]
        if (keys) invalidate(keys)
        // A deletion also takes the entity's inbox mail, unread badge and
        // advice with it, which no update ever does.
        if (getString('action') === 'delete') {
          if (entityType === 'email_account') invalidate([...MAILBOX_REMOVAL_KEYS])
          if (entityType === 'campaign' || entityType === 'step') invalidate([['advisor']])
        }
        if (entityId && entityType === 'contact') invalidate([['contacts', entityId]])
        if (entityId && entityType === 'segment') invalidate([['segments', entityId]])
        if (entityId && entityType === 'form') invalidate([['forms', entityId]])
        if (entityId && entityType === 'campaign') {
          invalidate([['campaigns', entityId], ['segments', 'campaign', entityId]])
          // A campaign write can enrol leads (linking a segment), so only the
          // contact lists scoped to that campaign move, not every list.
          void queryClient.invalidateQueries({
            predicate: (q) => {
              const [root, kind, options] = q.queryKey as [unknown, unknown, { campaign_ids?: string[] } | undefined]
              return root === 'contacts' && kind === 'list' && !!options?.campaign_ids?.includes(entityId)
            },
          })
        }
        if (entityId && entityType === 'automation') invalidate([['automations', entityId]])
        return
      }

      if (includes('DANGER', 'DELETION')) {
        invalidate([
          ['dangerzone'],
          ['auth', 'me'],
          ['organizations', 'current'],
        ])
        return
      }

      invalidate([
        ['analytics', 'dashboard'],
        ['auth', 'me'],
      ])
    },
    [
      addUniboxEmail,
      currentOrg?.id,
      invalidate,
      myId,
      queryClient,
      refreshPlacement,
      setSubscription,
      updateCampaign,
      updateDeal,
    ],
  )

  useEffect(() => {
    if (!isConnected || !user?.id) return

    const topic = `user:${user.id}`
    return subscribeToChannel(topic, '*', handleRealtimeEvent)
  }, [isConnected, user?.id, subscribeToChannel, handleRealtimeEvent])

  useEffect(() => {
    if (!isConnected || !currentOrg?.id) return

    const topic = `org:${currentOrg.id}`
    return subscribeToChannel(topic, '*', handleRealtimeEvent)
  }, [isConnected, currentOrg?.id, subscribeToChannel, handleRealtimeEvent])
}
