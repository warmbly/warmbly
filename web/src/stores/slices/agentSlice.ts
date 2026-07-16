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

function makeTab(partial?: Partial<AgentTab>): AgentTab {
  return {
    key: newTabKey(),
    sessionId: null,
    title: 'New chat',
    turns: [],
    pending: null,
    running: false,
    draft: '',
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

  setAgentExpanded: (v: boolean) => void
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

  setAgentExpanded: (agentExpanded) => set({ agentExpanded }),

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
