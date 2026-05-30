import { useCallback, useEffect } from 'react'
import { useQueryClient, type QueryKey } from '@tanstack/react-query'
import { useSocket } from './context/socket'
import { useAppStore } from '@/stores'
import { useUserProfile } from './context/user'

// Bridges realtime socket events into both the zustand store and react-query
// cache so list pages, detail panes, counters, and workflow states stay live.
export function useRealtimeEvents() {
  const { isConnected, subscribeToChannel } = useSocket()
  const { user } = useUserProfile()
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

      if (includes('TEMPLATE')) {
        invalidate([['templates']])
        return
      }

      if (includes('AUDIT')) {
        invalidate([['audit']])
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
