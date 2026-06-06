import React, { useEffect, useRef } from 'react'
import { useSocket } from './context/socket'
import { useAppStore } from '@/stores'
import { useUserProfile } from './context/user'
import { useRealtimeEvents } from './useRealtimeEvents'
import useUnseenCount from '@/lib/api/hooks/app/unibox/useUnseenCount'

export function RealtimeManager({ children }: { children: React.ReactNode }) {
  const { isConnected, reconnectAttempt, joinChannel, leaveChannel } = useSocket()
  const { user } = useUserProfile()
  const currentOrg = useAppStore((s) => s.currentOrganization)
  const setConnectionStatus = useAppStore((s) => s.setConnectionStatus)
  const setReconnectAttempt = useAppStore((s) => s.setReconnectAttempt)
  const setConnectionQuality = useAppStore((s) => s.setConnectionQuality)
  const addJoinedChannel = useAppStore((s) => s.addJoinedChannel)
  const removeJoinedChannel = useAppStore((s) => s.removeJoinedChannel)
  const setUnseenCount = useAppStore((s) => s.setUnseenCount)

  const prevOrgIdRef = useRef<string | null>(null)
  const heartbeatRef = useRef<ReturnType<typeof setInterval> | null>(null)
  const lastHeartbeatRef = useRef<number>(Date.now())

  // Sync connection status to store
  useEffect(() => {
    if (isConnected) {
      setConnectionStatus('connected')
      setConnectionQuality('good')
      setReconnectAttempt(0)
    } else if (reconnectAttempt > 0) {
      setConnectionStatus('connecting')
      setConnectionQuality('poor')
      setReconnectAttempt(reconnectAttempt)
    } else {
      setConnectionStatus('disconnected')
      setConnectionQuality('disconnected')
    }
  }, [isConnected, reconnectAttempt, setConnectionStatus, setConnectionQuality, setReconnectAttempt])

  // Auto-join user channel. Topic uses the user UUID, not the email —
  // the realtime channel handler is `def join("user:" <> user_id, ...)`
  // and compares against `socket.assigns.user_id` which is the JWT
  // `sub` (UUID). Using email here would refuse the join.
  useEffect(() => {
    if (isConnected && user?.id) {
      const topic = `user:${user.id}`
      joinChannel(topic)
      addJoinedChannel(topic)
      return () => {
        leaveChannel(topic)
        removeJoinedChannel(topic)
      }
    }
  }, [isConnected, user?.id, joinChannel, leaveChannel, addJoinedChannel, removeJoinedChannel])

  // Auto-join/leave org channel on org switch
  useEffect(() => {
    if (!isConnected) return

    const prevOrgId = prevOrgIdRef.current
    const newOrgId = currentOrg?.id

    if (prevOrgId && prevOrgId !== newOrgId) {
      const prevTopic = `org:${prevOrgId}`
      leaveChannel(prevTopic)
      removeJoinedChannel(prevTopic)
    }

    if (newOrgId) {
      const topic = `org:${newOrgId}`
      joinChannel(topic)
      addJoinedChannel(topic)
    }

    prevOrgIdRef.current = newOrgId ?? null
  }, [isConnected, currentOrg?.id, joinChannel, leaveChannel, addJoinedChannel, removeJoinedChannel])

  // Connection quality monitoring
  useEffect(() => {
    if (!isConnected) return

    lastHeartbeatRef.current = Date.now()

    heartbeatRef.current = setInterval(() => {
      const elapsed = Date.now() - lastHeartbeatRef.current
      if (elapsed > 15000) {
        setConnectionQuality('poor')
      } else if (elapsed > 5000) {
        setConnectionQuality('degraded')
      } else {
        setConnectionQuality('good')
      }
      lastHeartbeatRef.current = Date.now()
    }, 5000)

    return () => {
      if (heartbeatRef.current) {
        clearInterval(heartbeatRef.current)
      }
    }
  }, [isConnected, setConnectionQuality])

  // Seed the unread inbox count from the server. The store value is otherwise
  // session-only (realtime increments it but it starts at 0), so seeding makes
  // the title + favicon badge reflect the real unread count.
  //
  // This is a react-query (not a one-shot fetch) on purpose: /unibox/count is
  // org-scoped, and on a fresh login the bootstrap fires before OrgGate has
  // re-synced the server session — so the first read returns the wrong/empty
  // count. OrgGate's "sync" switch invalidates org-scoped queries on success
  // (root "unibox" included), which refetches this against the correct session
  // and corrects the badge without a reload. Gated on a selected workspace so a
  // multi-org login (which redirects to /select-org before any sync) doesn't
  // fire a NULL-org read. Best-effort: a failure leaves the current count as-is.
  const unseenQuery = useUnseenCount({ enabled: !!currentOrg })
  useEffect(() => {
    const c = unseenQuery.data?.count
    if (typeof c === 'number') setUnseenCount(c)
  }, [unseenQuery.data, setUnseenCount])

  // Set up event-to-store routing
  useRealtimeEvents()

  return <>{children}</>
}
