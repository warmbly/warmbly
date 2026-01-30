'use client';

import { createContext, useContext, useState, useCallback, type ReactNode } from 'react';
import { useKeyboardShortcuts } from '@/hooks/useKeyboardShortcuts';
import { ShortcutsModal } from './ShortcutsModal';

interface KeyboardShortcutsContextValue {
  openShortcuts: () => void;
  openSearch: () => void;
}

const KeyboardShortcutsContext = createContext<KeyboardShortcutsContextValue | null>(null);

export function useKeyboardShortcutsContext() {
  const context = useContext(KeyboardShortcutsContext);
  if (!context) {
    throw new Error('useKeyboardShortcutsContext must be used within KeyboardShortcutsProvider');
  }
  return context;
}

export function KeyboardShortcutsProvider({ children }: { children: ReactNode }) {
  const [shortcutsOpen, setShortcutsOpen] = useState(false);

  const openShortcuts = useCallback(() => {
    setShortcutsOpen(true);
  }, []);

  const openSearch = useCallback(() => {
    const searchButton = document.querySelector('[data-search]') as HTMLButtonElement;
    if (searchButton) {
      searchButton.click();
    }
  }, []);

  useKeyboardShortcuts({
    onOpenShortcuts: openShortcuts,
    onOpenSearch: openSearch,
  });

  return (
    <KeyboardShortcutsContext.Provider value={{ openShortcuts, openSearch }}>
      {children}
      <ShortcutsModal open={shortcutsOpen} onOpenChange={setShortcutsOpen} />
    </KeyboardShortcutsContext.Provider>
  );
}
