import type { StateCreator } from 'zustand'

export interface Organization {
  id: string
  name: string
  avatar?: string
  avatar_url?: string | null
  plan?: string
  role: 'owner' | 'admin' | 'member'
}

export interface OrganizationSlice {
  organizations: Organization[]
  currentOrganization: Organization | null

  setOrganizations: (organizations: Organization[]) => void
  setCurrentOrganization: (org: Organization | null) => void
  switchOrganization: (orgId: string) => void
}

export const createOrganizationSlice: StateCreator<OrganizationSlice, [], [], OrganizationSlice> = (set, get) => ({
  organizations: [],
  currentOrganization: null,

  setOrganizations: (organizations) => {
    set({ organizations })
    // Auto-select first org if none selected
    if (!get().currentOrganization && organizations.length > 0) {
      set({ currentOrganization: organizations[0] })
    }
  },

  setCurrentOrganization: (currentOrganization) => set({ currentOrganization }),

  switchOrganization: (orgId) => {
    const org = get().organizations.find((o) => o.id === orgId)
    if (org) {
      set({ currentOrganization: org })
    }
  },
})
