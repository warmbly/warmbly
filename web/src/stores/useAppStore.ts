import { create } from 'zustand'
import { devtools, persist } from 'zustand/middleware'
import { useShallow } from 'zustand/react/shallow'
import { createUserSlice, type UserSlice } from './slices/userSlice'
import { createOrganizationSlice, type OrganizationSlice } from './slices/organizationSlice'
import { createUISlice, clampUniboxListWidth, clampUniboxRailWidth, sanitizeNavCollapsedSections, sanitizeUniboxRailFavorites, sanitizeUniboxRailHidden, sanitizeUniboxRailOrder, clampAppearanceGlassOpacity, clampAppearanceGlassBlur, clampAppearanceBackgroundBlur, clampAppearanceBackgroundOpacity, sanitizeBackgroundPreset, sanitizeBackgroundImage, type UISlice } from './slices/uiSlice'
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
            glassOpacity: clampAppearanceGlassOpacity(p.glassOpacity),
            glassBlur: clampAppearanceGlassBlur(p.glassBlur),
            backgroundPreset: sanitizeBackgroundPreset(p.backgroundPreset),
            backgroundImage: sanitizeBackgroundImage(p.backgroundImage),
            backgroundBlur: clampAppearanceBackgroundBlur(p.backgroundBlur),
            backgroundOpacity: clampAppearanceBackgroundOpacity(p.backgroundOpacity),
          }
        },
        partialize: (state) => ({
          // Only persist UI preferences
          navCollapsed: state.navCollapsed,
          // Appearance
          glassmorphismEnabled: state.glassmorphismEnabled,
          glassOpacity: state.glassOpacity,
          glassBlur: state.glassBlur,
          backgroundPreset: state.backgroundPreset,
          backgroundImage: state.backgroundImage,
          backgroundBlur: state.backgroundBlur,
          backgroundOpacity: state.backgroundOpacity,
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
    collapsed: state.navCollapsed,
    mobileOpen: state.sidebarMobileOpen,
    toggle: state.toggleSidebar,
    setCollapsed: state.setSidebarCollapsed,
    setMobileOpen: state.setSidebarMobileOpen,
  })))
export const useAppearance = () => {
  const glassmorphismEnabled = useAppStore((state) => state.glassmorphismEnabled)
  const glassOpacity = useAppStore((state) => state.glassOpacity)
  const glassBlur = useAppStore((state) => state.glassBlur)
  const backgroundPreset = useAppStore((state) => state.backgroundPreset)
  const backgroundImage = useAppStore((state) => state.backgroundImage)
  const backgroundBlur = useAppStore((state) => state.backgroundBlur)
  const backgroundOpacity = useAppStore((state) => state.backgroundOpacity)
  const setGlassmorphismEnabled = useAppStore((state) => state.setGlassmorphismEnabled)
  const setGlassOpacity = useAppStore((state) => state.setGlassOpacity)
  const setGlassBlur = useAppStore((state) => state.setGlassBlur)
  const setBackgroundPreset = useAppStore((state) => state.setBackgroundPreset)
  const setBackgroundImage = useAppStore((state) => state.setBackgroundImage)
  const setBackgroundBlur = useAppStore((state) => state.setBackgroundBlur)
  const setBackgroundOpacity = useAppStore((state) => state.setBackgroundOpacity)
  return {
    glassmorphismEnabled,
    glassOpacity,
    glassBlur,
    backgroundPreset,
    backgroundImage,
    backgroundBlur,
    backgroundOpacity,
    setGlassmorphismEnabled,
    setGlassOpacity,
    setGlassBlur,
    setBackgroundPreset,
    setBackgroundImage,
    setBackgroundBlur,
    setBackgroundOpacity,
  }
}
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
