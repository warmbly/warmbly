import type { StateCreator } from 'zustand'

export type Theme = 'light' | 'dark' | 'system'

// Unibox list-column bounds. The max keeps a readable thread pane on a laptop;
// the element also carries a `max-w: calc(100% - 360px)` so a width stored on a
// wide monitor cannot crush the thread on a narrow one.
export const UNIBOX_LIST_MIN_WIDTH = 280
export const UNIBOX_LIST_MAX_WIDTH = 620
export const UNIBOX_LIST_DEFAULT_WIDTH = 360

const clampListWidth = (w: number): number => {
  if (!Number.isFinite(w)) return UNIBOX_LIST_DEFAULT_WIDTH
  return Math.round(Math.min(UNIBOX_LIST_MAX_WIDTH, Math.max(UNIBOX_LIST_MIN_WIDTH, w)))
}

export interface UISlice {
  // Sidebar
  sidebarCollapsed: boolean
  sidebarMobileOpen: boolean

  // Theme
  theme: Theme
  resolvedTheme: 'light' | 'dark'

  // Modals
  tagsModalOpen: boolean
  foldersModalOpen: boolean
  addEmailModalOpen: boolean
  shortcutsModalOpen: boolean
  commandPaletteOpen: boolean

  // AI assistant panel (right-side, persistent across routes)
  aiAssistantOpen: boolean

  // Unibox layout preferences (persisted). The list column is drag-resizable
  // against the thread pane; the CRM rail remembers the last explicit toggle
  // so closing it survives opening the next thread.
  uniboxListWidth: number
  uniboxContactRailOpen: boolean

  // Actions - Sidebar
  toggleSidebar: () => void
  setSidebarCollapsed: (collapsed: boolean) => void
  setSidebarMobileOpen: (open: boolean) => void

  // Actions - Theme
  setTheme: (theme: Theme) => void
  setResolvedTheme: (theme: 'light' | 'dark') => void

  // Actions - Modals
  setTagsModalOpen: (open: boolean) => void
  setFoldersModalOpen: (open: boolean) => void
  setAddEmailModalOpen: (open: boolean) => void
  setShortcutsModalOpen: (open: boolean) => void
  setCommandPaletteOpen: (open: boolean) => void
  setAIAssistantOpen: (open: boolean) => void
  toggleAIAssistant: () => void

  // Actions - Unibox layout
  setUniboxListWidth: (width: number) => void
  setUniboxContactRailOpen: (open: boolean) => void
}

const getInitialTheme = (): Theme => {
  if (typeof window === 'undefined') return 'system'
  return (localStorage.getItem('theme') as Theme) || 'system'
}

// The dashboard is light-only today: every surface is styled on white, so a
// resolved dark theme would flip only the CSS-variable components (command
// palette, toasts) and look broken. 'dark'/'system' are accepted but resolve
// to light until a real dark theme ships.
const getResolvedTheme = (_theme: Theme): 'light' | 'dark' => {
  return 'light'
}

export const createUISlice: StateCreator<UISlice, [], [], UISlice> = (set, get) => ({
  // Sidebar
  sidebarCollapsed: false,
  sidebarMobileOpen: false,

  // Theme
  theme: getInitialTheme(),
  resolvedTheme: getResolvedTheme(getInitialTheme()),

  // Modals
  tagsModalOpen: false,
  foldersModalOpen: false,
  addEmailModalOpen: false,
  shortcutsModalOpen: false,
  commandPaletteOpen: false,
  aiAssistantOpen: false,

  // Unibox layout
  uniboxListWidth: UNIBOX_LIST_DEFAULT_WIDTH,
  uniboxContactRailOpen: true,

  // Actions - Sidebar
  toggleSidebar: () => set((state) => ({ sidebarCollapsed: !state.sidebarCollapsed })),
  setSidebarCollapsed: (sidebarCollapsed) =>
    set((state) => (state.sidebarCollapsed === sidebarCollapsed ? state : { sidebarCollapsed })),
  setSidebarMobileOpen: (sidebarMobileOpen) =>
    set((state) => (state.sidebarMobileOpen === sidebarMobileOpen ? state : { sidebarMobileOpen })),

  // Actions - Theme
  setTheme: (theme) => {
    if (get().theme === theme) return
    localStorage.setItem('theme', theme)
    const resolvedTheme = getResolvedTheme(theme)
    document.documentElement.classList.remove('dark')
    set({ theme, resolvedTheme })
  },
  setResolvedTheme: (resolvedTheme) =>
    set((state) => (state.resolvedTheme === resolvedTheme ? state : { resolvedTheme })),

  // Actions - Modals
  setTagsModalOpen: (tagsModalOpen) =>
    set((state) => (state.tagsModalOpen === tagsModalOpen ? state : { tagsModalOpen })),
  setFoldersModalOpen: (foldersModalOpen) =>
    set((state) => (state.foldersModalOpen === foldersModalOpen ? state : { foldersModalOpen })),
  setAddEmailModalOpen: (addEmailModalOpen) =>
    set((state) => (state.addEmailModalOpen === addEmailModalOpen ? state : { addEmailModalOpen })),
  setShortcutsModalOpen: (shortcutsModalOpen) =>
    set((state) => (state.shortcutsModalOpen === shortcutsModalOpen ? state : { shortcutsModalOpen })),
  setCommandPaletteOpen: (commandPaletteOpen) =>
    set((state) => (state.commandPaletteOpen === commandPaletteOpen ? state : { commandPaletteOpen })),
  setAIAssistantOpen: (aiAssistantOpen) =>
    set((state) => (state.aiAssistantOpen === aiAssistantOpen ? state : { aiAssistantOpen })),
  toggleAIAssistant: () => set((state) => ({ aiAssistantOpen: !state.aiAssistantOpen })),

  // Actions - Unibox layout
  setUniboxListWidth: (width) => {
    const uniboxListWidth = clampListWidth(width)
    set((state) => (state.uniboxListWidth === uniboxListWidth ? state : { uniboxListWidth }))
  },
  setUniboxContactRailOpen: (uniboxContactRailOpen) =>
    set((state) =>
      state.uniboxContactRailOpen === uniboxContactRailOpen ? state : { uniboxContactRailOpen },
    ),
})
