import { useCallback, useEffect, useRef, useState, type Dispatch, type SetStateAction } from "react";
import { useMe } from "@/hooks/useMe";
import { ADMIN_BROWSE_PREFIX, browseGeneration, canPersistBrowse } from "@/lib/browseState";

export default function useBrowseState<T>(name: string, initial: T, parse: (value: unknown) => T | null): [T, Dispatch<SetStateAction<T>>] {
    const { data: me } = useMe();
    const owner = me?.id;
    const key = owner ? `${ADMIN_BROWSE_PREFIX}${owner}:${name}` : null;
    const generation = useRef(browseGeneration());
    const read = () => {
        if (key) {
            try {
                const raw = sessionStorage.getItem(key);
                if (raw) return parse(JSON.parse(raw)?.value) ?? initial;
            } catch { /* Browsing works without storage. */ }
        }
        return initial;
    };
    const [memory, setMemory] = useState(() => ({ key, value: read() }));
    let current = memory;
    if (memory.key !== key) {
        current = { key, value: read() };
        setMemory(current);
    }
    const setValue = useCallback<Dispatch<SetStateAction<T>>>((next) => {
        setMemory((previous) => previous.key !== key ? previous : ({
            key, value: typeof next === "function" ? (next as (value: T) => T)(previous.value) : next,
        }));
    }, [key]);
    useEffect(() => {
        if (!key || key !== memory.key || !canPersistBrowse(generation.current)) return;
        try { sessionStorage.setItem(key, JSON.stringify({ value: memory.value })); }
        catch { /* Browsing works without storage. */ }
    }, [key, memory]);
    return [current.value, setValue];
}
