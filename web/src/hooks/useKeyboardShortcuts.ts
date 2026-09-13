// Every keyboard shortcut in the dashboard, declared once.
//
// The rule this file exists to enforce: **a shortcut the `?` modal shows is a
// shortcut the dispatcher runs.** They used to be two unrelated literals in
// here, so nine rows had no implementation at all and one more was shadowed by
// a single-key branch that returned before the sequence could resolve (#484).
// Now the modal renders this registry and the dispatcher walks it, so a row
// with nothing behind it cannot be written: `run` is required, and a row that
// needs something the current screen does not provide is hidden rather than
// shown dead.

import { useCallback, useEffect, type KeyboardEvent as ReactKeyboardEvent } from 'react'
import { useNavigate, type NavigateFunction } from 'react-router-dom'
import { useAppStore } from '@/stores'
import { useComposeStore } from '@/hooks/useComposeStore'
import { checkPermission } from '@/hooks/usePermission'
import { shortcutAction, type ShortcutActions } from '@/hooks/useShortcutActions'

export type ShortcutGroupId = 'navigation' | 'list' | 'actions' | 'assistant'

export interface ShortcutRow {
  /** As shown in the modal. */
  keys: string[]
  description: string
  group: ShortcutGroupId
  /** The screen-provided action this needs; without a provider the row is hidden. */
  needs?: keyof ShortcutActions
  /** Any further condition (a permission, a viewport) for the row being live. */
  available?: () => boolean
}

type Ctx = { navigate: NavigateFunction }

export interface GlobalShortcut extends ShortcutRow {
  /** Single-press form. */
  match?: (e: KeyboardEvent) => boolean
  /** Multi-key form, e.g. ['g', 'k']. */
  sequence?: string[]
  /** Fires even while the caret is in an input (modifier combos only). */
  whileTyping?: boolean
  run: (ctx: Ctx) => void
}

// A bare key with no modifier held. Every single-press shortcut goes through
// this, so none of them can collide with a Ctrl/Cmd combo.
const plain = (k: string) => (e: KeyboardEvent) =>
  !e.ctrlKey && !e.metaKey && !e.altKey && !e.shiftKey && e.key.toLowerCase() === k

const mod = (e: KeyboardEvent) => e.ctrlKey || e.metaKey

// Enter belongs to whatever the user has focused. Without this the list's
// "open selected item" would both steal the key from a focused button and
// cancel its activation.
const onAControl = (t: EventTarget | null) =>
  !!(t as HTMLElement | null)?.closest?.(
    'button, a[href], select, summary, [role="button"], [role="menuitem"], [role="tab"]',
  )

// Escape belongs to the innermost layer: a dialog, the confirm, or any of the
// app's popovers (they all carry data-floating). Clearing a list selection
// behind an open layer is not what the key was pressed for.
const layerOpen = () =>
  typeof document !== 'undefined' &&
  !!document.querySelector('[role="dialog"], [role="alertdialog"], [data-floating]')

const navRoutes: [key: string, path: string, label: string][] = [
  ['e', '/app/emails', 'Email Accounts'],
  ['c', '/app/contacts', 'Contacts'],
  ['m', '/app/campaigns', 'Campaigns'],
  ['u', '/app/unibox', 'Unibox'],
  ['a', '/app/analytics', 'Analytics'],
  ['p', '/app/crm/pipelines', 'Pipelines'],
  ['d', '/app/crm/deals', 'Deals'],
  ['t', '/app/crm/tasks', 'Tasks'],
  ['l', '/app/templates', 'Templates'],
  ['k', '/app/api-keys', 'API Keys'],
  ['s', '/app/settings', 'Settings'],
]

// The first visible search box on the page, for screens that have one but no
// keyboard integration of their own. `data-search-input` is on the shared
// SearchInput primitive, so this follows the convention rather than a list.
function pageSearchInput(): HTMLInputElement | null {
  if (typeof document === 'undefined') return null
  const nodes = Array.from(
    document.querySelectorAll<HTMLInputElement>('input[data-search-input]'),
  )
  const usable = nodes.filter((el) => !el.disabled)
  // Prefer one that is actually on screen: a responsive page can render a
  // mobile and a desktop search box and hide one of them. offsetParent is null
  // inside a display:none subtree, and a fixed-position input has none either,
  // hence the rects check. Neither works in a layout-less environment, so fall
  // back to the first rather than reporting the page has no search at all.
  return (
    usable.find((el) => el.offsetParent !== null || el.getClientRects().length > 0) ??
    usable[0] ??
    null
  )
}

export const globalShortcuts: GlobalShortcut[] = [
  // ── The assistant, which is reachable while typing ───────────────────────
  {
    keys: ['Ctrl', 'i'],
    description: 'Open / close the assistant',
    group: 'assistant',
    whileTyping: true,
    available: () => checkPermission('USE_AI'),
    match: (e) => mod(e) && e.key.toLowerCase() === 'i',
    run: () => {
      const s = useAppStore.getState()
      // Minimized means docked to the status bar, so the combo restores it
      // rather than closing a panel the user cannot see.
      if (s.aiAssistantOpen && s.agentMinimized) {
        s.setAgentMinimized(false)
        return
      }
      if (!s.aiAssistantOpen) s.setAgentMinimized(false)
      s.toggleAIAssistant()
    },
  },
  // ── Navigation: g then a letter ───────────────────────────────────────────
  ...navRoutes.map<GlobalShortcut>(([key, path, label]) => ({
    keys: ['g', key],
    description: `Go to ${label}`,
    group: 'navigation',
    sequence: ['g', key],
    run: ({ navigate }) => navigate(path),
  })),

  // ── List navigation: only on screens that register a list ─────────────────
  {
    keys: ['j'],
    description: 'Move down in list',
    group: 'list',
    needs: 'listMove',
    match: plain('j'),
    run: () => shortcutAction('listMove')?.(1),
  },
  {
    keys: ['k'],
    description: 'Move up in list',
    group: 'list',
    needs: 'listMove',
    match: plain('k'),
    run: () => shortcutAction('listMove')?.(-1),
  },
  {
    keys: ['g', 'g'],
    description: 'Go to first item',
    group: 'list',
    needs: 'listEdge',
    sequence: ['g', 'g'],
    run: () => shortcutAction('listEdge')?.('first'),
  },
  {
    keys: ['G'],
    description: 'Go to last item',
    group: 'list',
    needs: 'listEdge',
    match: (e) => !mod(e) && !e.altKey && e.shiftKey && e.key.toLowerCase() === 'g',
    run: () => shortcutAction('listEdge')?.('last'),
  },
  {
    keys: ['Enter'],
    description: 'Open selected item',
    group: 'list',
    needs: 'listOpen',
    match: (e) => !mod(e) && !e.altKey && e.key === 'Enter' && !onAControl(e.target),
    run: () => shortcutAction('listOpen')?.(),
  },
  {
    keys: ['Escape'],
    description: 'Clear the selection',
    group: 'list',
    needs: 'listDeselect',
    match: (e) => e.key === 'Escape' && !layerOpen(),
    run: () => shortcutAction('listDeselect')?.(),
  },
  {
    keys: ['c'],
    description: 'Label the open conversation',
    group: 'list',
    needs: 'labelThread',
    match: plain('c'),
    run: () => shortcutAction('labelThread')?.(),
  },

  // ── Actions ───────────────────────────────────────────────────────────────
  {
    keys: ['/'],
    description: 'Focus search',
    group: 'actions',
    available: () => !!shortcutAction('focusSearch') || !!pageSearchInput(),
    match: plain('/'),
    run: () => {
      const own = shortcutAction('focusSearch')
      if (own) {
        own()
        return
      }
      pageSearchInput()?.focus()
    },
  },
  {
    keys: ['n'],
    description: 'Compose a new email',
    group: 'actions',
    match: plain('n'),
    run: () => useComposeStore.getState().openCompose(),
  },
  {
    keys: ['b'],
    description: 'Collapse / expand the sidebar',
    group: 'actions',
    match: plain('b'),
    run: () => useAppStore.getState().toggleSidebar(),
  },
  {
    keys: ['?'],
    description: 'Show shortcuts',
    group: 'actions',
    match: (e) => !mod(e) && !e.altKey && (e.key === '?' || (e.shiftKey && e.key === '/')),
    run: () => useAppStore.getState().setShortcutsModalOpen(true),
  },
  {
    // Listed last but matched anywhere in this array: a modifier combo
    // cannot collide with the bare keys above it.
    keys: ['Ctrl', 'k'],
    description: 'Command palette',
    group: 'actions',
    whileTyping: true,
    match: (e) => mod(e) && e.key.toLowerCase() === 'k',
    run: () => useAppStore.getState().setCommandPaletteOpen(true),
  },
]

// Which single keys open a multi-key sequence. Derived, so adding a `g x` route
// needs no second edit here.
const sequenceStarters = new Set(
  globalShortcuts.flatMap((s) => (s.sequence ? [s.sequence[0]] : [])),
)

// ── Panel-scoped shortcuts ──────────────────────────────────────────────────
// These fire from the assistant panel's own onKeyDown (they must not reach the
// page while focus is inside a textarea), but they are declared here so the
// modal has one source for every shortcut in the product.

export type PanelCtx = {
  close: () => void
  cycleTab: (dir: number) => void
  newTab: () => void
  closeTab: () => void
  minimize: () => void
  togglePopOut: () => void
  /** Pop-out is desktop-only and unavailable while expanded. */
  canPopOut: boolean
}

export interface PanelShortcut extends ShortcutRow {
  match: (e: ReactKeyboardEvent) => boolean
  run: (ctx: PanelCtx) => void
}

const assistantAvailable = () => checkPermission('USE_AI')

export const panelShortcuts: PanelShortcut[] = [
  {
    keys: ['Esc'],
    description: 'Close the panel',
    group: 'assistant',
    available: assistantAvailable,
    match: (e) => e.key === 'Escape',
    run: (c) => c.close(),
  },
  {
    keys: ['Ctrl', ']'],
    description: 'Next conversation tab',
    group: 'assistant',
    available: assistantAvailable,
    match: (e) => (e.metaKey || e.ctrlKey) && !e.altKey && e.key === ']',
    run: (c) => c.cycleTab(1),
  },
  {
    keys: ['Ctrl', '['],
    description: 'Previous conversation tab',
    group: 'assistant',
    available: assistantAvailable,
    match: (e) => (e.metaKey || e.ctrlKey) && !e.altKey && e.key === '[',
    run: (c) => c.cycleTab(-1),
  },
  // Alt combos match on e.code because macOS Option remaps e.key to a symbol.
  {
    keys: ['Alt', 'n'],
    description: 'New chat',
    group: 'assistant',
    available: assistantAvailable,
    match: (e) => e.altKey && !e.metaKey && !e.ctrlKey && e.code === 'KeyN',
    run: (c) => c.newTab(),
  },
  {
    keys: ['Alt', 'w'],
    description: 'Close tab',
    group: 'assistant',
    available: assistantAvailable,
    match: (e) => e.altKey && !e.metaKey && !e.ctrlKey && e.code === 'KeyW',
    run: (c) => c.closeTab(),
  },
  {
    keys: ['Alt', 'm'],
    description: 'Minimize to dock',
    group: 'assistant',
    available: assistantAvailable,
    match: (e) => e.altKey && !e.metaKey && !e.ctrlKey && e.code === 'KeyM',
    run: (c) => c.minimize(),
  },
  {
    keys: ['Alt', 'p'],
    description: 'Pop out / dock the panel',
    group: 'assistant',
    available: () =>
      assistantAvailable() &&
      typeof window !== 'undefined' &&
      !!window.matchMedia?.('(min-width: 40rem)').matches,
    match: (e) => e.altKey && !e.metaKey && !e.ctrlKey && e.code === 'KeyP',
    run: (c) => {
      if (c.canPopOut) c.togglePopOut()
    },
  },
]

export function dispatchPanelShortcut(e: ReactKeyboardEvent, ctx: PanelCtx): boolean {
  for (const s of panelShortcuts) {
    if (!s.match(e)) continue
    if (s.available && !s.available()) continue
    e.stopPropagation()
    // Escape has no default to cancel and the panel is not a form, so only the
    // combos that a browser binds get preventDefault.
    if (e.key !== 'Escape') e.preventDefault()
    s.run(ctx)
    return true
  }
  return false
}

// ── The modal's view of all of it ───────────────────────────────────────────

export const shortcutGroupTitles: Record<ShortcutGroupId, string> = {
  navigation: 'Navigation',
  list: 'Lists',
  actions: 'Actions',
  assistant: 'Assistant',
}

const allRows: ShortcutRow[] = [...globalShortcuts, ...panelShortcuts]

export function isShortcutAvailable(s: ShortcutRow): boolean {
  if (s.needs && !shortcutAction(s.needs)) return false
  if (s.available && !s.available()) return false
  return true
}

/** The rows worth showing for a group right now. Empty groups are not rendered. */
export function visibleShortcuts(group: ShortcutGroupId): ShortcutRow[] {
  return allRows.filter((s) => s.group === group && isShortcutAvailable(s))
}

export function useKeyboardShortcuts() {
  const navigate = useNavigate()
  const keySequence = useAppStore((state) => state.keySequence)
  const addToSequence = useAppStore((state) => state.addToSequence)
  const clearSequence = useAppStore((state) => state.clearSequence)

  const handleKeyDown = useCallback(
    (event: KeyboardEvent) => {
      const target = event.target as HTMLElement | null
      const isEditing =
        !!target &&
        (target.tagName === 'INPUT' ||
          target.tagName === 'TEXTAREA' ||
          target.isContentEditable ||
          !!target.closest?.('[role="textbox"]'))

      const key = event.key.toLowerCase()
      const ctx = { navigate }

      // Escape always abandons a half-typed sequence, typing or not.
      if (key === 'escape' && keySequence.length > 0) clearSequence()

      // A pending sequence owns the next BARE letter. Without this the
      // single-press handler for that letter runs first and the sequence never
      // resolves, which is exactly how `g k` lost the API keys route to `k`.
      // Modifiers are excluded or `g` followed within half a second by Ctrl+K
      // would navigate instead of opening the command palette.
      const bareLetter =
        /^[a-z]$/.test(key) &&
        !event.ctrlKey &&
        !event.metaKey &&
        !event.altKey &&
        !event.shiftKey
      if (!isEditing && keySequence.length > 0 && bareLetter) {
        event.preventDefault()
        const seq = [...keySequence, key].join(',')
        clearSequence()
        const hit = globalShortcuts.find((s) => s.sequence?.join(',') === seq)
        if (hit && isShortcutAvailable(hit)) hit.run(ctx)
        return
      }

      for (const s of globalShortcuts) {
        if (!s.match) continue
        if (isEditing && !s.whileTyping) continue
        if (!s.match(event)) continue
        // An unavailable shortcut falls through rather than swallowing the key:
        // `/` with no search box on screen should type nothing, not nothing at
        // all costs.
        if (!isShortcutAvailable(s)) continue
        event.preventDefault()
        // A shortcut that fires mid-sequence ends the sequence; leaving it
        // pending would hand the next letter to a `g` the user has moved on
        // from.
        if (keySequence.length > 0) clearSequence()
        s.run(ctx)
        return
      }

      if (isEditing) return
      if (
        sequenceStarters.has(key) &&
        !event.shiftKey &&
        !event.altKey &&
        !event.ctrlKey &&
        !event.metaKey
      ) {
        event.preventDefault()
        addToSequence(key)
      }
    },
    [keySequence, addToSequence, clearSequence, navigate],
  )

  useEffect(() => {
    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [handleKeyDown])
}
