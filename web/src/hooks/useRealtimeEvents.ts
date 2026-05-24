import { useEffect } from 'react'
import { useSocket } from './context/socket'
import { useAppStore } from '@/stores'
import { useUserProfile } from './context/user'

export function useRealtimeEvents() {
  const { isConnected, subscribeToChannel } = useSocket()
  const { user } = useUserProfile()
  const currentOrg = useAppStore((s) => s.currentOrganization)

  const updateCampaign = useAppStore((s) => s.updateCampaign)
  const addUniboxEmail = useAppStore((s) => s.addUniboxEmail)
  const incrementUnseenCount = useAppStore((s) => s.incrementUnseenCount)
  const updateDeal = useAppStore((s) => s.updateDeal)
  const setSubscription = useAppStore((s) => s.setSubscription)

  // User channel events
  useEffect(() => {
    if (!isConnected || !user?.email) return

    const topic = `user:${user.email}`
    const unsubs: (() => void)[] = []

    // New email received
    unsubs.push(
      subscribeToChannel(topic, 'new_email', (payload) => {
        addUniboxEmail(payload as any)
        incrementUnseenCount()
      })
    )

    // Campaign status changed
    unsubs.push(
      subscribeToChannel(topic, 'campaign_status_changed', (payload) => {
        const { campaign_id, ...updates } = payload as any
        if (campaign_id) {
          updateCampaign(campaign_id, updates)
        }
      })
    )

    // Subscription changed
    unsubs.push(
      subscribeToChannel(topic, 'subscription_changed', (payload) => {
        setSubscription(payload as any)
      })
    )

    return () => unsubs.forEach((fn) => fn())
  }, [isConnected, user?.email, subscribeToChannel, addUniboxEmail, incrementUnseenCount, updateCampaign, setSubscription])

  // Org channel events
  useEffect(() => {
    if (!isConnected || !currentOrg?.id) return

    const topic = `org:${currentOrg.id}`
    const unsubs: (() => void)[] = []

    // Deal updated
    unsubs.push(
      subscribeToChannel(topic, 'deal_updated', (payload) => {
        const { deal_id, ...updates } = payload as any
        if (deal_id) {
          updateDeal(deal_id, updates)
        }
      })
    )

    // Campaign events
    unsubs.push(
      subscribeToChannel(topic, 'email_sent', (payload) => {
        const { campaign_id } = payload as any
        if (campaign_id) {
          updateCampaign(campaign_id, {})
        }
      })
    )

    return () => unsubs.forEach((fn) => fn())
  }, [isConnected, currentOrg?.id, subscribeToChannel, updateDeal, updateCampaign])
}
