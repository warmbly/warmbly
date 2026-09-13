import { create } from 'zustand'
import { devtools, persist } from 'zustand/middleware'
import { useShallow } from 'zustand/react/shallow'
import { createUserSlice, type UserSlice } from './slices/userSlice'
import { createOrganizationSlice, type OrganizationSlice } from './slices/organizationSlice'
import { createUISlice, clampUniboxListWidth, type UISlice } from './slices/uiSlice'
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

export type AppStore = UserSlice & OrganizationSlice & UISlice & ShortcutSlice & DataSlice
  & RealtimeSlice & CRMSlice & UniboxSlice & AnalyticsSlice & SubscriptionSlice & APIKeysSlice & PresenceSlice & AgentSlice

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
        // v1: `sidebarCollapsed` was persisted and toggled by the documented `b`
        // key for a long time while nothing rendered from it, so anyone who ever
        // pressed it has `true` sitting in localStorage for an action they do not
        // remember. Now that the rail reads the flag, that would silently greet
        // them with an icon-only nav. Reset it once, on the upgrade only.
        version: 1,
        migrate: (persisted, from) =>
          from < 1
            ? { ...(persisted as Record<string, unknown>), sidebarCollapsed: false }
            : persisted,
        // Rehydration does not go through the slice setters, so re-clamp the one
        // stored value that has bounds. Without this a value from an older build
        // (or a hand-edited one) renders as `width: NaNpx`.
        merge: (persisted, current) => {
          const p = (persisted ?? {}) as Partial<AppStore>
          return {
            ...current,
            ...p,
            uniboxListWidth: clampUniboxListWidth(p.uniboxListWidth),
          }
        },
        partialize: (state) => ({
          // Only persist UI preferences
          theme: state.theme,
          sidebarCollapsed: state.sidebarCollapsed,
          // Assistant panel layout (edge + width + floating window geometry)
          agentSide: state.agentSide,
          agentWidth: state.agentWidth,
          agentFloating: state.agentFloating,
          agentFloatRect: state.agentFloatRect,
          // Unibox layout (list column width + CRM rail default)
          uniboxListWidth: state.uniboxListWidth,
          uniboxContactRailOpen: state.uniboxContactRailOpen,
          // Persist current organization selection
          currentOrganization: state.currentOrganization,
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
    collapsed: state.sidebarCollapsed,
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
export const useKeyboardNavigation = () =>
  useAppStore(useShallow((state) => ({
    sequence: state.keySequence,
    addToSequence: state.addToSequence,
    clearSequence: state.clearSequence,
    selectedIndex: state.selectedIndex,
    setSelectedIndex: state.setSelectedIndex,
    listLength: state.listLength,
    setListLength: state.setListLength,
    moveSelection: state.moveSelection,
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
