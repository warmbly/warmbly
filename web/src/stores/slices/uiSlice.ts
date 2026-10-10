import type { StateCreator } from 'zustand'
import { applyTheme, readStoredTheme, resolveTheme, storeTheme, type ResolvedTheme, type Theme, type ThemeOrigin } from '@/lib/theme'

export type { Theme } from '@/lib/theme'

// Unibox list-column bounds. These are the preference's bounds; what the column
// can actually render is additionally capped against the viewport at the drag
// site, so the stored value survives a narrow window instead of being rewritten
// by it.
export const UNIBOX_LIST_MIN_WIDTH = 280
export const UNIBOX_LIST_MAX_WIDTH = 620
export const UNIBOX_LIST_DEFAULT_WIDTH = 360

// A favorite's own name is a rail label, so it stays about as long as one.
export const UNIBOX_RAIL_FAVORITE_NAME_MAX = 40

// A scope pinned to the rail's Favorites section, with an optional name of its own.
export interface UniboxRailFavorite {
  key: string
  name?: string
}

export const UNIBOX_RAIL_MIN_WIDTH = 180
export const UNIBOX_RAIL_MAX_WIDTH = 360
export const UNIBOX_RAIL_DEFAULT_WIDTH = 220

export const clampUniboxRailWidth = (w: unknown): number => {
  if (typeof w !== 'number' || !Number.isFinite(w)) return UNIBOX_RAIL_DEFAULT_WIDTH
  return Math.round(Math.min(UNIBOX_RAIL_MAX_WIDTH, Math.max(UNIBOX_RAIL_MIN_WIDTH, w)))
}

// Exported because rehydration bypasses the setter: zustand's default merge
// writes localStorage straight into state, so the clamp has to run there too or
// a hand-edited (or newly out-of-range) value reaches the DOM unchecked.
export const clampUniboxListWidth = (w: unknown): number => {
  // Only a real number survives. Coercing would be worse than useless here:
  // `Number(null)` is 0, so a null in storage would silently become the minimum
  // width instead of falling back to the default.
  if (typeof w !== 'number' || !Number.isFinite(w)) return UNIBOX_LIST_DEFAULT_WIDTH
  return Math.round(Math.min(UNIBOX_LIST_MAX_WIDTH, Math.max(UNIBOX_LIST_MIN_WIDTH, w)))
}

export interface UISlice {
  // Sidebar. Deliberately NOT the old `sidebarCollapsed` key: that one was
  // persisted and toggled by `b` for a long time while nothing rendered from
  // it, so a stored `true` reflects a keystroke nobody remembers. A new key
  // starts everyone expanded without needing a migration, which zustand would
  // not have run anyway: it only migrates a store whose version is a number,
  // and every store written before this had no version field at all.
  navCollapsed: boolean
  sidebarMobileOpen: boolean
  // Folded sidebar sections, keyed by the section's stable id (not its label).
  navCollapsedSections: Record<string, boolean>

  // Unibox scope rail: folded sections (stable ids, same shape as the nav map)
  // and the rows the user hid, as the rail's scopeKey values.
  uniboxRailFolded: Record<string, boolean>
  uniboxRailHidden: string[]
  // Row order per section and the order of the sections; absent means default.
  uniboxRailOrder: Record<string, string[]>
  uniboxRailSectionOrder: string[]
  // Favorites, in rail order; each key is a scopeKey from any section.
  uniboxRailFavorites: UniboxRailFavorite[]
  // Whose saved rail the four fields above hold ("<userId>:<orgId>"); null
  // for a layout made before the rail was saved to the account.
  uniboxRailOwner: string | null

  // Theme
  theme: Theme
  resolvedTheme: ResolvedTheme

  // Modals
  tagsModalOpen: boolean
  foldersModalOpen: boolean
  addEmailModalOpen: boolean
  shortcutsModalOpen: boolean
  commandPaletteOpen: boolean

  // AI assistant panel (right-side, persistent across routes)
  aiAssistantOpen: boolean

  // Product tour overlay; not persisted, whether it is still owed rides the user record.
  productTourOpen: boolean

  // Unibox layout preferences (persisted). The list column is drag-resizable
  // against the thread pane; the CRM rail remembers the last explicit toggle
  // so closing it survives opening the next thread.
  uniboxListWidth: number
  uniboxRailWidth: number
  uniboxContactRailOpen: boolean

  // Actions - Sidebar
  toggleSidebar: () => void
  setSidebarCollapsed: (collapsed: boolean) => void
  setSidebarMobileOpen: (open: boolean) => void
  toggleNavSection: (id: string) => void

  // Actions - Unibox scope rail
  toggleUniboxRailSection: (id: string) => void
  toggleUniboxRailRow: (key: string) => void
  setUniboxRailFolded: (folded: Record<string, boolean>) => void
  setUniboxRailRowsHidden: (keys: string[], hidden: boolean) => void
  setUniboxRailOrder: (section: string, keys: string[] | null) => void
  setUniboxRailSectionOrder: (ids: string[]) => void
  toggleUniboxRailFavorite: (key: string) => void
  setUniboxRailFavorites: (favorites: UniboxRailFavorite[]) => void

  // Actions - Theme
  // origin is where the switch was asked for; the new theme spreads from it.
  setTheme: (theme: Theme, origin?: ThemeOrigin) => void
  setResolvedTheme: (theme: ResolvedTheme, origin?: ThemeOrigin) => void

  // Actions - Modals
  setTagsModalOpen: (open: boolean) => void
  setFoldersModalOpen: (open: boolean) => void
  setAddEmailModalOpen: (open: boolean) => void
  setShortcutsModalOpen: (open: boolean) => void
  setCommandPaletteOpen: (open: boolean) => void
  setAIAssistantOpen: (open: boolean) => void
  toggleAIAssistant: () => void
  setProductTourOpen: (open: boolean) => void

  // Actions - Unibox layout
  setUniboxListWidth: (width: number) => void
  setUniboxRailWidth: (width: number) => void
  setUniboxContactRailOpen: (open: boolean) => void
}

// Rehydration bypasses the setter, so a stored value that is not a map of
// booleans (older build, hand edit) falls back to everything expanded.
export const sanitizeNavCollapsedSections = (v: unknown): Record<string, boolean> => {
  if (!v || typeof v !== 'object' || Array.isArray(v)) return {}
  return Object.fromEntries(Object.entries(v).filter(([, folded]) => typeof folded === 'boolean'))
}

// Same rehydration gap for the rail's hidden rows: anything that is not a
// string would never match a scope key, and a duplicate would make one click
// on the row's checkbox appear to do nothing.
export const sanitizeUniboxRailHidden = (v: unknown): string[] => {
  if (!Array.isArray(v)) return []
  return [...new Set(v.filter((k): k is string => typeof k === 'string'))]
}

// Row orders rehydrate as a map of string lists, each deduplicated.
export const sanitizeUniboxRailOrder = (v: unknown): Record<string, string[]> => {
  if (!v || typeof v !== 'object' || Array.isArray(v)) return {}
  return Object.fromEntries(
    Object.entries(v).flatMap(([id, keys]) => (Array.isArray(keys) ? [[id, sanitizeUniboxRailHidden(keys)]] : [])),
  )
}

// A blank name means "use the row's own label"; anything else is trimmed and capped by code point.
export const cleanUniboxRailFavoriteName = (v: unknown): string | undefined => {
  if (typeof v !== 'string') return undefined
  const name = Array.from(v.replace(/\s+/g, ' ').trim()).slice(0, UNIBOX_RAIL_FAVORITE_NAME_MAX).join('').trim()
  return name || undefined
}

// Favorites rehydrate as a list of entries with a string key, one per key.
export const sanitizeUniboxRailFavorites = (v: unknown): UniboxRailFavorite[] => {
  if (!Array.isArray(v)) return []
  const seen = new Set<string>()
  return v.flatMap((entry): UniboxRailFavorite[] => {
    if (!entry || typeof entry !== 'object' || Array.isArray(entry)) return []
    const { key, name } = entry as { key?: unknown; name?: unknown }
    if (typeof key !== 'string' || !key || seen.has(key)) return []
    seen.add(key)
    const clean = cleanUniboxRailFavoriteName(name)
    return [clean ? { key, name: clean } : { key }]
  })
}

// A stored order over keys that come and go: known keys keep the stored order,
// and a key the stored order never saw lands right after its default predecessor.
export const applyRailOrder = (defaults: string[], stored: string[] | undefined): string[] => {
  if (!stored?.length) return defaults
  const known = new Set(defaults)
  const out = stored.filter((k) => known.has(k))
  const placed = new Set(out)
  defaults.forEach((k, i) => {
    if (placed.has(k)) return
    const prev = i > 0 ? out.indexOf(defaults[i - 1]) : -1
    out.splice(prev + 1, 0, k)
    placed.add(k)
  })
  return out
}

export const createUISlice: StateCreator<UISlice, [], [], UISlice> = (set, get) => ({
  // Sidebar
  navCollapsed: false,
  sidebarMobileOpen: false,
  navCollapsedSections: {},
  uniboxRailFolded: {},
  uniboxRailHidden: [],
  uniboxRailOrder: {},
  uniboxRailSectionOrder: [],
  uniboxRailFavorites: [],
  uniboxRailOwner: null,

  // Theme
  theme: readStoredTheme(),
  resolvedTheme: resolveTheme(readStoredTheme()),

  // Modals
  tagsModalOpen: false,
  foldersModalOpen: false,
  addEmailModalOpen: false,
  shortcutsModalOpen: false,
  commandPaletteOpen: false,
  aiAssistantOpen: false,
  productTourOpen: false,

  // Unibox layout
  uniboxListWidth: UNIBOX_LIST_DEFAULT_WIDTH,
  uniboxRailWidth: UNIBOX_RAIL_DEFAULT_WIDTH,
  uniboxContactRailOpen: false,

  // Actions - Sidebar
  toggleSidebar: () => set((state) => ({ navCollapsed: !state.navCollapsed })),
  setSidebarCollapsed: (navCollapsed) =>
    set((state) => (state.navCollapsed === navCollapsed ? state : { navCollapsed })),
  setSidebarMobileOpen: (sidebarMobileOpen) =>
    set((state) => (state.sidebarMobileOpen === sidebarMobileOpen ? state : { sidebarMobileOpen })),

  toggleNavSection: (id) =>
    set((state) => ({
      navCollapsedSections: { ...state.navCollapsedSections, [id]: !state.navCollapsedSections[id] },
    })),

  // Actions - Unibox scope rail
  toggleUniboxRailSection: (id) =>
    set((state) => ({
      uniboxRailFolded: { ...state.uniboxRailFolded, [id]: !state.uniboxRailFolded[id] },
    })),
  toggleUniboxRailRow: (key) =>
    set((state) => ({
      uniboxRailHidden: state.uniboxRailHidden.includes(key)
        ? state.uniboxRailHidden.filter((k) => k !== key)
        : [...state.uniboxRailHidden, key],
    })),
  setUniboxRailFolded: (folded) =>
    set((state) => ({ uniboxRailFolded: { ...state.uniboxRailFolded, ...folded } })),
  setUniboxRailRowsHidden: (keys, hidden) =>
    set((state) => {
      const rest = state.uniboxRailHidden.filter((k) => !keys.includes(k))
      return { uniboxRailHidden: hidden ? [...rest, ...keys] : rest }
    }),
  setUniboxRailOrder: (section, keys) =>
    set((state) => {
      const next = { ...state.uniboxRailOrder }
      if (keys) next[section] = keys
      else delete next[section]
      return { uniboxRailOrder: next }
    }),
  setUniboxRailSectionOrder: (ids) => set({ uniboxRailSectionOrder: ids }),
  toggleUniboxRailFavorite: (key) =>
    set((state) => ({
      uniboxRailFavorites: state.uniboxRailFavorites.some((f) => f.key === key)
        ? state.uniboxRailFavorites.filter((f) => f.key !== key)
        : [...state.uniboxRailFavorites, { key }],
    })),
  setUniboxRailFavorites: (favorites) => set({ uniboxRailFavorites: sanitizeUniboxRailFavorites(favorites) }),

  // Actions - Theme
  setTheme: (theme, origin) => {
    if (get().theme === theme) return
    storeTheme(theme)
    const resolvedTheme = resolveTheme(theme)
    applyTheme(resolvedTheme, origin)
    set({ theme, resolvedTheme })
  },
  setResolvedTheme: (resolvedTheme, origin) => {
    applyTheme(resolvedTheme, origin)
    set((state) => (state.resolvedTheme === resolvedTheme ? state : { resolvedTheme }))
  },

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
  setProductTourOpen: (productTourOpen) =>
    set((state) => (state.productTourOpen === productTourOpen ? state : { productTourOpen })),

  // Actions - Unibox layout
  setUniboxRailWidth: (width) => {
    const uniboxRailWidth = clampUniboxRailWidth(width)
    set((state) => (state.uniboxRailWidth === uniboxRailWidth ? state : { uniboxRailWidth }))
  },
  setUniboxListWidth: (width) => {
    const uniboxListWidth = clampUniboxListWidth(width)
    set((state) => (state.uniboxListWidth === uniboxListWidth ? state : { uniboxListWidth }))
  },
  setUniboxContactRailOpen: (uniboxContactRailOpen) =>
    set((state) =>
      state.uniboxContactRailOpen === uniboxContactRailOpen ? state : { uniboxContactRailOpen },
    ),
})
