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

  // Reconciles the local org list with whatever's currently selected.
  // We DO NOT auto-pick the first org here anymore — that was the bug
  // that made the sidebar show a workspace name while the server
  // session still had `current_organization_id = NULL`. Selection is
  // now owned by OrgGate (which routes to /select-org) and by explicit
  // user action via the sidebar switcher, both of which round-trip
  // through `POST /organization/switch/:id`.
  //
  // We still null out `currentOrganization` if it's no longer in the
  // membership list (e.g. user got removed from a workspace in another
  // tab) so the gate can redirect cleanly.
  setOrganizations: (organizations) => {
    const current = get().currentOrganization
    const stillMember = current
      ? organizations.some((o) => o.id === current.id)
      : false
    set({
      organizations,
      currentOrganization: stillMember ? current : null,
    })
  },

  setCurrentOrganization: (currentOrganization) => set({ currentOrganization }),

  // Local-only switch. Callers MUST also POST `/organization/switch/:id`
  // (use the `useSwitchOrganization` hook) or the server session will
  // disagree with the UI. Kept for callers that have already done the
  // server round-trip and just need to advance local state.
  switchOrganization: (orgId) => {
    const org = get().organizations.find((o) => o.id === orgId)
    if (org) {
      set({ currentOrganization: org })
    }
  },
})
