import { useCallback, useEffect } from 'react'
import { useQueryClient, type QueryKey } from '@tanstack/react-query'
import { useSocket } from './context/socket'
import { useAppStore } from '@/stores'
import { useUserProfile } from './context/user'
import { markSelfMutation } from '@/lib/realtime/selfActivity'

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
  const incrementUnseenCount = useAppStore((s) => s.incrementUnseenCount)
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

      if (includes('EMAIL_RECEIVED', 'NEW_EMAIL', 'INBOX_NEW')) {
        addUniboxEmail(payload as any)
        incrementUnseenCount()
        invalidate([
          ['unibox'],
          ['analytics'],
          ['emails', 'list'],
        ])
        if (threadId) invalidate([['unibox', 'thread', threadId]])
        if (emailId) invalidate([['unibox', 'email', emailId]])
        return
      }

      if (includes('EMAIL_UPDATED', 'EMAIL_DELETED', 'INBOX_UPDATE')) {
        invalidate([['unibox'], ['analytics']])
        if (threadId) invalidate([['unibox', 'thread', threadId]])
        if (emailId) invalidate([['unibox', 'email', emailId]])
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

      if (includes('ACCOUNT', 'EMAIL_STATUS', 'EMAIL_ERROR', 'WARMUP')) {
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
          contact: [['contacts']],
          campaign: [['campaigns'], ['analytics']],
          step: [['campaigns']],
          email_account: [['emails', 'list'], ['analytics', 'accounts']],
          api_key: [['api-keys']],
          webhook: [['webhooks'], ['integrations', 'connections']],
          template: [['templates']],
          organization: [['organizations']],
          organization_member: [['organizations'], ['organizations', 'members']],
          invitation: [['organizations', 'invitations']],
          team: [['teams']],
          role: [['organizations']],
          automation: [['automations']],
          integration: [['integrations', 'connections']],
          lead_sync_source: [['lead-sync', 'sources']],
          meeting: [['meetings'], ['meetings', 'summary']],
          subscription: [['subscription'], ['organizations', 'limits']],
          settings: [['organizations', 'current']],
          unibox: [['unibox']],
          crm_note: [['crm'], ['contacts']],
          crm_pipeline: [['crm', 'pipelines'], ['crm', 'deals']],
          crm_stage: [['crm', 'pipelines'], ['crm', 'deals']],
          crm_deal: [['crm', 'deals'], ['contacts']],
          crm_task: [['crm', 'tasks'], ['crm', 'deals']],
          warmup_routing_rule: [['analytics', 'warmup']],
          // Folders / tags / categories ride the user payload.
          folder: [['auth', 'me']],
          tag: [['auth', 'me']],
          category: [['auth', 'me'], ['contacts']],
        }
        const keys = spine[entityType]
        if (keys) invalidate(keys)
        if (entityId && entityType === 'contact') invalidate([['contacts', entityId]])
        if (entityId && entityType === 'campaign') invalidate([['campaigns', entityId]])
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
      incrementUnseenCount,
      invalidate,
      myId,
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
