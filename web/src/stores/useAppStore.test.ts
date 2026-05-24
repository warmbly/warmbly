import { describe, it, expect, beforeEach } from 'vitest'
import { useAppStore } from './useAppStore'

describe('useAppStore', () => {
  beforeEach(() => {
    // Reset store to initial state before each test
    useAppStore.setState({
      user: null,
      access: null,
      timezones: [],
      isAuthenticated: false,
      isLoading: true,
      theme: 'system',
      sidebarCollapsed: false,
      sidebarMobileOpen: false,
      tagsModalOpen: false,
      foldersModalOpen: false,
      addEmailModalOpen: false,
      shortcutsModalOpen: false,
      commandPaletteOpen: false,
      campaigns: [],
      emails: [],
      tags: [],
      folders: [],
      categories: [],
    })
  })

  describe('UI Slice', () => {
    it('should toggle sidebar', () => {
      const { toggleSidebar, sidebarCollapsed } = useAppStore.getState()
      expect(sidebarCollapsed).toBe(false)

      toggleSidebar()
      expect(useAppStore.getState().sidebarCollapsed).toBe(true)

      toggleSidebar()
      expect(useAppStore.getState().sidebarCollapsed).toBe(false)
    })

    it('should set theme', () => {
      const { setTheme } = useAppStore.getState()

      setTheme('dark')
      expect(useAppStore.getState().theme).toBe('dark')

      setTheme('light')
      expect(useAppStore.getState().theme).toBe('light')
    })

    it('should open and close modals', () => {
      const { setShortcutsModalOpen, setCommandPaletteOpen } = useAppStore.getState()

      setShortcutsModalOpen(true)
      expect(useAppStore.getState().shortcutsModalOpen).toBe(true)

      setShortcutsModalOpen(false)
      expect(useAppStore.getState().shortcutsModalOpen).toBe(false)

      setCommandPaletteOpen(true)
      expect(useAppStore.getState().commandPaletteOpen).toBe(true)
    })
  })

  describe('User Slice', () => {
    it('should set user and update isAuthenticated', () => {
      const { setUser } = useAppStore.getState()

      const mockUser = {
        id: 'user-1',
        first_name: 'Test',
        last_name: 'User',
        email: 'test@example.com',
        referral_source: '',
        onboarding_completed_at: null,
        tags: [],
        categories: [],
        folders: [],
        roles: ['member'],
        updated_at: new Date(),
        created_at: new Date(),
      }

      setUser(mockUser)
      expect(useAppStore.getState().user).toEqual(mockUser)
      expect(useAppStore.getState().isAuthenticated).toBe(true)
    })

    it('should logout and clear user', () => {
      const { setUser, logout } = useAppStore.getState()

      setUser({
        id: 'user-2',
        first_name: 'Test',
        last_name: 'User',
        email: 'test@example.com',
        referral_source: '',
        onboarding_completed_at: null,
        tags: [],
        categories: [],
        folders: [],
        roles: [],
        updated_at: new Date(),
        created_at: new Date(),
      })

      logout()
      expect(useAppStore.getState().user).toBeNull()
      expect(useAppStore.getState().isAuthenticated).toBe(false)
    })
  })

  describe('Shortcut Slice', () => {
    it('should add keys to sequence', () => {
      const { addToSequence, keySequence } = useAppStore.getState()
      expect(keySequence).toEqual([])

      addToSequence('g')
      expect(useAppStore.getState().keySequence).toEqual(['g'])

      addToSequence('e')
      expect(useAppStore.getState().keySequence).toEqual(['g', 'e'])
    })

    it('should clear sequence', () => {
      const { addToSequence, clearSequence } = useAppStore.getState()

      addToSequence('g')
      addToSequence('e')
      clearSequence()

      expect(useAppStore.getState().keySequence).toEqual([])
    })

    it('should move selection', () => {
      const { setListLength, setSelectedIndex, moveSelection } = useAppStore.getState()

      setListLength(5)
      setSelectedIndex(0)

      moveSelection('down')
      expect(useAppStore.getState().selectedIndex).toBe(1)

      moveSelection('up')
      expect(useAppStore.getState().selectedIndex).toBe(0)

      moveSelection('last')
      expect(useAppStore.getState().selectedIndex).toBe(4)

      moveSelection('first')
      expect(useAppStore.getState().selectedIndex).toBe(0)
    })
  })

  describe('Data Slice', () => {
    it('should set campaigns', () => {
      const { setCampaigns } = useAppStore.getState()
      const mockCampaigns = [{ id: '1', name: 'Test', status: 'active' }]

      setCampaigns(mockCampaigns as never)
      expect(useAppStore.getState().campaigns).toEqual(mockCampaigns)
    })

    it('should set emails', () => {
      const { setEmails } = useAppStore.getState()
      const mockEmails = [{ id: '2', email: 'test@example.com' }]

      setEmails(mockEmails as never)
      expect(useAppStore.getState().emails).toEqual(mockEmails)
    })

    it('should add and remove tags', () => {
      const { addTag, removeTag } = useAppStore.getState()
      const mockTag = { id: '1', title: 'Test', color: 'blue', position: 0, updated_at: new Date(), created_at: new Date() }

      addTag(mockTag)
      expect(useAppStore.getState().tags).toContainEqual(mockTag)

      removeTag('1')
      expect(useAppStore.getState().tags).not.toContainEqual(mockTag)
    })
  })
})
