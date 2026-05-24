import type { StateCreator } from 'zustand'
import type User from '@/lib/api/models/auth/User'
import type Access from '@/lib/api/models/app/admin/Access'
import type Timezone from '@/lib/api/models/app/Timezone'

export interface UserSlice {
  user: User | null
  access: Access | null
  timezones: Timezone[]
  isAuthenticated: boolean
  isLoading: boolean

  setUser: (user: User | null) => void
  setAccess: (access: Access | null) => void
  setTimezones: (timezones: Timezone[]) => void
  setIsLoading: (loading: boolean) => void
  logout: () => void
}

function sameStringArray(a: string[] = [], b: string[] = []): boolean {
  return a.length === b.length && a.every((v, i) => v === b[i])
}

function sameUser(a: User | null, b: User | null): boolean {
  if (a === b) return true
  if (!a || !b) return false

  return (
    a.email === b.email &&
    a.first_name === b.first_name &&
    a.last_name === b.last_name &&
    String(a.onboarding_completed_at ?? '') === String(b.onboarding_completed_at ?? '') &&
    String(a.updated_at) === String(b.updated_at) &&
    sameStringArray(a.roles, b.roles)
  )
}

function sameAccess(a: Access | null, b: Access | null): boolean {
  if (a === b) return true
  if (!a || !b) return false
  return (
    a.roles.length === b.roles.length &&
    a.permissions.length === b.permissions.length
  )
}

function sameTimezones(a: Timezone[] = [], b: Timezone[] = []): boolean {
  if (a === b) return true
  if (a.length !== b.length) return false
  return a.every((tz, i) => tz.name === b[i]?.name && tz.display_name === b[i]?.display_name)
}

export const createUserSlice: StateCreator<UserSlice, [], [], UserSlice> = (set) => ({
  user: null,
  access: null,
  timezones: [],
  isAuthenticated: false,
  isLoading: true,

  setUser: (user) => set((state) => {
    if (sameUser(state.user, user) && state.isAuthenticated === !!user) return state
    return { user, isAuthenticated: !!user }
  }),
  setAccess: (access) => set((state) => {
    if (sameAccess(state.access, access)) return state
    return { access }
  }),
  setTimezones: (timezones) => set((state) => {
    if (sameTimezones(state.timezones, timezones)) return state
    return { timezones }
  }),
  setIsLoading: (isLoading) => set((state) => {
    if (state.isLoading === isLoading) return state
    return { isLoading }
  }),
  logout: () => set({ user: null, access: null, isAuthenticated: false }),
})
