import { create } from 'zustand'
import { devtools, persist } from 'zustand/middleware'
import { useShallow } from 'zustand/react/shallow'
import { createUserSlice, type UserSlice } from './slices/userSlice'
import { createOrganizationSlice, type OrganizationSlice } from './slices/organizationSlice'
import { createUISlice, type UISlice } from './slices/uiSlice'
import { createShortcutSlice, type ShortcutSlice } from './slices/shortcutSlice'
import { createDataSlice, type DataSlice } from './slices/dataSlice'
import { createRealtimeSlice, type RealtimeSlice } from './slices/realtimeSlice'
import { createCRMSlice, type CRMSlice } from './slices/crmSlice'
import { createUniboxSlice, type UniboxSlice } from './slices/uniboxSlice'
import { createAnalyticsSlice, type AnalyticsSlice } from './slices/analyticsSlice'
import { createSubscriptionSlice, type SubscriptionSlice } from './slices/subscriptionSlice'
import { createAPIKeysSlice, type APIKeysSlice } from './slices/apiKeysSlice'
import { createPresenceSlice, type PresenceSlice } from './slices/presenceSlice'

export type AppStore = UserSlice & OrganizationSlice & UISlice & ShortcutSlice & DataSlice
  & RealtimeSlice & CRMSlice & UniboxSlice & AnalyticsSlice & SubscriptionSlice & APIKeysSlice & PresenceSlice

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
      }),
      {
        name: 'warmbly-storage',
        partialize: (state) => ({
          // Only persist UI preferences
          theme: state.theme,
          sidebarCollapsed: state.sidebarCollapsed,
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
