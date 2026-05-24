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
      // Clicked inside the main element → ignore
      if (!ref.current || ref.current.contains(event.target as Node)) return;

      handler();
    };

    document.addEventListener('mousedown', listener);
    document.addEventListener('touchstart', listener);

    return () => {
      document.removeEventListener('mousedown', listener);
      document.removeEventListener('touchstart', listener);
    };
  }, [ref, handler]);
}