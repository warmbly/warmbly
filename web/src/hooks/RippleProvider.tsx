import { useEffect } from 'react';

export default function RippleProvider({ children }: { children: React.ReactNode }) {
  useRipple();
  return children
}


function useRipple() {
  useEffect(() => {
    // Extend HTMLElement to include our custom _ripple field

    type RippleElement = HTMLElement & { _ripple?: HTMLSpanElement | null };

    // e.target is not always an Element: a document-level mouseleave reports
    // the document itself, which has no classList. closest() walks for us.
    const rippleHost = (t: EventTarget | null): RippleElement | null =>
      t instanceof Element ? (t.closest('.ripple') as RippleElement | null) : null;

    const down = (e: MouseEvent) => {
      const target = rippleHost(e.target);
      if (!target) return;

      const rect = target.getBoundingClientRect();
      const ripple = document.createElement('span');

      const size = Math.max(rect.width, rect.height);
      const x = e.clientX - rect.left - size / 2;
      const y = e.clientY - rect.top - size / 2;

      ripple.style.left = `${x}px`;
      ripple.style.top = `${y}px`;
      ripple.style.width = `${size}px`;
      ripple.style.height = `${size}px`;
      ripple.className = 'ripple-effect-span';

      target.appendChild(ripple);
      target._ripple = ripple;

      requestAnimationFrame(() => {
        ripple.classList.add('in');
      });
    };

    const up = (e: MouseEvent) => {
      const target = rippleHost(e.target);
      const rippleData = target?._ripple;
      if (target && rippleData) {
        rippleData.classList.add('out');

        setTimeout(() => {
          rippleData.remove()
        }, 300);

        target._ripple = null;
      }
    };

    document.addEventListener('mousedown', down);
    document.addEventListener('mouseup', up);
    document.addEventListener('mouseleave', up);

    return () => {
      document.removeEventListener('mousedown', down);
      document.removeEventListener('mouseup', up);
      document.removeEventListener('mouseleave', up);
    };
  }, []);
}
