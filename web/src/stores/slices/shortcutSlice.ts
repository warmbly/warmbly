import type { StateCreator } from 'zustand'

export interface ShortcutSlice {
  // Sequence state for multi-key shortcuts (e.g., g + e)
  keySequence: string[]
  sequenceTimeout: ReturnType<typeof setTimeout> | null

  // List navigation state
  selectedIndex: number
  listLength: number

  // Actions
  addToSequence: (key: string) => void
  clearSequence: () => void
  setSelectedIndex: (index: number) => void
  setListLength: (length: number) => void
  moveSelection: (direction: 'up' | 'down' | 'first' | 'last') => void
}

const SEQUENCE_TIMEOUT = 500 // ms

export const createShortcutSlice: StateCreator<ShortcutSlice, [], [], ShortcutSlice> = (set, get) => ({
  keySequence: [],
  sequenceTimeout: null,
  selectedIndex: -1,
  listLength: 0,

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

  setSelectedIndex: (selectedIndex) => set({ selectedIndex }),

  setListLength: (listLength) => set({ listLength }),

  moveSelection: (direction) => {
    const { selectedIndex, listLength } = get()

    if (listLength === 0) return

    let newIndex: number
    switch (direction) {
      case 'up':
        newIndex = selectedIndex <= 0 ? listLength - 1 : selectedIndex - 1
        break
      case 'down':
        newIndex = selectedIndex >= listLength - 1 ? 0 : selectedIndex + 1
        break
      case 'first':
        newIndex = 0
        break
      case 'last':
        newIndex = listLength - 1
        break
    }

    set({ selectedIndex: newIndex })
  },
})
