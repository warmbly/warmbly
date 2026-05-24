import { useEffect, useCallback } from 'react'
import { useNavigate } from 'react-router-dom'
import { useAppStore } from '@/stores'

export interface ShortcutDefinition {
  keys: string[]
  action: () => void
  description: string
  category: 'navigation' | 'list' | 'actions' | 'modal'
}

export function useKeyboardShortcuts() {
  const navigate = useNavigate()
  const keySequence = useAppStore((state) => state.keySequence)
  const addToSequence = useAppStore((state) => state.addToSequence)
  const clearSequence = useAppStore((state) => state.clearSequence)
  const moveSelection = useAppStore((state) => state.moveSelection)
  const setShortcutsModalOpen = useAppStore((state) => state.setShortcutsModalOpen)
  const setCommandPaletteOpen = useAppStore((state) => state.setCommandPaletteOpen)
  const toggleSidebar = useAppStore((state) => state.toggleSidebar)

  // Navigation shortcuts (g + key)
  const navigationShortcuts: Record<string, string> = {
    'g,e': '/app/emails',
    'g,c': '/app/contacts',
    'g,m': '/app/campaigns',
    'g,u': '/app/unibox',
    'g,a': '/app/analytics',
    'g,p': '/app/crm/pipelines',
    'g,d': '/app/crm/deals',
    'g,t': '/app/crm/tasks',
    'g,l': '/app/templates',
    'g,k': '/app/api-keys',
    'g,s': '/app/settings',
  }

  const handleKeyDown = useCallback(
    (event: KeyboardEvent) => {
      // Ignore if typing in an input, textarea, or contenteditable
      const target = event.target as HTMLElement
      const isEditing =
        target.tagName === 'INPUT' ||
        target.tagName === 'TEXTAREA' ||
        target.isContentEditable ||
        target.closest('[role="textbox"]')

      if (isEditing) return

      const key = event.key.toLowerCase()

      // Handle Ctrl/Cmd + K for command palette
      if ((event.ctrlKey || event.metaKey) && key === 'k') {
        event.preventDefault()
        setCommandPaletteOpen(true)
        return
      }

      // Handle Escape
      if (key === 'escape') {
        clearSequence()
        return
      }

      // Handle ? for shortcuts modal
      if (key === '?' || (event.shiftKey && key === '/')) {
        event.preventDefault()
        setShortcutsModalOpen(true)
        return
      }

      // Handle b for sidebar toggle
      if (key === 'b' && !event.ctrlKey && !event.metaKey) {
        event.preventDefault()
        toggleSidebar()
        return
      }

      // Handle / for search focus
      if (key === '/') {
        event.preventDefault()
        const searchInput = document.querySelector<HTMLInputElement>('[data-search-input]')
        searchInput?.focus()
        return
      }

      // List navigation (vim-style)
      if (key === 'j') {
        event.preventDefault()
        moveSelection('down')
        return
      }
      if (key === 'k') {
        event.preventDefault()
        moveSelection('up')
        return
      }
      if (key === 'g' && keySequence.length === 1 && keySequence[0] === 'g') {
        event.preventDefault()
        moveSelection('first')
        clearSequence()
        return
      }

      // Handle G (shift + g) for last item
      if (event.shiftKey && key === 'g') {
        event.preventDefault()
        moveSelection('last')
        return
      }

      // Build sequence for navigation shortcuts
      if (key.match(/^[a-z]$/)) {
        addToSequence(key)
        const sequence = [...keySequence, key].join(',')

        // Check if this sequence matches a navigation shortcut
        const route = navigationShortcuts[sequence]
        if (route) {
          event.preventDefault()
          navigate(route)
          clearSequence()
        }
      }
    },
    [
      keySequence,
      addToSequence,
      clearSequence,
      navigate,
      moveSelection,
      setShortcutsModalOpen,
      setCommandPaletteOpen,
      toggleSidebar,
      navigationShortcuts,
    ]
  )

  useEffect(() => {
    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [handleKeyDown])
}

// Export shortcut definitions for display in the modal
export const shortcutDefinitions = {
  navigation: [
    { keys: ['g', 'e'], description: 'Go to Email Accounts' },
    { keys: ['g', 'c'], description: 'Go to Contacts' },
    { keys: ['g', 'm'], description: 'Go to Campaigns' },
    { keys: ['g', 'u'], description: 'Go to Unibox' },
    { keys: ['g', 'a'], description: 'Go to Analytics' },
    { keys: ['g', 'p'], description: 'Go to Pipelines' },
    { keys: ['g', 'd'], description: 'Go to Deals' },
    { keys: ['g', 't'], description: 'Go to Tasks' },
    { keys: ['g', 'l'], description: 'Go to Templates' },
    { keys: ['g', 'k'], description: 'Go to API Keys' },
    { keys: ['g', 's'], description: 'Go to Settings' },
  ],
  list: [
    { keys: ['j'], description: 'Move down in list' },
    { keys: ['k'], description: 'Move up in list' },
    { keys: ['g', 'g'], description: 'Go to first item' },
    { keys: ['G'], description: 'Go to last item' },
    { keys: ['Enter'], description: 'Open selected item' },
    { keys: ['Escape'], description: 'Close modal / Deselect' },
    { keys: ['x'], description: 'Select/deselect item' },
  ],
  actions: [
    { keys: ['/'], description: 'Focus search' },
    { keys: ['n'], description: 'New item' },
    { keys: ['e'], description: 'Edit selected item' },
    { keys: ['b'], description: 'Toggle sidebar' },
    { keys: ['?'], description: 'Show shortcuts' },
    { keys: ['Ctrl', 'k'], description: 'Command palette' },
  ],
}
