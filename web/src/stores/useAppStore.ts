import { create } from 'zustand'
import { devtools, persist } from 'zustand/middleware'
import { useShallow } from 'zustand/react/shallow'
import { createUserSlice, type UserSlice } from './slices/userSlice'
import { createOrganizationSlice, type OrganizationSlice } from './slices/organizationSlice'
import { createUISlice, clampUniboxListWidth, clampUniboxRailWidth, sanitizeNavCollapsedSections, sanitizeUniboxRailFavorites, sanitizeUniboxRailHidden, sanitizeUniboxRailOrder, type UISlice } from './slices/uiSlice'
import { createShortcutSlice, type ShortcutSlice } from './slices/shortcutSlice'
import { createDataSlice, type DataSlice } from './slices/dataSlice'
import { createRealtimeSlice, type RealtimeSlice } from './slices/realtimeSlice'
import { createCRMSlice, type CRMSlice } from './slices/crmSlice'
import { createUniboxSlice, type UniboxSlice } from './slices/uniboxSlice'
import { createAnalyticsSlice, type AnalyticsSlice } from './slices/analyticsSlice'
import { createSubscriptionSlice, type SubscriptionSlice } from './slices/subscriptionSlice'
import { createAPIKeysSlice, type APIKeysSlice } from './slices/apiKeysSlice'
import { createPresenceSlice, type PresenceSlice } from './slices/presenceSlice'
import { createAgentSlice, type AgentSlice } from './slices/agentSlice'
import type { Organization } from './slices/organizationSlice'
import { overlayClientView, type ClientView } from '@/lib/clientView'

export type AppStore = UserSlice & OrganizationSlice & UISlice & ShortcutSlice & DataSlice
  & RealtimeSlice & CRMSlice & UniboxSlice & AnalyticsSlice & SubscriptionSlice & APIKeysSlice & PresenceSlice & AgentSlice

// A stored workspace is the member's own row; this tab's client view, when it names it, narrows it again.
function restoreOrganization(member: Organization | null, view: ClientView | null) {
  const kept = member && view && view.orgId === member.id ? view : null
  return { memberOrganization: member, clientView: kept, currentOrganization: member ? overlayClientView(member, kept) : null }
}

export const useAppStore = create<AppStore>()(
  devtools(
    persist(
      (...args) => ({
        ...createUserSlice(...args),
        ...createOrganizationSlice(...args),
        ...createUISlice(...args),
        ...createShortcutSlice(...args),
        ...createDataSlice(...args),
        ...createRealtimeSlice(...args),
        ...createCRMSlice(...args),
        ...createUniboxSlice(...args),
        ...createAnalyticsSlice(...args),
        ...createSubscriptionSlice(...args),
        ...createAPIKeysSlice(...args),
        ...createPresenceSlice(...args),
        ...createAgentSlice(...args),
      }),
      {
        name: 'warmbly-storage',
        // v1: Remie opens as a floating window. A docked value stored before
        // that was the old default, not a choice, so it is reset once.
        version: 1,
        migrate: (persisted, version) => {
          const p = (persisted ?? {}) as Partial<AppStore>
          if (version < 1) p.agentFloating = true
          return p as AppStore
        },
        // Rehydration does not go through the slice setters, so re-clamp the
        // stored values that have bounds. Without this a value from an older build
        // (or a hand-edited one) renders as `width: NaNpx`.
        merge: (persisted, current) => {
          const p = (persisted ?? {}) as Partial<AppStore>
          return {
            ...current,
            ...p,
            uniboxListWidth: clampUniboxListWidth(p.uniboxListWidth),
            uniboxRailWidth: clampUniboxRailWidth(p.uniboxRailWidth),
            navCollapsedSections: sanitizeNavCollapsedSections(p.navCollapsedSections),
            uniboxRailFolded: sanitizeNavCollapsedSections(p.uniboxRailFolded),
            uniboxRailHidden: sanitizeUniboxRailHidden(p.uniboxRailHidden),
            uniboxRailOrder: sanitizeUniboxRailOrder(p.uniboxRailOrder),
            uniboxRailSectionOrder: sanitizeUniboxRailHidden(p.uniboxRailSectionOrder),
            uniboxRailFavorites: sanitizeUniboxRailFavorites(p.uniboxRailFavorites),
            uniboxRailOwner: typeof p.uniboxRailOwner === 'string' ? p.uniboxRailOwner : null,
            // The theme lives under its own key, which index.html reads before first paint.
            theme: current.theme,
            resolvedTheme: current.resolvedTheme,
            ...restoreOrganization(p.currentOrganization ?? null, current.clientView),
          }
        },
        partialize: (state) => ({
          // Only persist UI preferences
          navCollapsed: state.navCollapsed,
          navCollapsedSections: state.navCollapsedSections,
          // Assistant panel layout (edge + width + floating window geometry)
          agentSide: state.agentSide,
          agentWidth: state.agentWidth,
          agentFloating: state.agentFloating,
          agentFloatRect: state.agentFloatRect,
          // Unibox layout (pane widths + CRM rail default)
          uniboxListWidth: state.uniboxListWidth,
          uniboxRailWidth: state.uniboxRailWidth,
          uniboxContactRailOpen: state.uniboxContactRailOpen,
          // Unibox scope rail (folds, hidden rows, row and section order, favorites).
          // All but the folds are a cache of the member's saved rail (useUniboxRailSync).
          uniboxRailFolded: state.uniboxRailFolded,
          uniboxRailHidden: state.uniboxRailHidden,
          uniboxRailOrder: state.uniboxRailOrder,
          uniboxRailSectionOrder: state.uniboxRailSectionOrder,
          uniboxRailFavorites: state.uniboxRailFavorites,
          uniboxRailOwner: state.uniboxRailOwner,
          // Persist current organization selection: the member's own row, never a client view's narrowing.
          currentOrganization: state.memberOrganization ?? state.currentOrganization,
        }),
      }
    ),
    {
      name: 'Warmbly Store',
    }
  )
)

// Selectors for commonly used state combinations
export const useUser = () => useAppStore((state) => state.user)
export const useIsAuthenticated = () => useAppStore((state) => state.isAuthenticated)
export const useTheme = () =>
  useAppStore(useShallow((state) => ({ theme: state.theme, resolvedTheme: state.resolvedTheme })))
export const useSidebar = () =>
  useAppStore(useShallow((state) => ({
    collapsed: state.navCollapsed,
    mobileOpen: state.sidebarMobileOpen,
    toggle: state.toggleSidebar,
    setCollapsed: state.setSidebarCollapsed,
    setMobileOpen: state.setSidebarMobileOpen,
  })))
export const useCurrentOrg = () => useAppStore((state) => state.currentOrganization)
export const useOrganizations = () =>
  useAppStore(useShallow((state) => ({
    organizations: state.organizations,
    current: state.currentOrganization,
    switch: state.switchOrganization,
  })))
export const useCachedData = () =>
  useAppStore(useShallow((state) => ({
    campaigns: state.campaigns,
    emails: state.emails,
    tags: state.tags,
    folders: state.folders,
    categories: state.categories,
  })))
export const useConnectionStatus = () =>
  useAppStore(useShallow((state) => ({
    status: state.connectionStatus,
    quality: state.connectionQuality,
    reconnectAttempt: state.reconnectAttempt,
  })))
export const useUnseenCount = () => useAppStore((state) => state.unseenCount)
