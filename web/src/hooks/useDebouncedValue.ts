import { useEffect, useState } from "react";

// Returns value once it has been stable for `delay` ms. Used to hold API
// searches until the user pauses typing instead of firing per keystroke.
// Falsy values (cleared input) propagate immediately so menus close fast.
export default function useDebouncedValue<T>(value: T, delay = 300): T {
    const [debounced, setDebounced] = useState(value);
    useEffect(() => {
        if (!value) {
            setDebounced(value);
            return;
        }
        const t = window.setTimeout(() => setDebounced(value), delay);
        return () => window.clearTimeout(t);
    }, [value, delay]);
    return debounced;
}
