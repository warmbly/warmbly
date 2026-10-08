import { useCallback, useContext, useEffect, useState, type Dispatch, type SetStateAction } from "react";
import type { z } from "zod";
import { UserContext } from "./context/user";
import { useAppStore } from "@/stores/useAppStore";
import { BROWSE_STORAGE_PREFIX } from "@/lib/browseState";

export default function useBrowseState<T>(
    name: string,
    initial: T | (() => T),
    schema: z.ZodType<T>,
    options?: { initialOverride: T },
): [T, Dispatch<SetStateAction<T>>] {
    const userID = useContext(UserContext)?.user.id;
    const organizationID = useAppStore((state) => state.currentOrganization?.id ?? "personal");
    const key = userID ? `${BROWSE_STORAGE_PREFIX}${userID}:${organizationID}:${name}` : null;
    const read = () => {
        if (options) return options.initialOverride;
        if (key) {
            try {
                const saved = sessionStorage.getItem(key);
                if (saved !== null) {
                    const result = schema.safeParse(JSON.parse(saved)?.value);
                    if (result.success) return result.data;
                }
            } catch { /* Storage may be unavailable or malformed. */ }
        }
        return typeof initial === "function" ? (initial as () => T)() : initial;
    };
    const [memory, setMemory] = useState(() => ({ key, value: read() }));
    let current = memory;
    // Restore before committing, so a workspace switch never queries or saves the old filters.
    if (memory.key !== key) {
        current = { key, value: read() };
        setMemory(current);
    }
    const setValue = useCallback<Dispatch<SetStateAction<T>>>((next) => {
        setMemory((previous) => previous.key !== key ? previous : ({
            key: previous.key,
            value: typeof next === "function" ? (next as (value: T) => T)(previous.value) : next,
        }));
    }, [key]);
    useEffect(() => {
        if (memory.key === null || memory.key !== key) return;
        try { sessionStorage.setItem(memory.key, JSON.stringify({ value: memory.value })); }
        catch { /* Browsing must work when storage is disabled or full. */ }
    }, [key, memory]);
    return [current.value, setValue];
}
