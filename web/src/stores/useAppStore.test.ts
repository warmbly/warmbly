import { describe, it, expect, beforeEach, afterEach } from 'vitest'
import { useAppStore } from './useAppStore'
import {
  UNIBOX_LIST_DEFAULT_WIDTH,
  UNIBOX_LIST_MAX_WIDTH,
  UNIBOX_LIST_MIN_WIDTH,
} from './slices/uiSlice'

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
      navCollapsed: false,
      sidebarMobileOpen: false,
      tagsModalOpen: false,
      foldersModalOpen: false,
      addEmailModalOpen: false,
      shortcutsModalOpen: false,
      commandPaletteOpen: false,
      uniboxListWidth: UNIBOX_LIST_DEFAULT_WIDTH,
      uniboxContactRailOpen: true,
      campaigns: [],
      emails: [],
      tags: [],
      folders: [],
      categories: [],
    })
  })

  describe('UI Slice', () => {
    it('should toggle sidebar', () => {
      const { toggleSidebar, navCollapsed } = useAppStore.getState()
      expect(navCollapsed).toBe(false)

      toggleSidebar()
      expect(useAppStore.getState().navCollapsed).toBe(true)

      toggleSidebar()
      expect(useAppStore.getState().navCollapsed).toBe(false)
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

    it('clamps the unibox list width to its bounds', () => {
      const { setUniboxListWidth } = useAppStore.getState()

      setUniboxListWidth(440)
      expect(useAppStore.getState().uniboxListWidth).toBe(440)

      // A drag past either end parks at the bound instead of collapsing the
      // list or crushing the thread pane.
      setUniboxListWidth(10)
      expect(useAppStore.getState().uniboxListWidth).toBe(UNIBOX_LIST_MIN_WIDTH)

      setUniboxListWidth(5000)
      expect(useAppStore.getState().uniboxListWidth).toBe(UNIBOX_LIST_MAX_WIDTH)

      // Sub-pixel pointer coordinates must not reach the DOM as fractions.
      setUniboxListWidth(400.6)
      expect(useAppStore.getState().uniboxListWidth).toBe(401)

      // A non-finite value falls back rather than rendering `width: NaNpx`.
      setUniboxListWidth(Number.NaN)
      expect(useAppStore.getState().uniboxListWidth).toBe(UNIBOX_LIST_DEFAULT_WIDTH)
    })

    // Rehydration does NOT go through the setters: zustand merges the stored
    // object into state directly, so the clamp above proves nothing about what
    // localStorage can put on screen. These drive the real persist path.
    describe('rehydration', () => {
      const original = useAppStore.persist.getOptions().storage

      const rehydrateFrom = async (value: unknown) => {
        useAppStore.persist.setOptions({
          storage: {
            getItem: () => value as never,
            setItem: () => {},
            removeItem: () => {},
          },
        })
        await useAppStore.persist.rehydrate()
      }

      afterEach(() => {
        useAppStore.persist.setOptions({ storage: original })
      })

      it('clamps a stored width that is out of range or not a number', async () => {
        await rehydrateFrom({ state: { uniboxListWidth: 99999 } })
        expect(useAppStore.getState().uniboxListWidth).toBe(UNIBOX_LIST_MAX_WIDTH)

        await rehydrateFrom({ state: { uniboxListWidth: 4 } })
        expect(useAppStore.getState().uniboxListWidth).toBe(UNIBOX_LIST_MIN_WIDTH)

        await rehydrateFrom({ state: { uniboxListWidth: null } })
        expect(useAppStore.getState().uniboxListWidth).toBe(UNIBOX_LIST_DEFAULT_WIDTH)
      })

      it('ignores the old key that `b` filled in while nothing rendered it', async () => {
        // A store written before this feature carries `sidebarCollapsed: true`
        // for a keystroke the user does not remember, and no version field at
        // all — so zustand would not have run a migrate even if one existed
        // (it only migrates when the stored version is a number). The new key
        // sidesteps the whole problem: it is simply absent, so the nav opens
        // expanded.
        await rehydrateFrom({ state: { sidebarCollapsed: true } })
        expect(useAppStore.getState().navCollapsed).toBe(false)

        // A collapse made deliberately since then is honoured.
        await rehydrateFrom({ state: { navCollapsed: true } })
        expect(useAppStore.getState().navCollapsed).toBe(true)
      })
    })

    it('remembers the unibox contact rail toggle', () => {
      const { setUniboxContactRailOpen } = useAppStore.getState()
      expect(useAppStore.getState().uniboxContactRailOpen).toBe(true)

      setUniboxContactRailOpen(false)
      expect(useAppStore.getState().uniboxContactRailOpen).toBe(false)

      setUniboxContactRailOpen(true)
      expect(useAppStore.getState().uniboxContactRailOpen).toBe(true)
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
