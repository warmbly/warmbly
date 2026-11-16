import type { StateCreator } from 'zustand'
import { overlayClientView, readClientView, writeClientView, type ClientView } from '@/lib/clientView'

export interface Organization {
  id: string
  name: string
  category?: "standard" | "test"
  avatar?: string
  avatar_url?: string | null
  plan?: string
  // Built-in role id or a custom role's name.
  role: string
  // Caller's effective permission bitmask in this org (custom-role aware).
  permissions?: number
  // "restricted" reaches only the campaigns and mailboxes granted to the caller.
  access_scope?: "workspace" | "restricted"
  // What a restricted caller may act with inside their grants; 0 is read-only.
  scope_actions?: number
  // The agency this workspace is a client of, when it is one.
  agency?: { id: string; name: string; can_leave?: boolean }
}

export interface OrganizationSlice {
  organizations: Organization[]
  // What the tab draws: the member's own row, narrowed while a client view is on.
  currentOrganization: Organization | null
  // The member's own row for the current workspace, before any client view.
  memberOrganization: Organization | null
  clientView: ClientView | null

  setOrganizations: (organizations: Organization[]) => void
  setCurrentOrganization: (org: Organization | null) => void
  switchOrganization: (orgId: string) => void
  // Null ends the client view; a view for another workspace than the current one is ignored.
  setClientView: (view: ClientView | null) => void
}

export const createOrganizationSlice: StateCreator<OrganizationSlice, [], [], OrganizationSlice> = (set, get) => {
  // Every write of the current workspace goes through here, so the view follows it and ends when it moves.
  const select = (member: Organization | null, view: ClientView | null = get().clientView) => {
    const kept = member && view && view.orgId === member.id ? view : null
    if (kept !== view) writeClientView(null)
    const prev = get()
    // Same row and same view keep the same object: effects that depend on it would otherwise rerun forever.
    const unchanged = prev.memberOrganization === member && prev.clientView === kept && prev.currentOrganization
    return {
      memberOrganization: member,
      clientView: kept,
      currentOrganization: member ? (unchanged || overlayClientView(member, kept)) : null,
    }
  }

  return {
  organizations: [],
  currentOrganization: null,
  memberOrganization: null,
  clientView: readClientView(),

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
    const current = get().memberOrganization ?? get().currentOrganization
    // Adopt the FRESH row for the current org (role/permissions may have
    // changed since the last fetch — e.g. a role edit propagated live).
    const fresh = current
      ? (organizations.find((o) => o.id === current.id) ?? null)
      : null
    set({ organizations, ...select(fresh) })
  },

  setCurrentOrganization: (org) => set(select(org)),

  // Local-only switch. Callers MUST also POST `/organization/switch/:id`
  // (use the `useSwitchOrganization` hook) or the server session will
  // disagree with the UI. Kept for callers that have already done the
  // server round-trip and just need to advance local state.
  switchOrganization: (orgId) => {
    const org = get().organizations.find((o) => o.id === orgId)
    if (org) {
      set(select(org))
    }
  },

  setClientView: (view) => {
    const member = get().memberOrganization
    const kept = view && member && view.orgId === member.id ? view : null
    writeClientView(kept)
    set(select(member, kept))
  },
  }
}
