'use client';

import { useEffect, useCallback, useRef } from 'react';
import { useRouter } from 'next/navigation';

export interface ShortcutDefinition {
  keys: string[];
  description: string;
}

interface UseKeyboardShortcutsOptions {
  onOpenShortcuts: () => void;
  onOpenSearch: () => void;
}

export function useKeyboardShortcuts({ onOpenShortcuts, onOpenSearch }: UseKeyboardShortcutsOptions) {
  const router = useRouter();
  const keySequenceRef = useRef<string[]>([]);
  const sequenceTimeoutRef = useRef<NodeJS.Timeout | null>(null);

  const clearSequence = useCallback(() => {
    keySequenceRef.current = [];
    if (sequenceTimeoutRef.current) {
      clearTimeout(sequenceTimeoutRef.current);
      sequenceTimeoutRef.current = null;
    }
  }, []);

  const addToSequence = useCallback((key: string) => {
    keySequenceRef.current = [...keySequenceRef.current, key];

    if (sequenceTimeoutRef.current) {
      clearTimeout(sequenceTimeoutRef.current);
    }
    sequenceTimeoutRef.current = setTimeout(() => {
      clearSequence();
    }, 1500);
  }, [clearSequence]);

  const navigationShortcuts: Record<string, string> = {
    'g,h': '/guides/mailboxes',
    'g,a': '/api',
    'g,p': '/api/permissions',
    'g,e': '/api/error-codes',
  };

  const handleKeyDown = useCallback(
    (event: KeyboardEvent) => {
      const target = event.target as HTMLElement;
      const isEditing =
        target.tagName === 'INPUT' ||
        target.tagName === 'TEXTAREA' ||
        target.isContentEditable ||
        target.closest('[role="textbox"]');

      if (isEditing) return;

      const key = event.key.toLowerCase();

      if ((event.ctrlKey || event.metaKey) && key === 'k') {
        event.preventDefault();
        onOpenSearch();
        return;
      }

      if (key === 'escape') {
        clearSequence();
        return;
      }

      if (key === '?' || (event.shiftKey && key === '/')) {
        event.preventDefault();
        onOpenShortcuts();
        return;
      }

      if (key === '/') {
        event.preventDefault();
        onOpenSearch();
        return;
      }

      if (key === 'j') {
        event.preventDefault();
        window.scrollBy({ top: 100, behavior: 'smooth' });
        return;
      }
      if (key === 'k') {
        event.preventDefault();
        window.scrollBy({ top: -100, behavior: 'smooth' });
        return;
      }

      if (event.shiftKey && key === 'g') {
        event.preventDefault();
        window.scrollTo({ top: document.body.scrollHeight, behavior: 'smooth' });
        return;
      }

      if (key === 'g' && keySequenceRef.current.length === 1 && keySequenceRef.current[0] === 'g') {
        event.preventDefault();
        window.scrollTo({ top: 0, behavior: 'smooth' });
        clearSequence();
        return;
      }

      if (key.match(/^[a-z]$/)) {
        addToSequence(key);
        const sequence = keySequenceRef.current.join(',');

        const route = navigationShortcuts[sequence];
        if (route) {
          event.preventDefault();
          router.push(route);
          clearSequence();
        }
      }
    },
    [addToSequence, clearSequence, router, onOpenShortcuts, onOpenSearch]
  );

  useEffect(() => {
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [handleKeyDown]);
}

export const shortcutDefinitions = {
  navigation: [
    { keys: ['g', 'h'], description: 'Go to guides' },
    { keys: ['g', 'a'], description: 'Go to API reference' },
    { keys: ['g', 'p'], description: 'Go to permissions' },
    { keys: ['g', 'e'], description: 'Go to error codes' },
  ],
  scroll: [
    { keys: ['j'], description: 'Scroll down' },
    { keys: ['k'], description: 'Scroll up' },
    { keys: ['g', 'g'], description: 'Go to top' },
    { keys: ['G'], description: 'Go to bottom' },
  ],
  actions: [
    { keys: ['/'], description: 'Focus search' },
    { keys: ['?'], description: 'Show shortcuts' },
    { keys: ['Ctrl', 'K'], description: 'Open search' },
    { keys: ['Escape'], description: 'Close modal / Clear' },
  ],
};
