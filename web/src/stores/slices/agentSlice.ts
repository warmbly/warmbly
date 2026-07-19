import type { StateCreator } from 'zustand'

// AI assistant workspace state. Multiple conversations run as tabs; each tab
// owns its own transcript, pending approval, and run state, so a background run
// keeps streaming into its tab even while the user reads another. Kept in the
// store (not component state) so tabs survive the panel closing/reopening.
// Deliberately NOT persisted: reopening a past conversation rehydrates from the
// server transcript, so there is nothing worth writing to localStorage.

export type AgentToolStep = {
  id: string
  tool: string
  argsSummary?: string
  result?: string
  done: boolean
  entityType?: string
  entityId?: string
  openURL?: string
}

export type AgentPending = {
  toolCallId: string
  tool: string
  risk: string
  argsSummary?: string
}

export type AgentBlock =
  | { kind: 'text'; text: string }
  | { kind: 'tool'; step: AgentToolStep }
  | { kind: 'error'; code?: string; message: string }

export type AgentTurn = {
  id: string
  role: 'user' | 'assistant'
  blocks: AgentBlock[]
}

export type AgentTab = {
  key: string
  sessionId: string | null
  title: string
  turns: AgentTurn[]
  pending: AgentPending | null
  running: boolean
  // Unsent composer text, kept per tab so switching tabs never leaks input.
  draft: string
  // A run finished while the user wasn't looking at this tab (panel closed,
  // minimized, or another tab active). Drives the tab dot, the dock status,
  // and the header-icon badge; cleared the moment the tab is actually viewed.
  unseen: boolean
  // hydrated is false for a tab opened from history until its transcript loads.
  hydrated: boolean
  credits: number | null
  budget: number
  iteration: number
  // True when this conversation is running on a free/local model (AI_LOCAL_MODEL):
  // the composer shows a "free model" notice and no credits are charged.
  freeModel: boolean
}

let tabSeq = 0
const newTabKey = () => `tab_${++tabSeq}`

export const AGENT_MIN_WIDTH = 360
export const AGENT_MAX_WIDTH = 720
export const AGENT_DEFAULT_WIDTH = 440

// Floating-window geometry bounds (desktop only; mobile stays a sheet).
export const AGENT_FLOAT_MIN_W = 360
export const AGENT_FLOAT_MAX_W = 920
export const AGENT_FLOAT_MIN_H = 400

export type AgentFloatRect = { x: number; y: number; w: number; h: number }

function makeTab(partial?: Partial<AgentTab>): AgentTab {
  return {
    key: newTabKey(),
    sessionId: null,
    title: 'New chat',
    turns: [],
    pending: null,
    running: false,
    draft: '',
    unseen: false,
    hydrated: true,
    credits: null,
    budget: 20,
    iteration: 0,
    freeModel: false,
    ...partial,
  }
}

export interface AgentSlice {
  agentTabs: AgentTab[]
  agentActiveKey: string | null
  agentExpanded: boolean
  // Minimized docks the open panel into a compact status bar; runs keep
  // streaming and the dock mirrors their state.
  agentMinimized: boolean
  // Layout preferences, persisted: which screen edge the panel docks to and
  // its width in the normal (non-expanded) mode.
  agentSide: 'left' | 'right'
  agentWidth: number
  // Floating mode: the panel detaches into a movable, resizable window.
  // agentSide is kept as the dock-back target. Rect is null until the first
  // pop-out (the component computes a default from the viewport).
  agentFloating: boolean
  agentFloatRect: AgentFloatRect | null

  setAgentExpanded: (v: boolean) => void
  setAgentMinimized: (v: boolean) => void
  setAgentSide: (v: 'left' | 'right') => void
  // Clamped to [AGENT_MIN_WIDTH, AGENT_MAX_WIDTH].
  setAgentWidth: (v: number) => void
  setAgentFloating: (v: boolean) => void
  // Size is clamped to the float bounds; position clamping is the caller's
  // job (it knows the viewport).
  setAgentFloatRect: (r: AgentFloatRect) => void
  // Ensure at least one tab exists (called when the panel first opens).
  agentEnsureTab: () => void
  // Open a brand-new empty conversation tab and focus it.
  agentNewTab: () => void
  // Focus the tab for an existing session, creating an (unhydrated) tab if none
  // is open for it yet.
  agentOpenSession: (sessionId: string, title: string) => void
  agentCloseTab: (key: string) => void
  agentSetActive: (key: string) => void
  // Patch a tab by key (no-op if it was closed mid-run).
  agentPatchTab: (key: string, patch: Partial<AgentTab>) => void
  // Functional update of a tab's fields (used by the stream handler).
  agentUpdateTab: (key: string, fn: (t: AgentTab) => AgentTab) => void
}

export const createAgentSlice: StateCreator<AgentSlice, [], [], AgentSlice> = (set, get) => ({
  agentTabs: [],
  agentActiveKey: null,
  agentExpanded: false,
  agentMinimized: false,
  agentSide: 'right',
  agentWidth: AGENT_DEFAULT_WIDTH,
  agentFloating: false,
  agentFloatRect: null,

  setAgentExpanded: (agentExpanded) => set({ agentExpanded }),
  setAgentMinimized: (agentMinimized) => set({ agentMinimized }),
  setAgentSide: (agentSide) => set({ agentSide }),
  setAgentWidth: (v) =>
    set({ agentWidth: Math.min(AGENT_MAX_WIDTH, Math.max(AGENT_MIN_WIDTH, Math.round(v))) }),
  setAgentFloating: (agentFloating) => set({ agentFloating }),
  setAgentFloatRect: (r) =>
    set({
      agentFloatRect: {
        x: Math.round(r.x),
        y: Math.round(r.y),
        w: Math.min(AGENT_FLOAT_MAX_W, Math.max(AGENT_FLOAT_MIN_W, Math.round(r.w))),
        h: Math.max(AGENT_FLOAT_MIN_H, Math.round(r.h)),
      },
    }),

  agentEnsureTab: () => {
    if (get().agentTabs.length > 0) return
    const tab = makeTab()
    set({ agentTabs: [tab], agentActiveKey: tab.key })
  },

  agentNewTab: () => {
    const tab = makeTab()
    set((s) => ({ agentTabs: [...s.agentTabs, tab], agentActiveKey: tab.key }))
  },

  agentOpenSession: (sessionId, title) => {
    const existing = get().agentTabs.find((t) => t.sessionId === sessionId)
    if (existing) {
      set({ agentActiveKey: existing.key })
      return
    }
    const tab = makeTab({ sessionId, title, hydrated: false })
    set((s) => ({ agentTabs: [...s.agentTabs, tab], agentActiveKey: tab.key }))
  },

  agentCloseTab: (key) =>
    set((s) => {
      const idx = s.agentTabs.findIndex((t) => t.key === key)
      if (idx === -1) return s
      const tabs = s.agentTabs.filter((t) => t.key !== key)
      // Closing the last tab starts a fresh one so the panel is never blank.
      if (tabs.length === 0) {
        const tab = makeTab()
        return { agentTabs: [tab], agentActiveKey: tab.key }
      }
      let active = s.agentActiveKey
      if (active === key) {
        const neighbor = tabs[idx] ?? tabs[idx - 1] ?? tabs[0]
        active = neighbor ? neighbor.key : null
      }
      return { agentTabs: tabs, agentActiveKey: active }
    }),

  agentSetActive: (agentActiveKey) => set({ agentActiveKey }),

  agentPatchTab: (key, patch) =>
    set((s) => ({
      agentTabs: s.agentTabs.map((t) => (t.key === key ? { ...t, ...patch } : t)),
    })),

  agentUpdateTab: (key, fn) =>
    set((s) => ({
      agentTabs: s.agentTabs.map((t) => (t.key === key ? fn(t) : t)),
    })),
})
