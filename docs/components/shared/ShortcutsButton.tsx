'use client';

import { Keyboard } from 'lucide-react';
import { useKeyboardShortcutsContext } from './KeyboardShortcutsProvider';

export function ShortcutsButton() {
  const { openShortcuts } = useKeyboardShortcutsContext();

  return (
    <button
      onClick={openShortcuts}
      className="inline-flex h-8 w-8 items-center justify-center rounded-md text-fd-muted-foreground hover:bg-fd-accent hover:text-fd-accent-foreground transition-colors"
      title="Keyboard shortcuts (?)"
    >
      <Keyboard className="h-4 w-4" />
    </button>
  );
}
