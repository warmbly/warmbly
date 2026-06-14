import type { RefObject} from 'react';
import { useEffect } from 'react';

/**
 * Calls handler when a click happens outside the element referenced by ref.
 * Optionally pass a second ref if the trigger button itself should not count as “outside”.
 */
export default function useClickOutside(
  ref: RefObject<HTMLElement | null>,
  handler: () => void,
) {
  useEffect(() => {
    const listener = (event: MouseEvent | TouchEvent) => {
      if (!ref.current || ref.current.contains(event.target as Node)) return;

      // Ignore clicks inside any portaled floating layer (a date-picker calendar,
      // a SelectMenu, a nested popover). Those render to <body>, outside `ref`, so
      // without this a click into a popover this element opened would dismiss it.
      const el = event.target as Element | null;
      if (el && typeof el.closest === "function" && el.closest("[data-floating]")) return;

      handler();
    };

    // Capture phase so we still fire inside containers that call
    // stopPropagation() on mousedown (e.g. the contact edit slide-over,
    // which stops bubbling so clicks inside the panel don't dismiss the
    // overlay). Bubble-phase listeners never reach document in that case.
    document.addEventListener('mousedown', listener, true);
    document.addEventListener('touchstart', listener, true);

    return () => {
      document.removeEventListener('mousedown', listener, true);
      document.removeEventListener('touchstart', listener, true);
    };
  }, [ref, handler]);
}