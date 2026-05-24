import React from 'react'
import { useAppStore } from '@/stores'
import { useSyncToStore } from './useSyncToStore'
import { useUserProfile } from './context/user'

export function DataSyncProvider({ children }: { children: React.ReactNode }) {
  const { user, access, timezones } = useUserProfile()

  const setUser = useAppStore((s) => s.setUser)
  const setAccess = useAppStore((s) => s.setAccess)
  const setTimezones = useAppStore((s) => s.setTimezones)
  const setIsLoading = useAppStore((s) => s.setIsLoading)

  useSyncToStore(user, (data) => {
    setUser(data)
    setIsLoading(false)
  })
  useSyncToStore(access, setAccess)
  useSyncToStore(timezones, setTimezones)

  return <>{children}</>
}
