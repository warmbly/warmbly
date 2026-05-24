import type { StateCreator } from 'zustand'
import type APIKey from '@/lib/api/models/app/apikeys/APIKey'
import type APIPermission from '@/lib/api/models/app/apikeys/APIPermission'

export interface APIKeysSlice {
  apiKeys: APIKey[]
  apiPermissions: APIPermission[]

  setAPIKeys: (keys: APIKey[]) => void
  addAPIKey: (key: APIKey) => void
  updateAPIKey: (id: string, updates: Partial<APIKey>) => void
  removeAPIKey: (id: string) => void
  setAPIPermissions: (permissions: APIPermission[]) => void
}

export const createAPIKeysSlice: StateCreator<APIKeysSlice, [], [], APIKeysSlice> = (set) => ({
  apiKeys: [],
  apiPermissions: [],

  setAPIKeys: (apiKeys) => set({ apiKeys }),
  addAPIKey: (key) => set((state) => ({ apiKeys: [...state.apiKeys, key] })),
  updateAPIKey: (id, updates) =>
    set((state) => ({
      apiKeys: state.apiKeys.map((k) => (k.id === id ? { ...k, ...updates } : k)),
    })),
  removeAPIKey: (id) => set((state) => ({ apiKeys: state.apiKeys.filter((k) => k.id !== id) })),
  setAPIPermissions: (apiPermissions) => set({ apiPermissions }),
})
