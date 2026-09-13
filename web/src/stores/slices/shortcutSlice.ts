import type { StateCreator } from 'zustand'

// Multi-key sequences only (g then a letter). The selection state that used to
// live here — selectedIndex / listLength / moveSelection — was written by
// nobody and read by nobody, which is what made j, k, gg and G dead keys: the
// dispatcher called moveSelection and it returned on `listLength === 0` every
// time. A screen now offers list navigation through useShortcutActions, where
// the list that owns the rows also owns their state.
export interface ShortcutSlice {
  keySequence: string[]
  sequenceTimeout: ReturnType<typeof setTimeout> | null

  addToSequence: (key: string) => void
  clearSequence: () => void
}

const SEQUENCE_TIMEOUT = 500 // ms

export const createShortcutSlice: StateCreator<ShortcutSlice, [], [], ShortcutSlice> = (set, get) => ({
  keySequence: [],
  sequenceTimeout: null,

  addToSequence: (key) => {
    const { sequenceTimeout } = get()

    // Clear existing timeout
    if (sequenceTimeout) {
      clearTimeout(sequenceTimeout)
    }

    // Add key to sequence
    set((state) => ({ keySequence: [...state.keySequence, key] }))

    // Set new timeout to clear sequence
    const timeout = setTimeout(() => {
      set({ keySequence: [], sequenceTimeout: null })
    }, SEQUENCE_TIMEOUT)

    set({ sequenceTimeout: timeout })
  },

  clearSequence: () => {
    const { sequenceTimeout } = get()
    if (sequenceTimeout) {
      clearTimeout(sequenceTimeout)
    }
    set({ keySequence: [], sequenceTimeout: null })
  },
})
