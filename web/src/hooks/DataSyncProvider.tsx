import React from 'react'
import { useQuery } from '@tanstack/react-query'
import { useAppStore } from '@/stores'
import { useSyncToStore } from './useSyncToStore'
import { useUserProfile } from './context/user'
import getEmails from '@/lib/api/client/app/emails/getEmails'
import type Inbox from '@/lib/api/models/app/emails/Inbox'

// Full mailbox directory for the store's emails slice. Several surfaces
// resolve mailbox→tag membership from it (the unibox tag scope, the
// filter sheet, the compose From picker), so it has to hold every
// mailbox, not one page. The key sits under ["emails", "list"] so the
// audit spine's account invalidations keep it fresh in realtime.
async function fetchAllMailboxes(): Promise<Inbox[]> {
  const all: Inbox[] = []
  let cursor: string | null = null
  // 20 pages × 200 is a sanity cap, not a real limit.
  for (let i = 0; i < 20; i++) {
    const page = await getEmails('', cursor, null, 200)
    all.push(...(page.data ?? []))
    if (!page.pagination.has_more || !page.pagination.next_cursor) break
    cursor = page.pagination.next_cursor
  }
  return all
}

export function DataSyncProvider({ children }: { children: React.ReactNode }) {
  const { user, access, timezones } = useUserProfile()

  const setUser = useAppStore((s) => s.setUser)
  const setAccess = useAppStore((s) => s.setAccess)
  const setTimezones = useAppStore((s) => s.setTimezones)
  const setIsLoading = useAppStore((s) => s.setIsLoading)
  const setEmails = useAppStore((s) => s.setEmails)
  const setTags = useAppStore((s) => s.setTags)

  const directory = useQuery({
    queryKey: ['emails', 'list', 'directory'],
    queryFn: fetchAllMailboxes,
    staleTime: 60_000,
    gcTime: 30 * 60 * 1000,
  })

  useSyncToStore(user, (data) => {
    setUser(data)
    setIsLoading(false)
  })
  useSyncToStore(access, setAccess)
  useSyncToStore(timezones, setTimezones)
  useSyncToStore(user?.tags, setTags)
  useSyncToStore(directory.data, setEmails)

  return <>{children}</>
}
