import React from "react";

// Observe an element's CSS-pixel size.
export default function useMeasure<T extends HTMLElement>(): [React.RefObject<T | null>, { w: number; h: number }] {
    const ref = React.useRef<T | null>(null);
    const [size, setSize] = React.useState({ w: 0, h: 0 });
    React.useEffect(() => {
        const el = ref.current;
        if (!el) return;
        const ro = new ResizeObserver((entries) => {
            const r = entries[0].contentRect;
            const w = Math.round(r.width);
            const h = Math.round(r.height);
            setSize((s) => (s.w === w && s.h === h ? s : { w, h }));
        });
        ro.observe(el);
        return () => ro.disconnect();
    }, []);
    return [ref, size];
}
