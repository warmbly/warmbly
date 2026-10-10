'use client';
import { Children, type MouseEvent, type ReactNode, useEffect, useRef } from 'react';
import { ChevronDown } from 'lucide-react';

// A section of detail, closed until asked for. The first child (the section's
// heading) is the toggle and keeps its id, so the table of contents, search
// results and #links still land on it; following one opens the fold first.
export function Fold({ children }: { children: ReactNode }) {
  const ref = useRef<HTMLDetailsElement>(null);
  const [head, ...rest] = Children.toArray(children);

  useEffect(() => {
    const reveal = () => {
      const id = decodeURIComponent(location.hash.slice(1));
      const el = id ? document.getElementById(id) : null;
      if (!el || !ref.current?.contains(el)) return;
      ref.current.open = true;
      requestAnimationFrame(() => el.scrollIntoView());
    };
    reveal();
    window.addEventListener('hashchange', reveal);
    return () => window.removeEventListener('hashchange', reveal);
  }, []);

  // The heading's own anchor would only jump to it; a click toggles the fold instead.
  const toggle = (e: MouseEvent) => {
    e.preventDefault();
    const d = ref.current;
    if (!d) return;
    d.open = !d.open;
    const id = d.querySelector('summary [id]')?.id;
    if (d.open && id) history.replaceState(null, '', `#${id}`);
  };

  return (
    <details ref={ref} className="wd-fold group">
      <summary onClick={toggle}>
        <div className="min-w-0 flex-1">{head}</div>
        <ChevronDown aria-hidden="true" className="wd-fold-chevron size-4 shrink-0 transition-transform group-open:rotate-180" />
      </summary>
      <div className="wd-fold-body">{rest}</div>
    </details>
  );
}
