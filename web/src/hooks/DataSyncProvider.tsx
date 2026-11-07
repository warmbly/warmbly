import React from 'react'
import { useAppStore } from '@/stores'
import { useSyncToStore } from './useSyncToStore'
import { useUserProfile } from './context/user'
import useEmails from '@/lib/api/hooks/app/emails/useEmails'

export function DataSyncProvider({ children }: { children: React.ReactNode }) {
  const { user, access, timezones } = useUserProfile()

  const setUser = useAppStore((s) => s.setUser)
  const setAccess = useAppStore((s) => s.setAccess)
  const setTimezones = useAppStore((s) => s.setTimezones)
  const setIsLoading = useAppStore((s) => s.setIsLoading)
  const setEmails = useAppStore((s) => s.setEmails)
  const setTags = useAppStore((s) => s.setTags)

  // The whole mailbox directory feeds the store (tag scope, filter sheet, From picker).
  // It is the same cached list the mailboxes page and pickers read, so the app fetches it once.
  const directory = useEmails({ query: '', tag: '' })
  const whole = directory.data && !directory.isLoadingRest ? directory.emails : undefined

  useSyncToStore(user, (data) => {
    setUser(data)
    setIsLoading(false)
  })
  useSyncToStore(access, setAccess)
  useSyncToStore(timezones, setTimezones)
  useSyncToStore(user?.tags, setTags)
  useSyncToStore(whole, setEmails)

  return <>{children}</>
}
