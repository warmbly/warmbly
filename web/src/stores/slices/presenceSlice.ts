import type { StateCreator } from 'zustand'

// One tracked socket for a user. A user with two tabs has two metas under the
// same user id; phx_ref identifies each socket for diff reconciliation.
export interface PresenceMeta {
  phx_ref?: string
  online_at?: number
  name?: string | null
  avatar?: string | null
  /** Current dashboard route, e.g. "/app/unibox". */
  page?: string | null
  /** Focused record, e.g. "thread:<id>" / "automation:<id>" / "contact:<id>". */
  resource?: string | null
  /** "viewing" | "editing" | "replying" | "idle" */
  action?: string | null
  updated_at?: number
}

type PresenceMap = Record<string, PresenceMeta[]>

interface PresencePayloadEntry {
  metas?: PresenceMeta[]
}

export interface PresenceSlice {
  /** user id -> live metas (one per open socket). Empty when offline/unknown. */
  presence: PresenceMap

  setPresenceState: (state: Record<string, PresencePayloadEntry>) => void
  applyPresenceDiff: (diff: {
    joins?: Record<string, PresencePayloadEntry>
    leaves?: Record<string, PresencePayloadEntry>
  }) => void
  clearPresence: () => void
}

const toMap = (state: Record<string, PresencePayloadEntry>): PresenceMap => {
  const next: PresenceMap = {}
  for (const [userId, entry] of Object.entries(state ?? {})) {
    if (entry?.metas?.length) next[userId] = entry.metas
  }
  return next
}

export const createPresenceSlice: StateCreator<PresenceSlice, [], [], PresenceSlice> = (set) => ({
  presence: {},

  setPresenceState: (state) => set({ presence: toMap(state) }),

  // Phoenix presence semantics: joins append/replace metas by phx_ref,
  // leaves remove the matching refs; a user with no metas left is offline.
  applyPresenceDiff: (diff) =>
    set((current) => {
      const next: PresenceMap = { ...current.presence }

      for (const [userId, entry] of Object.entries(diff?.joins ?? {})) {
        const joining = entry?.metas ?? []
        if (!joining.length) continue
        const existing = next[userId] ?? []
        const joinedRefs = new Set(joining.map((m) => m.phx_ref))
        next[userId] = [...existing.filter((m) => !joinedRefs.has(m.phx_ref)), ...joining]
      }

      for (const [userId, entry] of Object.entries(diff?.leaves ?? {})) {
        const leavingRefs = new Set((entry?.metas ?? []).map((m) => m.phx_ref))
        const remaining = (next[userId] ?? []).filter((m) => !leavingRefs.has(m.phx_ref))
        if (remaining.length) {
          next[userId] = remaining
        } else {
          delete next[userId]
        }
      }

      return { presence: next }
    }),

  clearPresence: () => set({ presence: {} }),
})
