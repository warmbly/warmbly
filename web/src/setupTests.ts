import '@testing-library/jest-dom'
import { MotionGlobalConfig } from 'framer-motion'
import { vi } from 'vitest'

// Animations finish instantly, so no test waits on wall-clock transitions.
MotionGlobalConfig.skipAnimations = true

if (typeof window !== 'undefined') {
  // Mock matchMedia for tests
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: vi.fn().mockImplementation((query: string) => ({
      matches: false,
      media: query,
      onchange: null,
      addListener: vi.fn(),
      removeListener: vi.fn(),
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      dispatchEvent: vi.fn(),
    })),
  })

  // Mock localStorage
  const localStorageMock = {
    getItem: vi.fn(),
    setItem: vi.fn(),
    removeItem: vi.fn(),
    clear: vi.fn(),
  }
  Object.defineProperty(window, 'localStorage', { value: localStorageMock })

  // Mock ResizeObserver
  class ResizeObserverMock {
    observe = vi.fn()
    unobserve = vi.fn()
    disconnect = vi.fn()
  }
  window.ResizeObserver = ResizeObserverMock
}
