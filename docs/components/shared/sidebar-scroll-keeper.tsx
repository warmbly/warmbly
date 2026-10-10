'use client';
import { usePathname } from 'next/navigation';
import { useEffect } from 'react';

// Clicking a page in the sidebar keeps the sidebar where it was. Fumadocs scrolls
// the new active item into view, which moves a list you were just reading. The
// position is kept for a moment in session storage, so it also survives a full
// page load on a static host.
const KEY = 'wd-sidebar-scroll';
const FRESH_MS = 5000;

function viewport(): HTMLElement | null {
  const root = document.getElementById('nd-sidebar');
  if (!root) return null;
  for (const el of root.querySelectorAll<HTMLElement>('*')) {
    const y = getComputedStyle(el).overflowY;
    if ((y === 'auto' || y === 'scroll') && el.scrollHeight > el.clientHeight) return el;
  }
  return null;
}

export function SidebarScrollKeeper() {
  const pathname = usePathname();

  useEffect(() => {
    const remember = (e: MouseEvent) => {
      const link = (e.target as Element | null)?.closest('#nd-sidebar a[href]');
      const el = link && viewport();
      if (el) sessionStorage.setItem(KEY, JSON.stringify({ top: el.scrollTop, at: Date.now() }));
    };
    document.addEventListener('click', remember, true);
    return () => document.removeEventListener('click', remember, true);
  }, []);

  useEffect(() => {
    let saved: { top: number; at: number } | null = null;
    try {
      saved = JSON.parse(sessionStorage.getItem(KEY) ?? 'null');
    } catch {}
    if (!saved || Date.now() - saved.at > FRESH_MS) return;
    // The active item scrolls itself into view after this render; put the list back after it.
    const restore = () => {
      const el = viewport();
      if (el && saved) el.scrollTop = saved.top;
    };
    restore();
    const frame = requestAnimationFrame(restore);
    const late = window.setTimeout(restore, 80);
    const done = window.setTimeout(() => sessionStorage.removeItem(KEY), 400);
    return () => {
      cancelAnimationFrame(frame);
      window.clearTimeout(late);
      window.clearTimeout(done);
    };
  }, [pathname]);

  return null;
}
